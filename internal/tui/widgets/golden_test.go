package widgets

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

// checkGolden implements the repo's os.WriteFile golden pattern (same as
// internal/tui/theme and internal/tui/frame): goldens hold raw bytes
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
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui/widgets -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch\nwant: %q\ngot:  %q", name, string(want), got)
	}
}

func goldenProfiles() []struct {
	name string
	prof colorprofile.Profile
} {
	return []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}
}

func TestListGolden(t *testing.T) {
	t.Parallel()

	for _, p := range goldenProfiles() {
		t.Run(p.name, func(t *testing.T) {
			m := NewList(theme.NewWith(p.prof, true), 30, 5)
			m.SetItems(items(7))
			m.SetCursor(3)
			checkGolden(t, "list_5x30_"+p.name, m.View())
		})
	}
}

func TestListEmptyGolden(t *testing.T) {
	t.Parallel()

	for _, p := range goldenProfiles() {
		t.Run(p.name, func(t *testing.T) {
			m := NewList(theme.NewWith(p.prof, true), 30, 5)
			m.SetEmptyMessage("No transactions. Press t to pick a tx file.")
			checkGolden(t, "list_empty_"+p.name, m.View())
		})
	}
}

func TestTableGolden(t *testing.T) {
	t.Parallel()

	for _, p := range goldenProfiles() {
		t.Run(p.name, func(t *testing.T) {
			th := theme.NewWith(p.prof, true)
			m := tableFixture(th, 44)
			m.SortBy(1, false)
			m.SetCursor(2)
			checkGolden(t, "table_44_"+p.name, m.View())
		})
	}
}

func TestTableEmptyGolden(t *testing.T) {
	t.Parallel()

	for _, p := range goldenProfiles() {
		t.Run(p.name, func(t *testing.T) {
			m := tableFixture(theme.NewWith(p.prof, true), 44)
			m.SetRows(nil)
			checkGolden(t, "table_empty_"+p.name, m.View())
		})
	}
}

func TestConfirmGolden(t *testing.T) {
	t.Parallel()

	for _, p := range goldenProfiles() {
		t.Run(p.name, func(t *testing.T) {
			m := NewConfirmDialog(theme.NewWith(p.prof, true), "Quit with 2 active workers?")
			m.Open()
			checkGolden(t, "confirm_"+p.name, m.View())
		})
	}
}

// TestFilePickerGolden pins the picker's populated / filtered / empty
// states. The tree is a t.TempDir fixture and the View carries only
// the virtual "fixture/" label — never the absolute temp path.
func TestFilePickerGolden(t *testing.T) {
	t.Parallel()

	states := []struct {
		name  string
		fresh bool
		drive func(*FilePicker)
	}{
		// SetCursor(3): the .. row is index 0 now, keeping the selector
		// on a.json; goldens differ only by that row and the up hint.
		{"populated", false, func(p *FilePicker) { p.list.SetCursor(3) }},
		{"filtered", false, func(p *FilePicker) {
			p.Update(ch('/'))
			for _, c := range "json" {
				p.Update(ch(c))
			}
		}},
		{"empty", true, nil},
	}
	for _, prof := range goldenProfiles() {
		for _, st := range states {
			t.Run(st.name+"_"+prof.name, func(t *testing.T) {
				root := t.TempDir()
				if !st.fresh {
					root = pickFixture(t)
				}
				p := NewFilePicker(theme.NewWith(prof.prof, true), 44, 6,
					FilePickerOptions{
						Root: root, RootLabel: "fixture/",
						Selectable: func(n string) bool { return strings.HasSuffix(n, ".json") },
					})
				if st.drive != nil {
					st.drive(p)
				}
				got := p.View()
				if strings.Contains(got, root) {
					t.Fatalf("golden would carry the temp path:\n%s", got)
				}
				checkGolden(t, "picker_"+st.name+"_"+prof.name, got)
			})
		}
	}
}

// TestToastGolden pins a single success toast and a stacked
// info/success/error stack right-aligned to width 40.
func TestToastGolden(t *testing.T) {
	t.Parallel()

	tr := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, prof := range goldenProfiles() {
		t.Run("single_"+prof.name, func(t *testing.T) {
			m := NewToast(theme.NewWith(prof.prof, true))
			m.SetSize(40)
			m.Push("saved 2 key(s)", ToastSuccess, tr)
			checkGolden(t, "toast_single_"+prof.name, m.View())
		})
		t.Run("stacked_"+prof.name, func(t *testing.T) {
			m := NewToast(theme.NewWith(prof.prof, true))
			m.SetSize(40)
			m.Push("picked a.json", ToastInfo, tr)
			m.Push("saved 2 key(s)", ToastSuccess, tr.Add(time.Second))
			m.Push("spec unreadable", ToastError, tr.Add(2*time.Second))
			checkGolden(t, "toast_stacked_"+prof.name, m.View())
		})
	}
}

// TestGoldensAsciiArePlain re-asserts on the golden bytes themselves that
// every ascii golden is escape- and non-ASCII-free.
func TestGoldensAsciiArePlain(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"list_5x30", "list_empty", "table_44", "table_empty", "confirm",
		"picker_populated", "picker_filtered", "picker_empty", "toast_single", "toast_stacked",
	} {
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
