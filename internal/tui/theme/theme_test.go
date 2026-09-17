package theme

import (
	"flag"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

var update = flag.Bool("update", false, "update golden files")

// TestHotKeyStyle: the body-copy hotkey affordance must
// carry the bold attribute together with the accent foreground under
// colour profiles — bold is a text attribute, so even the 16-colour
// profile keeps it — while the colourless ASCII profile keeps its
// plain-text, zero-escape-code contract (see package docs).
func TestHotKeyStyle(t *testing.T) {
	t.Setenv("JISO_ASCII", "")

	for _, tc := range []struct {
		name  string
		prof  colorprofile.Profile
		valid bool // true when the profile may emit escapes
	}{
		{"truecolor", colorprofile.TrueColor, true},
		{"ansi256", colorprofile.ANSI256, true},
		{"ansi16", colorprofile.ANSI, true},
		{"ascii", colorprofile.ASCII, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := NewWith(tc.prof, true)
			got := th.HotKey.Render("b")
			if !tc.valid {
				if got != "b" {
					t.Errorf("ascii HotKey = %q, want plain %q", got, "b")
				}

				return
			}
			if !strings.Contains(got, "\x1b[1;") {
				t.Errorf("HotKey lacks the bold SGR: %q", got)
			}
			// The glyph itself must survive stripping (styling never
			// alters the plain text).
			if stripped := stripSGR(got); stripped != "b" {
				t.Errorf("stripped HotKey = %q, want %q", stripped, "b")
			}
			// Same foreground token as Accent, plus Bold.
			if fg := th.HotKey.GetForeground(); fg != th.Accent.GetForeground() {
				t.Errorf("HotKey foreground %v, want Accent foreground %v", fg, th.Accent.GetForeground())
			}
		})
	}
}

// stripSGR drops CSI ... m sequences (test-local; the theme leaf must
// not pull the x/ansi dependency).
func stripSGR(s string) string {
	for {
		i := strings.IndexByte(s, 0x1b)
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], 'm')
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+1:]
	}
}

// TestHotKeySurvivesBaseRender pins the assumption the pages/widgets hotkey
// convention is built on: an outer whole-string Render of a *different*
// style wraps (not flattens) pre-styled inner segments, so hint lines
// must style their non-key spans with the base explicitly.
func TestHotKeySurvivesBaseRender(t *testing.T) {
	t.Setenv("JISO_ASCII", "")
	th := NewWith(colorprofile.TrueColor, true)

	inner := "press " + th.HotKey.Render("b")
	got := th.TextMuted.Render(inner + " to start")

	if !strings.Contains(got, inner) {
		t.Errorf("outer Render destroyed the inner HotKey span:\n%q", got)
	}
	if stripped := stripSGR(got); stripped != "press b to start" {
		t.Errorf("stripped = %q, want %q", stripped, "press b to start")
	}
}

// checkGolden implements the repo's os.WriteFile golden pattern (the CLI
// goldens use their own JSON harness; x/exp is not a repo dependency).
// Goldens hold raw bytes including escape sequences so a profile change
// shows up as a diff.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui/theme -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch\nwant: %q\ngot:  %q", name, string(want), got)
	}
}

// frame renders every user-visible degradation surface in one blob.
func frame(t *Theme) string {
	var b strings.Builder
	b.WriteString(t.Status(KindOK, "sent 33/33") + "\n")
	b.WriteString(t.Status(KindWarn, "slow response") + "\n")
	b.WriteString(t.Status(KindError, "connection refused") + "\n")
	b.WriteString(t.Selector(true) + t.Selection.Render("0210 RC 00") + "\n")
	b.WriteString(t.Selector(false) + t.TextPrimary.Render("jiso send --file tx.yaml") + "\n")
	b.WriteString(t.TextMuted.Render("spec: visa.sepa") + "\n")
	b.WriteString(t.Accent.Render("127.0.0.1:8583") + "\n")
	b.WriteString(t.Dim.Render("updated ~5s ago") + "\n")
	b.WriteString(t.Deemphasized.Render("(session golden-0001)") + "\n")
	b.WriteString(t.Border.Border(profileBorder(t)).Render("pane") + "\n")
	return b.String()
}

// profileBorder picks the border glyph set the theme is pinning. The ASCII
// profile's contract is "7-bit, zero non-ASCII bytes", so under it the box
// must not draw box-drawing rules.
func profileBorder(t *Theme) lipgloss.Border {
	if t.ASCII {
		return lipgloss.ASCIIBorder()
	}

	return lipgloss.NormalBorder()
}

func TestStatusGoldens(t *testing.T) {
	cases := []struct {
		name    string
		profile colorprofile.Profile
		hasDark bool
	}{
		{"truecolor-dark", colorprofile.TrueColor, true},
		{"truecolor-light", colorprofile.TrueColor, false},
		{"ansi256-dark", colorprofile.ANSI256, true},
		{"ansi16-dark", colorprofile.ANSI, true},
		{"ascii", colorprofile.ASCII, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JISO_ASCII", "") // golden determinism
			checkGolden(t, "status_"+tc.name, frame(NewWith(tc.profile, tc.hasDark)))
		})
	}
}

// TestNoColorYieldsZeroEscapes pins the NO_COLOR contract: Detect/Env
// resolve NO_COLOR to the ASCII profile, and every token then renders as
// plain text with zero escape codes (symbol+text carries the signal).
func TestNoColorYieldsZeroEscapes(t *testing.T) {
	env := []string{"NO_COLOR=1", "TERM=xterm-256color", "COLORTERM=truecolor"}
	p := colorprofile.Env(env)
	if p != colorprofile.ASCII {
		t.Fatalf("colorprofile.Env(NO_COLOR…) = %v, want ASCII", p)
	}
	th := NewWith(p, true)
	out := frame(th)
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x9b") {
		t.Errorf("NO_COLOR output contains escape codes: %q", out)
	}
	for _, want := range []string{"[ok] sent 33/33", "[!] slow response", "[x] connection refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("NO_COLOR output missing %q in %q", want, out)
		}
	}
}

// TestNoColorKeepsUnicodeGlyphs pins the decoupling: NO_COLOR drops
// colour only — the Unicode glyph set stays unless JISO_ASCII=1 forces
// the ASCII set.
func TestNoColorKeepsUnicodeGlyphs(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("JISO_ASCII", "")
	if th := New(); th.ASCII {
		t.Error("NO_COLOR forced the ASCII glyph set")
	}
	t.Setenv("JISO_ASCII", "1")
	if th := New(); !th.ASCII {
		t.Error("JISO_ASCII=1 with NO_COLOR must still select ASCII glyphs")
	}
}

func TestJISOAsciiGlyphFallback(t *testing.T) {
	// The JISO_ASCII env policy itself is pinned in the New tests; the
	// glyph behavior is pinned here on the pure constructor (NewWith
	// reads no env, so tests build themes concurrently).
	th := NewWith(colorprofile.TrueColor, true)
	th.ASCII = true
	if !th.ASCII {
		t.Fatal("JISO_ASCII=1 must enable ASCII mode")
	}
	got := th.Status(KindOK, "ok")
	if !strings.Contains(got, ASCIIOK) || strings.Contains(got, GlyphOK) {
		t.Errorf("Status(KindOK) = %q, want %q not %q", got, ASCIIOK, GlyphOK)
	}
	for k, want := range map[Kind]string{KindOK: "[ok]", KindWarn: "[!]", KindError: "[x]"} {
		if s := th.Symbol(k); s != want {
			t.Errorf("Symbol(%v) = %q, want %q", k, s, want)
		}
	}
	if sel := th.Selector(true); sel != "> " || strings.Contains(sel, GlyphSelected) {
		t.Errorf("Selector(true) = %q, want %q", sel, ASCIISelected+" ")
	}
}

// TestAdaptiveColorsResolveLightDark asserts the LightDark resolution
// directly: same token, two backgrounds, two different TrueColors, each
// equal to the table's light/dark hex.
func TestAdaptiveColorsResolveLightDark(t *testing.T) {
	t.Setenv("JISO_ASCII", "")
	dark := NewWith(colorprofile.TrueColor, true)
	light := NewWith(colorprofile.TrueColor, false)

	cases := []struct {
		name      string
		darkFG    color.Color
		lightFG   color.Color
		wantDark  color.Color
		wantLight color.Color
	}{
		{"status.ok", dark.StatusOK.GetForeground(), light.StatusOK.GetForeground(), lipgloss.Color("#3fb950"), lipgloss.Color("#116329")},
		{"status.warn", dark.StatusWarn.GetForeground(), light.StatusWarn.GetForeground(), lipgloss.Color("#d29922"), lipgloss.Color("#9a6700")},
		{"status.error", dark.StatusError.GetForeground(), light.StatusError.GetForeground(), lipgloss.Color("#ff7b72"), lipgloss.Color("#cf222e")},
		{"accent", dark.Accent.GetForeground(), light.Accent.GetForeground(), lipgloss.Color("#4493f8"), lipgloss.Color("#0550ae")},
		{"text.muted", dark.TextMuted.GetForeground(), light.TextMuted.GetForeground(), lipgloss.Color("#8d96a0"), lipgloss.Color("#59636e")},
	}
	for _, tc := range cases {
		if reflect.DeepEqual(tc.darkFG, tc.lightFG) {
			t.Errorf("%s: light and dark resolve to the same color %v", tc.name, tc.darkFG)
		}
		if !reflect.DeepEqual(tc.darkFG, tc.wantDark) {
			t.Errorf("%s dark fg = %#v, want %#v", tc.name, tc.darkFG, tc.wantDark)
		}
		if !reflect.DeepEqual(tc.lightFG, tc.wantLight) {
			t.Errorf("%s light fg = %#v, want %#v", tc.name, tc.lightFG, tc.wantLight)
		}
	}

	// text.primary must never be tinted.
	if _, ok := dark.TextPrimary.GetForeground().(lipgloss.NoColor); !ok {
		t.Errorf("text.primary fg = %#v, want lipgloss.NoColor{}", dark.TextPrimary.GetForeground())
	}

	// Selection background adapts; white-on-blue fg stays constant.
	if bg := dark.Selection.GetBackground(); !reflect.DeepEqual(bg, lipgloss.Color("#1f6feb")) {
		t.Errorf("dark selection bg = %#v, want #1f6feb", bg)
	}
	if bg := light.Selection.GetBackground(); !reflect.DeepEqual(bg, lipgloss.Color("#0969da")) {
		t.Errorf("light selection bg = %#v, want #0969da", bg)
	}

	// And the standalone LightDark lookup agrees with the theme's choice.
	ld := lipgloss.LightDark(true)
	if got := ld(lipgloss.Color("#116329"), lipgloss.Color("#3fb950")); !reflect.DeepEqual(got, dark.StatusOK.GetForeground()) {
		t.Errorf("LightDark(true) lookup %#v != theme fg %#v", got, dark.StatusOK.GetForeground())
	}
}

// TestProfileFallbackSlots pins the Complete(profile) fallback chain:
// ANSI16 themes get the canonical hue slot, ANSI256 the mid-luminance
// slot, TrueColor the hex.
func TestProfileFallbackSlots(t *testing.T) {
	t.Setenv("JISO_ASCII", "")
	ok := NewWith(colorprofile.ANSI, true).StatusOK.GetForeground()
	if !reflect.DeepEqual(ok, lipgloss.Color("2")) {
		t.Errorf("ANSI status.ok = %#v, want Color(\"2\")", ok)
	}
	ok256 := NewWith(colorprofile.ANSI256, true).StatusOK.GetForeground()
	if !reflect.DeepEqual(ok256, lipgloss.Color("34")) {
		t.Errorf("ANSI256 status.ok = %#v, want Color(\"34\")", ok256)
	}
}

func TestProfileEnvMatrix(t *testing.T) {
	cases := []struct {
		env  []string
		want colorprofile.Profile
	}{
		{[]string{"TERM=xterm-256color", "COLORTERM=truecolor"}, colorprofile.TrueColor},
		{[]string{"TERM=xterm-256color"}, colorprofile.ANSI256},
		{[]string{"TERM=xterm-color"}, colorprofile.ANSI},
		{[]string{"TERM=dumb"}, colorprofile.NoTTY},
		{[]string{"TERM=xterm-256color", "COLORTERM=truecolor", "NO_COLOR=1"}, colorprofile.ASCII},
		{[]string{"NO_COLOR=1", "COLORTERM=truecolor"}, colorprofile.NoTTY}, // no TERM => dumb
	}
	for _, tc := range cases {
		if got := colorprofile.Env(tc.env); got != tc.want {
			t.Errorf("colorprofile.Env(%v) = %v, want %v", tc.env, got, tc.want)
		}
	}
}

func TestTokenTableSanity(t *testing.T) {
	seen := map[string]bool{}
	for _, tk := range tokens {
		if tk.name == "" || seen[tk.name] {
			t.Errorf("token with empty/duplicate name %q", tk.name)
		}
		seen[tk.name] = true
		for _, spec := range [][]string{{tk.light, tk.dark}, {tk.ansi, tk.ansi256}} {
			for _, s := range spec {
				if s == "" {
					continue
				}
				if _, ok := lipgloss.Color(s).(lipgloss.NoColor); ok {
					t.Errorf("token %s: unparsable color %q", tk.name, s)
				}
			}
		}
	}
}

func TestSelectorConstantWidth(t *testing.T) {
	t.Setenv("JISO_ASCII", "1")
	th := NewWith(colorprofile.TrueColor, true)
	for _, sel := range []bool{true, false} {
		if n := lipgloss.Width(th.Selector(sel)); n != 2 {
			t.Errorf("Selector(%v) width = %d, want 2", sel, n)
		}
	}
}

// TestElideMiddle pins the one mid-elision in the TUI, in both glyph sets.
func TestElideMiddle(t *testing.T) {
	t.Setenv("JISO_ASCII", "")

	cases := []struct {
		name     string
		in       string
		head     int
		tail     int
		wantUni  string
		wantASCI string
	}{
		{"session id", "9f3ca1e2b7d84455a1", 4, 2, "9f3c…a1", "9f3c~a1"},
		{"masked pan", "41111122222233334444", 6, 4, "411111…4444", "411111~4444"},
		// The floor is head+tail+1: below it the marker would not save a cell.
		// Callers with a taste for when shortening is worth it guard earlier
		// (shortSessionID leaves ids of 8 alone), which is a copy decision, not
		// a glyph decision.
		{"floor", "9f3ca1e2", 4, 2, "9f3c…e2", "9f3c~e2"},
		{"below the floor", "9f3ca1e", 4, 2, "9f3ca1e", "9f3ca1e"},
		// Counted in runes, so a leading combining char cannot cut a rune in half.
		{"rune safe", "é9f3ca1e2b7", 3, 2, "é9f…b7", "é9f~b7"},
	}

	for _, tc := range cases {
		uni := NewWith(colorprofile.TrueColor, true)
		if got := uni.ElideMiddle(tc.in, tc.head, tc.tail); got != tc.wantUni {
			t.Errorf("%s: unicode profile: got %q, want %q", tc.name, got, tc.wantUni)
		}

		ascii := NewWith(colorprofile.ASCII, true)
		got := ascii.ElideMiddle(tc.in, tc.head, tc.tail)
		if got != tc.wantASCI {
			t.Errorf("%s: ascii profile: got %q, want %q", tc.name, got, tc.wantASCI)
		}

		// The point of asking the theme rather than writing the literal: the
		// marker is ASCII in the ASCII profile, whatever the value around it
		// contains. A "…" here would land in a 7-bit golden.
		if strings.Contains(got, GlyphEllipsis) {
			t.Errorf("%s: ascii profile used the unicode ellipsis in %q", tc.name, got)
		}
	}
}

// BorderFocused applies the accent foreground over the border token,
// byte-identical to the pages sectionW idiom; the ASCII profile stays
// plain-text with zero escapes.
func TestBorderFocused(t *testing.T) {
	t.Run("truecolor matches the by-hand pages derivation", func(t *testing.T) {
		th := NewWith(colorprofile.TrueColor, true)
		b := lipgloss.RoundedBorder()

		want := lipgloss.NewStyle().
			Border(b).
			BorderForeground(th.Border.GetBorderTopForeground()).
			BorderForeground(th.Accent.GetForeground()).
			Render("x")
		got := th.BorderFocused().Border(b).Render("x")
		if got != want {
			t.Errorf("focused border bytes differ:\ngot:  %q\nwant: %q", got, want)
		}

		unfocused := lipgloss.NewStyle().
			Border(b).
			BorderForeground(th.Border.GetBorderTopForeground()).
			Render("x")
		if got == unfocused {
			t.Error("a focused border must differ from the neutral one")
		}
	})

	t.Run("ascii stays plain text", func(t *testing.T) {
		th := NewWith(colorprofile.ASCII, true)
		got := th.BorderFocused().Border(lipgloss.ASCIIBorder()).Render("x")
		if strings.ContainsAny(got, "\x1b\u009b") {
			t.Errorf("ascii focused border emitted escape codes: %q", got)
		}
		for _, r := range got {
			if r > 127 {
				t.Errorf("ascii focused border emitted non-ASCII rune %q", r)
				break
			}
		}
	})
}
