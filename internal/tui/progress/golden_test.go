package progress

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

var update = flag.Bool("update", false, "update golden files")

// checkGolden mirrors internal/tui/theme's golden harness: raw bytes with
// escapes, regenerated with -update.
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
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui/progress -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch\nwant: %q\ngot:  %q", name, string(want), got)
	}
}

// barBlob renders one bar state at a fixed width/clock; the unknown state
// pins the pulse so goldens prove "no frozen full bar".
func barBlob(t *theme.Theme, done, total int) string {
	now := epoch
	var b strings.Builder
	bar := NewBar(t, "send", total)
	bar.Started = now.Add(-3 * time.Second)
	bar.now = func() time.Time { return now }
	bar.SetProgress(done, total)
	b.WriteString(bar.View(40) + "\n")

	return b.String()
}

func TestBarStateGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		profile colorprofile.Profile
		hasDark bool
	}{
		{"truecolor-dark", colorprofile.TrueColor, true},
		{"ascii", colorprofile.ASCII, true},
	}
	states := []struct {
		suffix    string
		done, tot int
	}{
		{"0", 0, 10},
		{"50", 5, 10},
		{"100", 10, 10},
		{"unknown", 42, 0},
	}
	for _, tc := range cases {
		for _, st := range states {
			t.Run(tc.name+"-"+st.suffix, func(t *testing.T) {
				name := "bar_" + tc.name + "-" + st.suffix
				checkGolden(t, name, barBlob(theme.NewWith(tc.profile, tc.hasDark), st.done, st.tot))
			})
		}
	}
}
