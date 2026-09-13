// Package theme provides the jiso TUI's semantic color tokens and the
// accessibility rules around them (design contract:
// .opencode/plans/00-overhaul-plan.md §"TUI design contract").
//
// Rules encoded structurally here:
//
//   - Semantic tokens only (status.ok/warn/error, text.muted, accent, …);
//     screens never hardcode colors. See tokens.go for the table.
//   - Adaptive colors: every TrueColor token resolves through
//     lipgloss.LightDark against the detected background, so themes are
//     legible on light and dark terminals.
//   - Never color alone: Status renders symbol+text (✓/⚠/✗ plus the
//     message); de-emphasis goes through Dim, which pairs a faint
//     attribute with the muted color instead of relying on low contrast.
//   - Graceful degradation: colorprofile.Detect honors NO_COLOR/CLICOLOR/
//     TERM/COLORTERM. Under a colorless profile (ASCII/NoTTY — which is
//     what NO_COLOR resolves to) every style degrades to plain text with
//     zero escape codes. JISO_ASCII=1 additionally swaps Unicode glyphs
//     for ASCII fallbacks (✓→[ok], ⚠→[!], ✗→[x], ▸→>) so goldens can pin
//     both glyph sets.
package theme

import (
	"image/color"
	"os"
	"strconv"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Kind is the semantic status carried by Status renderings. Every kind is
// always rendered as symbol + text + (optional) color — color is never the
// sole signal.
type Kind int

const (
	// KindOK renders "✓ text" ("[ok] text" in ASCII mode).
	KindOK Kind = iota
	// KindWarn renders "⚠ text" ("[!] text" in ASCII mode).
	KindWarn
	// KindError renders "✗ text" ("[x] text" in ASCII mode).
	KindError
)

// Unicode status/selection glyphs (design contract: symbol+text, never
// emoji chips).
const (
	GlyphOK       = "✓"
	GlyphWarn     = "⚠"
	GlyphError    = "✗"
	GlyphSelected = "▸"
)

// ASCII fallbacks used when ASCII mode is on (colorless profile or
// JISO_ASCII=1).
const (
	ASCIIOK       = "[ok]"
	ASCIIWarn     = "[!]"
	ASCIIError    = "[x]"
	ASCIISelected = ">"
)

// Theme is the set of semantic token styles for one terminal: one color
// profile plus one background (light/dark) resolution. Build it with New
// (auto-detect) or NewWith (forced profile — goldens pin both).
type Theme struct {
	// ASCII selects the ASCII glyph set in Status/Selector. It is true
	// when JISO_ASCII=1, or when the terminal itself cannot do Unicode
	// (TERM=dumb / non-TTY output). NO_COLOR alone does NOT set it —
	// colourless terminals keep the Unicode glyphs (see New).
	ASCII bool

	// Status tokens: bold + colored; symbols are prepended by Status.
	StatusOK    lipgloss.Style
	StatusWarn  lipgloss.Style
	StatusError lipgloss.Style

	// TextPrimary inherits the terminal default foreground (never
	// tinted); TextMuted is the AA-compliant gray.
	TextPrimary lipgloss.Style
	TextMuted   lipgloss.Style

	// Accent marks interactive/highlighted content (links, counts).
	Accent lipgloss.Style

	// HotKey renders key glyphs inside body copy (the bold-accent
	// affordance matching the footer's accented keys: "press b to
	// start" reads as an actionable hotkey, not dim prose).
	HotKey lipgloss.Style

	// Border / SubtleBorder set BorderForeground for pane and inner
	// separator borders respectively.
	Border       lipgloss.Style
	SubtleBorder lipgloss.Style

	// Selection is the list-row highlight (bg + contrasting fg);
	// SelectionBg/SelectionFg expose the halves for widgets that need
	// them separately.
	Selection   lipgloss.Style
	SelectionBg lipgloss.Style
	SelectionFg lipgloss.Style

	// Dim de-emphasizes via faint attribute + muted color (never
	// low-contrast-only); Deemphasized is the muted color without the
	// attribute.
	Dim          lipgloss.Style
	Deemphasized lipgloss.Style

	profile colorprofile.Profile
}

// New detects everything from the process environment: the color profile
// via colorprofile.Detect on os.Stdout (honors NO_COLOR, CLICOLOR, TERM,
// COLORTERM) and the background via
// lipgloss.HasDarkBackground(os.Stdin, os.Stdout) — non-TTY input fails
// the raw-mode query and defaults to dark, which matches the token table.
// In Bubble Tea prefer seeding NewWith from tea.BackgroundColorMsg.
//
// Glyph selection: ASCII glyphs come from JISO_ASCII=1, TERM=dumb, or a
// non-TTY output — never from NO_COLOR alone, which drops colour but
// keeps the Unicode glyph set (the symbol+word contract renders fine
// without colour).
func New() *Theme {
	t := NewWith(
		colorprofile.Detect(os.Stdout, os.Environ()),
		lipgloss.HasDarkBackground(os.Stdin, os.Stdout),
	)
	// The env policy lives here, not in NewWith: an explicit-profile
	// constructor is pure so tests can build themes concurrently (a
	// process-wide JISO_ASCII read inside NewWith serialized the whole
	// test suite behind t.Setenv).
	switch {
	case truthy(os.Getenv("JISO_ASCII")):
		t.ASCII = true
	case hasEnv(os.Environ(), "NO_COLOR"):
		t.ASCII = false
	}

	return t
}

// hasEnv reports whether environ contains a non-empty KEY= assignment.
func hasEnv(environ []string, key string) bool {
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, key+"="); ok && v != "" {
			return true
		}
	}

	return false
}

// NewWith builds a Theme for an explicit color profile and background.
// It reads no environment: JISO_ASCII/NO_COLOR are applied by New.
// Under a colorless profile (ASCII/NoTTY) every token degrades to the
// identity style: Render emits plain text, zero escape codes.
func NewWith(p colorprofile.Profile, hasDark bool) *Theme {
	t := &Theme{
		profile: p,
		ASCII:   p <= colorprofile.ASCII,
	}
	if p <= colorprofile.ASCII {
		return t // no colors, no attributes: plain text everywhere.
	}

	ld := lipgloss.LightDark(hasDark)
	complete := lipgloss.Complete(p)
	fg := func(tk token) color.Color {
		return complete(
			lipgloss.Color(tk.ansi),
			lipgloss.Color(tk.ansi256),
			ld(lipgloss.Color(tk.light), lipgloss.Color(tk.dark)),
		)
	}

	t.StatusOK = lipgloss.NewStyle().Foreground(fg(tkStatusOK)).Bold(true)
	t.StatusWarn = lipgloss.NewStyle().Foreground(fg(tkStatusWarn)).Bold(true)
	t.StatusError = lipgloss.NewStyle().Foreground(fg(tkStatusError)).Bold(true)

	t.TextPrimary = lipgloss.NewStyle().Foreground(fg(tkTextPrimary))
	t.TextMuted = lipgloss.NewStyle().Foreground(fg(tkTextMuted))
	t.Accent = lipgloss.NewStyle().Foreground(fg(tkAccent))
	t.HotKey = lipgloss.NewStyle().Foreground(fg(tkAccent)).Bold(true)

	t.Border = lipgloss.NewStyle().BorderForeground(fg(tkBorder))
	t.SubtleBorder = lipgloss.NewStyle().BorderForeground(fg(tkSubtleBorder))

	t.Selection = lipgloss.NewStyle().
		Foreground(fg(tkSelectionFg)).
		Background(fg(tkSelectionBg))
	t.SelectionBg = lipgloss.NewStyle().Background(fg(tkSelectionBg))
	t.SelectionFg = lipgloss.NewStyle().Foreground(fg(tkSelectionFg))

	t.Dim = lipgloss.NewStyle().Foreground(fg(tkTextMuted)).Faint(true)
	t.Deemphasized = lipgloss.NewStyle().Foreground(fg(tkTextMuted))

	return t
}

// Profile reports the color profile this theme was built for.
func (t *Theme) Profile() colorprofile.Profile { return t.profile }

// Symbol returns the status glyph for k under the theme's ASCII mode.
func (t *Theme) Symbol(k Kind) string {
	if t.ASCII {
		switch k {
		case KindOK:
			return ASCIIOK
		case KindWarn:
			return ASCIIWarn
		default:
			return ASCIIError
		}
	}
	switch k {
	case KindOK:
		return GlyphOK
	case KindWarn:
		return GlyphWarn
	default:
		return GlyphError
	}
}

// Status renders "symbol text" with the matching status style. The
// symbol+text pair guarantees the signal survives color loss (NO_COLOR,
// 16-color terminals, screen readers).
func (t *Theme) Status(k Kind, text string) string {
	st := t.StatusOK
	switch k {
	case KindWarn:
		st = t.StatusWarn
	case KindError:
		st = t.StatusError
	}
	return st.Render(t.Symbol(k) + " " + text)
}

// Selector renders the list-row selection marker, padded to a constant
// two cells: "▸ " / "> " when selected, "  " otherwise.
func (t *Theme) Selector(selected bool) string {
	if !selected {
		return "  "
	}
	if t.ASCII {
		return ASCIISelected + " "
	}
	return GlyphSelected + " "
}

var (
	defaultOnce  sync.Once
	defaultTheme *Theme
)

// Default is the lazily-detected process theme: what a page gets when it is
// constructed without an explicit one (production). Golden tests inject their
// own via NewWith so they pin a profile deterministically.
func Default() *Theme {
	defaultOnce.Do(func() { defaultTheme = New() })
	return defaultTheme
}

func truthy(v string) bool {
	b, err := strconv.ParseBool(v)
	return err == nil && b
}
