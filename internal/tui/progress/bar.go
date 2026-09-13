package progress

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// Bar glyphs: truecolor/Unicode set and the ascii fallbacks. The empty
// track stays visible (░/.) so the bar's total extent never disappears.
const (
	FillFull  = "█"
	FillEmpty = "░"

	ASCIIFillFull  = "#"
	ASCIIFillEmpty = "."

	// BarMinWidth is the smallest width the full form can attempt; below
	// it View degrades, and it never panics for any width ≥ 8.
	BarMinWidth = 8
)

// Bar is a determinate progress bar. Total > 0 means the work is bounded
// and the bar fills; Total <= 0 means unknown, and the bar pulses as
// spinner+count instead — a frozen full bar is never shown for unknown
// durations. Percent clamps to [0,100] so a racy Done>Total renders 100%,
// never a negative or oversized fill.
//
// Eighths opts into sub-cell fill precision (▂▌▉ partial blocks after the
// full ones, the wireframe §H per-worker rows); it is an opt-in so every
// pre-existing caller keeps the full-block form, and it degrades to the
// plain "#"/"." ascii fill under theme.ASCII (7-bit goldens stay 7-bit).
type Bar struct {
	Theme   *theme.Theme
	Label   string
	Done    int
	Total   int // <=0 → unknown duration/count
	Started time.Time
	Note    string // optional suffix (e.g. "12 tps")
	Eighths bool   // sub-cell block fill (non-ascii only)

	now func() time.Time
}

// eighthGlyphs are the 1/8..7/8 left-block partial fills (index = eighths).
const eighthGlyphs = "▏▎▍▌▋▊▉"

// NewBar returns a bar at 0/total (total<=0 starts in pulse mode).
func NewBar(th *theme.Theme, label string, total int) *Bar {
	if th == nil {
		th = theme.Default()
	}

	return &Bar{Theme: th, Label: label, Total: total, now: time.Now}
}

// SetProgress records new counters; total<=0 switches the bar to pulse
// (unknown) mode. Negative done clamps to 0.
func (b *Bar) SetProgress(done, total int) {
	b.Done, b.Total = max(done, 0), total
}

// Unknown reports whether the total is unbounded/unknown.
func (b *Bar) Unknown() bool { return b.Total <= 0 }

// Percent returns the clamped [0,100] completion, or -1 when unknown.
func (b *Bar) Percent() int {
	if b.Unknown() {
		return -1
	}
	p := b.Done * 100 / b.Total
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}

	return p
}

// elapsed renders the time since Started ("" when unset).
func (b *Bar) elapsed() string {
	if b.Started.IsZero() {
		return ""
	}
	d := b.now().Sub(b.Started)
	if d < 0 {
		d = 0
	}

	return d.Truncate(time.Second).String()
}

// View renders the bar for a terminal width. Degradation order, richest
// first: "label [fill] 50% 3s note" → drop note → drop label → drop
// elapsed → "[fill] 50%" → "50%" → clipped "5"… Any width ≥ 8 renders
// without panic; widths below the bar floor degrade to text-only forms.
func (b *Bar) View(width int) string {
	if b.Theme == nil {
		b.Theme = theme.Default()
	}
	if b.Unknown() {
		return b.pulse(width)
	}

	pct := b.Percent()
	pctTxt := strconv.Itoa(pct) + "%"
	elapsed := b.elapsed()
	if b.Note != "" {
		if elapsed != "" {
			elapsed += " " + b.Note
		} else {
			elapsed = b.Note
		}
	}

	full := b.trackForm(width, b.Label, pctTxt, elapsed, true)
	full = b.fit(full, width, pctTxt)

	return full
}

// trackForm builds "…[fill] pct [elapsed]" using every cell width affords
// for the track; if even the minimal track does not fit, ok=false and the
// caller degrades further.
func (b *Bar) trackForm(width int, label, pctTxt, elapsed string, withBrackets bool) string {
	suffix := " " + pctTxt
	if elapsed != "" {
		suffix += " " + b.Theme.Dim.Render(elapsed)
	}
	prefix := ""
	if label != "" {
		prefix = b.Theme.TextMuted.Render(label) + " "
	}
	open, closeB := "[", "]"
	if !withBrackets {
		open, closeB = "", ""
	}
	// Reserve cells for prefix(raw)+brackets+suffix(raw) to size the track.
	reserved := len([]rune(pctTxt)) + 3 // suffix space + two brackets
	if label != "" {
		reserved += len([]rune(label)) + 1
	}
	if elapsed != "" {
		reserved += len([]rune(elapsed)) + 1
	}
	track := width - reserved
	if track < 1 {
		return ""
	}

	return prefix + open + b.fill(track, b.Percent()) + closeB + suffix
}

// fill renders a track of cells cells, filled proportionally to pct
// (clamped); 0% is empty, 100% is solid. With Eighths set (and a non-
// ascii theme) the boundary cell becomes the matching partial block.
func (b *Bar) fill(cells, pct int) string {
	if b.Eighths && !b.Theme.ASCII {
		return b.fillEighths(cells, pct)
	}
	full, empty := FillFull, FillEmpty
	if b.Theme.ASCII {
		full, empty = ASCIIFillFull, ASCIIFillEmpty
	}
	filled := (cells*pct + 50) / 100 // round to nearest
	filled = min(max(filled, 0), cells)

	filledStr := ""
	if filled > 0 {
		filledStr = b.Theme.Accent.Render(strings.Repeat(full, filled))
	}
	emptyStr := ""
	if n := cells - filled; n > 0 {
		emptyStr = b.Theme.Dim.Render(strings.Repeat(empty, n))
	}

	return filledStr + emptyStr
}

// fillEighths renders the track with sub-cell precision: floor(pct)
// full blocks plus the partial block matching the remainder (0..7
// eighths); 0% is empty, 100% solid, like fill.
func (b *Bar) fillEighths(cells, pct int) string {
	pct = min(max(pct, 0), 100)
	units := cells * pct * 8 / 100
	full, rem := units/8, units%8
	if full > cells {
		full, rem = cells, 0
	}

	var s string
	if full > 0 {
		s += b.Theme.Accent.Render(strings.Repeat(FillFull, full))
	}
	partial := 0
	if rem > 0 {
		partial = 1
		s += b.Theme.Accent.Render(string([]rune(eighthGlyphs)[rem-1]))
	}
	if n := cells - full - partial; n > 0 {
		s += b.Theme.Dim.Render(strings.Repeat(FillEmpty, n))
	}

	return s
}

// pulse is the unknown-duration fallback: spinner frame + done count,
// never a bar. Degrades to count-only, then clipped count.
func (b *Bar) pulse(width int) string {
	th := b.Theme
	fr := []rune(ASCIISpinFrames)
	if !th.ASCII {
		fr = []rune(DefaultSpinFrames)
	}
	frame := string(fr[SpinIndex(b.now(), DefaultSpinnerInterval, len(fr))])
	count := strconv.Itoa(max(b.Done, 0))
	note := ""
	if b.Note != "" {
		note = " " + b.Note
	}

	if cand := b.fitRaw(th.Accent.Render(frame)+" "+th.TextPrimary.Render(count+note), width, count); cand != "" {
		return cand
	}
	if cand := b.fitRaw(th.TextPrimary.Render(count+note), width, count); cand != "" {
		return cand
	}

	return clip(count, width)
}

// fit returns t if it fits width, else degrades: label-less track form,
// percent-only, clipped percent. "" inputs (track did not fit at all)
// degrade too.
func (b *Bar) fit(t string, width int, pctTxt string) string {
	if t != "" && lipgloss.Width(t) <= width {
		return t
	}
	if t2 := b.trackForm(width, "", pctTxt, "", true); t2 != "" && lipgloss.Width(t2) <= width {
		return t2
	}
	if lipgloss.Width(pctTxt) <= width {
		return b.Theme.TextPrimary.Render(pctTxt)
	}

	return clip(pctTxt, width)
}

// fitRaw mirrors fit for the pulse forms.
func (b *Bar) fitRaw(t string, width int, fallback string) string {
	if t != "" && lipgloss.Width(t) <= width {
		return t
	}
	if lipgloss.Width(fallback) <= width {
		return b.Theme.TextPrimary.Render(fallback)
	}

	return ""
}

// clip truncates raw text to at most n runes (n<=0 → "").
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[:n])
}
