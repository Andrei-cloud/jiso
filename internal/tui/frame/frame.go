// Package frame renders the jiso TUI's outer screen frame around a page
// body (wireframe WF-01 "Global chrome", design contract
// .opencode/plans/00-overhaul-plan.md §"TUI design contract"):
//
//	┌─ jiso v2.0.0 ── target 10.0.0.5:8080 ✓ ─ spec visa.json ─ tx pool.json (12) ─┐
//	│  < page content >                                                              │
//	├────────────────────────────────────────────────────────────────────────────────┤
//	│ 1 dash  2 tx  …  q quit                                                        │
//	└────────────────────────────────────────────────────────────────────────────────┘
//
// The top rule embeds the app label left and the live status chips right
// (connection target+state, spec file, tx file+count, worker count); the
// footer sits inside the bottom half of the border. Every colour/style
// comes from internal/tui/theme tokens; under a colorless profile the
// whole frame degrades to plain text with ASCII rules. Render is pure —
// no I/O, no tea types — so it is golden-testable directly.
package frame

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// Responsive breakpoints (design contract §Responsive / wireframes floor
// test): the full top-rule text at ≥fullWidth, an elided chip set at
// medium, and the identity label alone below narrowWidth. The border
// itself never drops above MinWidth.
const (
	// FullWidth is the minimum width for the complete top-rule chip set.
	FullWidth = 100
	// NarrowWidth is below which the top rule carries no text (the
	// footer keeps primary keys only).
	NarrowWidth = 80
	// FallbackWidth and FallbackHeight size the frame before the first
	// tea.WindowSizeMsg reaches the model.
	FallbackWidth  = 80
	FallbackHeight = 24
	// MinContentHeight is the content area's hard floor: chrome yields to
	// it, never the other way round.
	MinContentHeight = 1
	// borderInset is the column cost of the border plus one space on each
	// side: the content/footer area is Width-borderInset wide.
	borderInset = 4
)

// Level is the responsive shape selected from the terminal width.
type Level int

// Responsive levels, coarsest last.
const (
	// LevelFull: width ≥ FullWidth — top rule with label + full chip set.
	LevelFull Level = iota
	// LevelMedium: NarrowWidth ≤ width < FullWidth — top rule text with
	// the chip set elided to fit.
	LevelMedium
	// LevelNarrow: width < NarrowWidth — top rule keeps the identity
	// label only (no chips), footer keeps primary keys only.
	LevelNarrow
)

// LevelFor maps a terminal width to its responsive level.
func LevelFor(width int) Level {
	switch {
	case width >= FullWidth:
		return LevelFull
	case width >= NarrowWidth:
		return LevelMedium
	default:
		return LevelNarrow
	}
}

// KeyHint is one footer entry: a key and what it does here. Page.Hints()
// returns the context-sensitive half; the router appends the global
// bindings. Primary marks the keys that survive the narrow footer.
//
// Key doubles as the click-dispatch spelling: it is written from the same
// matching vocabulary the bindings use (theme/keys.go), so
// frame.FooterHits reports it and a footer click replays exactly the typed
// key; labels that spell no single key ("j/k") are filtered inert by the
// hit map's synthKeyPress guard rather than firing a wrong press.
type KeyHint struct {
	Key     string
	Desc    string
	Primary bool
}

// Segment is one header chip token. Plain segments render in the muted
// token without a status symbol; otherwise Kind picks symbol+text+colour
// (theme guarantees colour is never the sole signal).
type Segment struct {
	Kind  theme.Kind
	Text  string
	Plain bool
}

// Props is everything Render needs for one frame. Zero values fall back:
// Theme→theme.Default(), Width/Height→80×24, App→"jiso".
type Props struct {
	Theme   *theme.Theme
	Width   int
	Height  int
	App     string
	Version string
	Conn    Segment // top-rule connection chip (always symbol+word)
	Target  string  // chip prefix "target <host:port>" when set
	Header  string  // connection header type chip ("hdr binary2") when set
	Spec    string  // spec file chip (base name); "no spec" handled by caller
	TxFile  string  // tx file chip "tx <file> (TxCount)" when set
	TxCount int
	Workers int // chip "N workers" when > 0
	Hints   []KeyHint
	// Console is the bottom console strip: the newest NON-TUI system
	// output line (connection manager). ConsoleErr styles it as an
	// error; empty renders no strip line (UAT: stderr writes corrupted
	// the frame; every such line now lands here instead).
	Console    string
	ConsoleErr bool
	Content    string // page body; truncated/padded into the content area
}

// clipTailCells trims a styled line to n visible cells, marking the cut with
// the theme's own ellipsis so it degrades under the ASCII glyph set, and
// preserving the leading SGR colour of the line. n <= 0 renders nothing.
func clipTailCells(th *theme.Theme, line string, n int) string {
	return th.Truncate(line, n)
}

// b2i is 1 when b, else 0 (frame-local, no bool arithmetic elsewhere).
func b2i(b bool) int {
	if b {
		return 1
	}

	return 0
}

// Render composes the framed layout into exactly Height lines (when Height
// allows) — chrome degrades before the content floor is violated, so no
// size produces a panic or a layout taller than the window.
func Render(p Props) string {
	th := p.Theme
	if th == nil {
		th = theme.Default()
	}
	width, height := p.Width, p.Height
	if width <= 0 {
		width = FallbackWidth
	}
	if height <= 0 {
		height = FallbackHeight
	}
	if p.App == "" {
		p.App = "jiso"
	}
	if width < MinWidth {
		return tooSmall(th, width, height)
	}
	lv := LevelFor(width)
	inner := width - borderInset

	// Shrink order honours MinContentHeight: top rule first, then the
	// footer pair (mid rule + footer row), bottom rule last — the same
	// chromeParts decision ContentSize reports.
	topShown, footerShown, bottomShown := chromeParts(height)
	var top, mid, footer, bottom []string
	if topShown {
		top = p.topRule(th, lv, width)
	}
	if footerShown {
		mid = []string{ruleLine(th, midLeft(th), midRight(th), width)}
		footer = wrapRow(th, p.footerLine(th, lv, inner)[0], width)
	}
	if bottomShown {
		bottom = []string{ruleLine(th, bottomLeft(th), bottomRight(th), width)}
	}
	consoleLine := ""
	if p.Console != "" && footerShown {
		label := "status" + th.Separator()

		if p.ConsoleErr {
			consoleLine = th.Status(theme.KindError, label+p.Console)
		} else {
			consoleLine = th.Dim.Render(label + p.Console)
		}
		// Chrome floor: the strip claims a line only while the content
		// floor survives alongside it; under extreme height pressure it
		// YIELDS instead of pushing the frame taller than the window
		// (UAT round 8 review: at h=4/5 the old composition emitted
		// height+1 lines, leaving the footer one row below where
		// FooterOrigin and the footer hit-map say it is).
		if height-len(top)-len(mid)-len(footer)-len(bottom)-1 < MinContentHeight {
			consoleLine = ""
		}
	}
	contentH := max(height-len(top)-len(mid)-len(footer)-len(bottom)-b2i(consoleLine != ""), MinContentHeight)

	lines := make([]string, 0, height)
	lines = append(lines, top...)
	lines = append(lines, wrapRows(th, fitContent(p.Content, inner, contentH), width)...)
	if consoleLine != "" {
		lines = append(lines, wrapRow(th, clipTailCells(th, consoleLine, inner), width)...)
	}
	lines = append(lines, mid...)
	lines = append(lines, footer...)
	lines = append(lines, bottom...)

	return strings.Join(lines, "\n")
}

func appLabel(p Props) string {
	if p.Version == "" {
		return p.App
	}
	return p.App + " " + p.Version
}

// chips builds the top-rule right side: conn (with target prefix), spec,
// tx (file + count), workers. Empty parts are skipped.
func (p Props) chips(th *theme.Theme) []string {
	var out []string
	conn := renderSegment(th, p.Conn)
	if p.Target != "" {
		target := th.Deemphasized.Render("target " + p.Target)
		if conn != "" {
			conn = target + " " + conn
		} else {
			conn = target
		}
	}
	if conn != "" {
		out = append(out, conn)
	}
	if p.Header != "" {
		out = append(out, th.TextMuted.Render("hdr "+p.Header))
	}
	if p.Spec != "" {
		out = append(out, th.TextMuted.Render("spec "+p.Spec))
	} else {
		out = append(out, th.Status(theme.KindWarn, "no spec"))
	}
	if p.TxFile != "" {
		out = append(out, th.TextMuted.Render(p.TxFile+" ("+itoaFrame(p.TxCount)+")"))
	}
	if p.Workers > 0 {
		out = append(out, th.TextMuted.Render(itoaFrame(p.Workers)+" workers"))
	}

	return out
}

// topRule renders the embedded top border rule: "┌─ label … ─ chip ─ chip ─┐".
// Chips drop from the tail (workers first, the connection chip never)
// until the rule fits; the narrow level and an impossible fit render the
// plain rule.
func (p Props) topRule(th *theme.Theme, lv Level, width int) []string {
	label := th.Accent.Render(appLabel(p))
	left := topRuleLeft(th) + " " + label + " "
	dash := dashGlyph(th)
	chips := p.chips(th)
	if lv == LevelNarrow && len(chips) > 1 {
		// Narrow keeps the identity label and the connection chip but
		// drops the informational chips (hdr/spec/tx/workers) — UAT round
		// 6 QA: the whole title used to drop below NarrowWidth, leaving a
		// featureless top rule and no sense of where you are. If even
		// label+conn can't fit, the loop still falls back to label-only
		// and then to a plain rule.
		chips = chips[:1]
	}
	for {
		if len(chips) > 0 {
			right := " " + strings.Join(chips, " "+dash+" ") + " " + dash + topRuleRight(th)
			fill := width - lipgloss.Width(left) - lipgloss.Width(right)
			if fill >= 1 {
				return []string{left + strings.Repeat(dash, fill) + right}
			}
			chips = chips[:len(chips)-1] // workers → tx → spec; conn never drops alone
			continue
		}
		// No chip set fits: label-only rule, then plain rule.
		right := " " + dash + topRuleRight(th)
		fill := width - lipgloss.Width(left) - lipgloss.Width(right)
		if fill >= 1 {
			return []string{truncate(left, max(width-lipgloss.Width(right), 2)) + strings.Repeat(dash, fill) + right}
		}

		return []string{ruleLine(th, topRuleLeft(th), topRuleRight(th), width)}
	}
}
