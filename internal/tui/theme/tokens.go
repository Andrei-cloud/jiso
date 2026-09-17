package theme

// This file is the single source of truth for the TUI visual language
// (the design contract) and doubles as the token reference for M5
// screens.
//
// Column semantics:
//   - light/dark: TrueColor hexes, picked per background via
//     lipgloss.LightDark. Chosen for WCAG AA text contrast (>=4.5:1,
//     AA large for borders) against the conventional light (#ffffff) and
//     dark (#0d1117) terminal backgrounds.
//   - ansi/ansi256: fixed fallback slots for 16-/256-color terminals.
//     Background adaptation is impossible once the terminal owns the
//     palette, so mid-luminance slots legible on both backgrounds are
//     used; ANSI slots carry the canonical hue so user palettes stay
//     respected.
//   - An empty hex means "use the terminal default" — the style property
//     is left unset (rendered as plain text), which preserves whatever
//     contrast the user configured.
type token struct {
	name    string
	light   string
	dark    string
	ansi    string
	ansi256 string
}

var (
	// status.ok — green. #116329 ≈5.9:1 on #ffffff; #3fb950 ≈7.0:1 on
	// #0d1117; ANSI green / 256-slot 34 (mid-luminance, both bgs).
	tkStatusOK = token{"status.ok", "#116329", "#3fb950", "2", "34"}

	// status.warn — amber. #9a6700 ≈5.1:1 on #ffffff; #d29922 ≈8.0:1 on
	// #0d1117; yellow slot, 256-slot 136 avoids eye-searing 172/196.
	tkStatusWarn = token{"status.warn", "#9a6700", "#d29922", "3", "136"}

	// status.error — red. #cf222e ≈5.4:1 on #ffffff; #ff7b72 ≈7.9:1 on
	// #0d1117; red slot, 256-slot 167 (dark red readable on light too).
	tkStatusError = token{"status.error", "#cf222e", "#ff7b72", "1", "167"}

	// text.primary — deliberately UNCOLORED: inheriting the terminal
	// default foreground keeps native contrast on any palette; body
	// text is never tinted.
	tkTextPrimary = token{"text.primary", "", "", "", ""}

	// text.muted — gray. #59636e ≈5.3:1 on #ffffff; #8d96a0 ≈6.6:1 on
	// #0d1117; bright-black slot, 256-slot 245 stays above AA.
	tkTextMuted = token{"text.muted", "#59636e", "#8d96a0", "8", "245"}

	// accent — blue. #0550ae ≈6.4:1 on #ffffff; #4493f8 ≈6.3:1 on
	// #0d1117; blue slot, 256-slot 33.
	tkAccent = token{"accent", "#0550ae", "#4493f8", "4", "33"}

	// border — chrome, not text: ~1.5:1 is intentional so structure
	// stays quieter than content (light #d0d7de / dark #3d444d).
	tkBorder = token{"border", "#d0d7de", "#3d444d", "8", "238"}

	// border.subtle — one step quieter still, for inner separators
	// (design contract: ≤1 border depth between edge and content).
	tkSubtleBorder = token{"border.subtle", "#eaeef2", "#272e37", "8", "235"}

	// selection.bg — mid blue, same family as accent so "focused" reads
	// consistently; white-on-#0969da ≈5.2:1, white-on-#1f6feb ≈4.8:1.
	tkSelectionBg = token{"selection.bg", "#0969da", "#1f6feb", "12", "27"}

	// selection.fg — white on both: selection is a background signal,
	// fg stays constant to guarantee the AA ratios quoted above.
	tkSelectionFg = token{"selection.fg", "#ffffff", "#ffffff", "15", "231"}
)

// tokens lists every token in documentation order (TestTokenTableSanity
// iterates it).
var tokens = []token{
	tkStatusOK,
	tkStatusWarn,
	tkStatusError,
	tkTextPrimary,
	tkTextMuted,
	tkAccent,
	tkBorder,
	tkSubtleBorder,
	tkSelectionBg,
	tkSelectionFg,
}
