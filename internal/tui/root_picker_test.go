// root_picker_test.go proves the TUI-406b root seam: pages.SettingsPickFileMsg
// (f on a §L path row) opens the shared widgets.FilePicker as a
// root-owned modal — it owns the keyboard over the page (q/:/? reach the
// picker, Esc closes it back to §L), the View carries only the virtual
// fixture label — a selection returns as a FilePickedMsg whose Path is
// committed through the settings commit seam (validation included);
// and the §L save-success line also surfaces as a toast that a ticked
// age prunes (the widget never reads the clock; now/toastTickf are
// injected).
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// pickRootFixture builds specs/visa.json, a.json, b.txt under t.TempDir.
func pickRootFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"specs/visa.json", "a.json", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestRootPickerOpensAndOwnsEsc(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()
	dir := pickRootFixture(t)
	r.m.filePickRootFn = func(string, string) (string, string) { return dir, "fixture/" }

	r.pump(pages.SettingsPickFileMsg{Key: app.SettingDB})

	body := r.body()
	for _, want := range []string{"fixture/", "specs", "a.json", "b.txt"} {
		if !strings.Contains(body, want) {
			t.Errorf("picker body lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, dir) {
		t.Fatal("absolute temp path leaked into the body")
	}
	// The picker owns the keyboard over the page: q/:/? are swallowed.
	for _, c := range []rune{'q', ':', '?'} {
		r.pump(ch(c))
		if !strings.Contains(r.body(), "fixture/") {
			t.Fatalf("%q escaped the picker", c)
		}
	}
	r.pump(special(tea.KeyEscape))
	if body := r.body(); strings.Contains(body, "fixture/") || !strings.Contains(body, "SETTINGS") {
		t.Fatalf("Esc must close the picker back to §L:\n%s", body)
	}
}

func TestRootPickerSelectCommitsChosenPath(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()
	dir := pickRootFixture(t)
	r.m.filePickRootFn = func(string, string) (string, string) { return dir, "fixture/" }

	r.pump(pages.SettingsPickFileMsg{Key: app.SettingSpec})
	// Entries: specs/ (dir), a.json (.json), b.txt (unselectable).
	r.pump(ch('j')) // cursor onto a.json
	r.pump(ch('j')) // b.txt: enter below must be inert first... move back
	r.pump(ch('k'))
	r.pump(special(tea.KeyEnter))

	var got string
	for _, p := range fake.applyPatches {
		if v, ok := p[app.SettingSpec]; ok {
			got = v
		}
	}
	if want := filepath.Join(dir, "a.json"); got != want {
		t.Fatalf("commit patch spec = %q, want %q (patches %v)", got, want, fake.applyPatches)
	}
	if body := r.body(); strings.Contains(body, "fixture/") {
		t.Fatalf("picker must close after a selection:\n%s", body)
	}
}

func TestRootPickerUnselectableEnterInert(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()
	dir := pickRootFixture(t)
	r.m.filePickRootFn = func(string, string) (string, string) { return dir, "fixture/" }

	r.pump(pages.SettingsPickFileMsg{Key: app.SettingSpec})
	r.pump(ch('G')) // b.txt (last; .json predicate rejects it)
	r.pump(special(tea.KeyEnter))
	if fake.applyN != 0 {
		t.Fatalf("unselectable selection applied: %v", fake.applyPatches)
	}
	if !strings.Contains(r.body(), "fixture/") {
		t.Fatal("picker must stay open after an inert enter")
	}
}

func TestRootToastAppearsThenPrunesOnTickedAge(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	r.m.now = func() time.Time { return base }
	var tickFn func() tea.Msg
	r.m.toastTickf = func(time.Duration, func() tea.Msg) tea.Cmd {
		if tickFn == nil { // keep the first handle; re-arms would overwrite it
			tickFn = func() tea.Msg { return toastTickMsg{} }
		}

		return nil
	}

	r.commit(app.SettingConnectTimeout, "8s")
	r.pump(pages.SettingsSaveMsg{})
	r.pump(pages.SettingsSaveConfirmMsg{})

	line := "saved 1 key(s) to ./user/config.yaml"
	if body := r.body(); strings.Count(body, line) != 2 {
		t.Fatalf("save line + toast expected twice:\n%s", body)
	}
	if tickFn == nil {
		t.Fatal("toast tick must be armed while a toast is visible")
	}

	// Age the toast out with the injected clock, then deliver the tick.
	r.m.now = func() time.Time { return base.Add(2 * toastDefaultTTL) }
	r.pump(tickFn())

	if body := r.body(); strings.Count(body, line) != 1 {
		t.Fatalf("toast must be pruned (page line stays):\n%s", body)
	}
}

// --- E5-FIX/M6 regression tests --------------------------------------

// TestRootTxPickFileOpensPickerAndCommitsPath: `t` on §B (the logged
// no-op the §B empty state and the §M registry advertise) opens the
// shared picker through the OpenFilePickerMsg seam with the .json
// filter, and a selection commits the tx-file path through the same
// settings commit path §L uses.
func TestRootTxPickFileOpensPickerAndCommitsPath(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	fake := fakeSettingsFixture()
	m.settingsSrc = fake
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2')) // §B

	dir := pickRootFixture(t)
	m.filePickRootFn = func(key, value string) (string, string) {
		if key != app.SettingTxFile {
			t.Errorf("picker root asked for key %q, want tx-file", key)
		}

		return dir, "fixture/"
	}

	_, cmd := m.Update(pages.TxPickFileMsg{})
	if cmd != nil {
		t.Fatalf("opening the picker must yield no cmd, got %v", cmd)
	}
	body := m.View().Content
	for _, want := range []string{"fixture/", "a.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("picker body lacks %q:\n%s", want, body)
		}
	}
	// The picker's design keeps unselectable files visible but dim;
	// the .json filter's bite is that Enter on b.txt must not pick it.
	if !strings.Contains(body, "a.json") {
		t.Errorf("picker body lacks a.json:\n%s", body)
	}

	_, cmd = m.Update(widgets.FilePickedMsg{Path: filepath.Join(dir, "a.json")})
	if cmd == nil {
		t.Fatal("a selection must arm the §L commit leg")
	}
	applied, ok := cmd().(settingsAppliedMsg)
	if !ok {
		t.Fatalf("selection did not take the settings apply leg: %T", cmd())
	}
	if got := applied.patch[app.SettingTxFile]; got != filepath.Join(dir, "a.json") {
		t.Fatalf("commit patch tx-file = %q", got)
	}
	if body := m.View().Content; strings.Contains(body, "fixture/") {
		t.Fatalf("picker must close after a selection:\n%s", body)
	}
}

// TestRootTxPickFileLoadErrorSurfacesOnB: a tx-file picked from §B that the
// app REJECTS must surface the reason on the transactions page, not fall back
// to the empty state in silence (UAT round 7).
func TestRootTxPickFileLoadErrorSurfacesOnB(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	fake := fakeSettingsFixture()
	fake.applyErrs = map[string]string{app.SettingTxFile: "transaction file rejected: field 22 is too short"}
	m.settingsSrc = fake
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2')) // §B

	dir := pickRootFixture(t)
	m.filePickRootFn = func(key, value string) (string, string) { return dir, "fixture/" }

	_, _ = m.Update(pages.TxPickFileMsg{}) // open picker (marks the pick as from §B)
	_, cmd := m.Update(widgets.FilePickedMsg{Path: filepath.Join(dir, "a.json")})
	if cmd == nil {
		t.Fatal("a selection must arm the commit leg")
	}
	applied, ok := cmd().(settingsAppliedMsg)
	if !ok {
		t.Fatalf("selection did not take the settings apply leg: %T", cmd())
	}
	_, _ = m.Update(applied) // fold the rejected load

	if body := m.View().Content; !strings.Contains(body, "transaction file rejected") {
		t.Errorf("the rejected tx-file must surface its reason on §B:\n%s", body)
	}
}

// TestToastDefaultTTLPinned: the wireframe pins the toast age to 3s
// (the old default outlived it).
func TestToastDefaultTTLPinned(t *testing.T) {
	t.Parallel()

	if toastDefaultTTL != 3*time.Second {
		t.Fatalf("toastDefaultTTL = %v, want 3s (wireframe)", toastDefaultTTL)
	}
	m := NewRootModel(nil)
	if m.toastTTL != toastDefaultTTL {
		t.Fatalf("root toastTTL = %v, want the default %v", m.toastTTL, toastDefaultTTL)
	}
}
