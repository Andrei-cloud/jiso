package palette

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

var update = flag.Bool("update", false, "update golden files")

// checkGolden implements the repo's golden pattern (same as
// internal/tui/theme, frame, and widgets): goldens hold raw bytes
// including escape sequences, so token or glyph changes show up as diffs.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui/palette -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch\nwant: %q\ngot:  %q", name, string(want), got)
	}
}

func goldenTheme(prof colorprofile.Profile) *theme.Theme {
	return theme.NewWith(prof, true)
}

// renderState builds a palette, types query, and renders the panel.
func renderState(prof colorprofile.Profile, query string) string {
	m := New(goldenTheme(prof), SeedMatcher(), 40, 12)
	for _, r := range query {
		m, _ = m.Update(ch(r))
	}
	if query == "se" {
		m, _ = m.Update(special(tea.KeyDown)) // pin the cursor row styling
	}

	return m.View()
}

func TestPaletteGoldenOpen(t *testing.T) {
	t.Parallel()

	for _, p := range []struct {
		name string
		prof colorprofile.Profile
	}{{"truecolor", colorprofile.TrueColor}, {"ascii", colorprofile.ASCII}} {
		t.Run(p.name, func(t *testing.T) {
			checkGolden(t, "palette_open_"+p.name, renderState(p.prof, ""))
		})
	}
}

func TestPaletteGoldenFiltered(t *testing.T) {
	t.Parallel()

	for _, p := range []struct {
		name string
		prof colorprofile.Profile
	}{{"truecolor", colorprofile.TrueColor}, {"ascii", colorprofile.ASCII}} {
		t.Run(p.name, func(t *testing.T) {
			checkGolden(t, "palette_filtered_se_"+p.name, renderState(p.prof, "se"))
		})
	}
}

func TestPaletteGoldenNoMatch(t *testing.T) {
	t.Parallel()

	for _, p := range []struct {
		name string
		prof colorprofile.Profile
	}{{"truecolor", colorprofile.TrueColor}, {"ascii", colorprofile.ASCII}} {
		t.Run(p.name, func(t *testing.T) {
			checkGolden(t, "palette_nomatch_"+p.name, renderState(p.prof, "qqqqzzz"))
		})
	}
}

// TestGoldensAsciiArePlain re-asserts on the golden bytes themselves that
// every ascii golden is escape- and non-ASCII-free (ascii-safe overlay).
func TestGoldensAsciiArePlain(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"palette_open", "palette_filtered_se", "palette_nomatch"} {
		data, err := os.ReadFile(filepath.Join("testdata", name+"_ascii.golden"))
		if err != nil {
			t.Fatalf("read ascii golden %s: %v", name, err)
		}
		s := string(data)
		if strings.ContainsAny(s, "\x1b\u009b") {
			t.Errorf("ascii golden %s contains escape codes", name)
		}
		for _, r := range s {
			if r > 127 {
				t.Errorf("ascii golden %s contains non-ASCII rune %q", name, r)
				break
			}
		}
	}
}
