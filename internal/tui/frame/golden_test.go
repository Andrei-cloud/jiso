package frame

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

var update = flag.Bool("update", false, "update golden files")

// checkGolden implements the repo's os.WriteFile golden pattern (same as
// internal/tui/theme): goldens hold raw bytes including escape sequences,
// so a profile or layout change shows up as a diff.
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
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui/frame -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch\nwant: %q\ngot:  %q", name, string(want), got)
	}
}

// goldenProps is the deterministic fixture: fixed version, chips
// and hints; content is a stand-in page body.
func goldenProps(th *theme.Theme, width int) Props {
	p := matrixProps(th, width)
	p.Content = "Status\n\ncoming in M5"

	return p
}

// TestFrameGoldens pins the full layout at 3 widths × ascii/truecolor.
func TestFrameGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		profile colorprofile.Profile
		hasDark bool
	}{
		{"truecolor", colorprofile.TrueColor, true},
		{"ascii", colorprofile.ASCII, true},
	}
	for _, width := range []int{120, 90, 70} {
		for _, tc := range cases {
			name := "frame_" + itoa(width) + "_" + tc.name
			t.Run(name, func(t *testing.T) {
				th := theme.NewWith(tc.profile, tc.hasDark)
				checkGolden(t, name, Render(goldenProps(th, width)))
			})
		}
	}
}

// TestFrameGoldenAsciiIsPlain re-asserts on the golden bytes themselves
// that the ascii goldens carry no escapes and only ASCII separators.
func TestFrameGoldenAsciiIsPlain(t *testing.T) {
	t.Parallel()

	for _, width := range []int{120, 90, 70} {
		data, err := os.ReadFile(filepath.Join("testdata", "frame_"+itoa(width)+"_ascii.golden"))
		if err != nil {
			t.Fatalf("read ascii golden %d: %v", width, err)
		}
		s := string(data)
		if strings.ContainsAny(s, "\x1b\u009b") {
			t.Errorf("ascii golden %d contains escape codes", width)
		}
		for _, r := range s {
			if r > 127 {
				t.Errorf("ascii golden %d contains non-ASCII rune %q", width, r)
				break
			}
		}
	}
}

// itoa keeps the test free of strconv noise.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
