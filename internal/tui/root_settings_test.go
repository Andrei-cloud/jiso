// root_settings_test.go proves the SCR-512 root contract with a fake
// façade (no real user config above the seam): entry onto §L loads
// the snapshot off the UI thread; a committed field runs a one-key
// ApplySettings (valid → snapshot refresh shows the live value,
// invalid → inline per-field error and no change recorded); w opens
// the save overlay with the changed-keys diff; w inside it calls
// SaveSettings ONCE with the changed-only patch; Esc closes it and
// writes nothing; a stale seq (page left mid-apply) is dropped; `r`
// re-queries; and a nil App keeps the page in its empty state.
package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
)

// fakeSettings is the injectable §L façade: a virtual user config
// (values map) with call counters and recorded patches; it never
// touches disk.
type fakeSettings struct {
	mu sync.Mutex

	path      string
	rows      []app.SettingsRow
	applyErrs map[string]string
	saveErr   error
	values    map[string]string

	currentN, applyN, saveN int
	applyPatches            []map[string]string
	savePatch               map[string]string
}

func (f *fakeSettings) CurrentSettings() (app.SettingsView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentN++

	view := app.SettingsView{ConfigPath: f.path}
	for _, row := range f.rows {
		if v, ok := f.values[row.Key]; ok {
			row.Value = v
			row.Source = app.SourceSession
		}
		view.Rows = append(view.Rows, row)
	}

	return view, nil
}

func (f *fakeSettings) ApplySettings(_ context.Context, patch map[string]string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyN++
	f.applyPatches = append(f.applyPatches, patch)

	var errs map[string]string
	for key, value := range patch {
		if msg, bad := f.applyErrs[key]; bad {
			if errs == nil {
				errs = map[string]string{}
			}
			errs[key] = msg

			continue
		}
		if f.values == nil {
			f.values = map[string]string{}
		}
		f.values[key] = value
	}

	return errs
}

func (f *fakeSettings) SaveSettings(_ context.Context, patch map[string]string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveN++
	f.savePatch = patch
	if f.saveErr != nil {
		return "", f.saveErr
	}

	return f.path, nil
}

// row builds one façade row (key, value, source); output is the only
// non-live-safe §L key.
func row(key, value, source string) app.SettingsRow {
	return app.SettingsRow{Key: key, Value: value, Source: source, LiveSafe: key != app.SettingOutput}
}

// fakeSettingsFixture is the wireframe §L data in façade shapes.
func fakeSettingsFixture() *fakeSettings {
	f := &fakeSettings{
		path: "./user/config.yaml",
		rows: []app.SettingsRow{
			row(app.SettingReconnectAttempts, "3", "config"),
			row(app.SettingConnectTimeout, "5s", "default"),
			row(app.SettingTotalConnectTimeout, "10s", "default"),
			row(app.SettingResponseTimeout, "5s", "default"),
			row(app.SettingListenTimeout, "5m", "default"),
			row(app.SettingHex, "off", "config"),
			row(app.SettingVisaStationID, "001234", "session"),
			row(app.SettingTLSConfig, "./testdata/certs/tls_config.json", "config"),
			row(app.SettingSpec, "./specs/visa.json", "config"),
			row(app.SettingTxFile, "./transactions/pool.json", "config"),
			row(app.SettingDB, "./sessions.db", "session"),
			row(app.SettingOutput, "text", "default"),
		},
	}
	// The save-overlay diff's old side reads the file layer.
	for i := range f.rows {
		switch f.rows[i].Key {
		case app.SettingConnectTimeout:
			f.rows[i].FileValue, f.rows[i].FileSet = "5s", true
		case app.SettingHex:
			f.rows[i].FileValue, f.rows[i].FileSet = "false", true
		}
	}

	return f
}

type settingsTestRoot struct {
	m *RootModel
}

func newSettingsTestRoot(t *testing.T, fake *fakeSettings) *settingsTestRoot {
	t.Helper()

	r := &settingsTestRoot{m: NewRootModel(nil)}
	t.Setenv("JISO_ASCII", "")
	r.m.theme = theme.NewWith(colorprofile.ASCII, true)
	if fake != nil {
		r.m.settingsSrc = fake
	}
	r.pump(tea.WindowSizeMsg{Width: 120, Height: 40})

	return r
}

func (r *settingsTestRoot) pump(msg tea.Msg) { r.pumpN(msg, 0) }

// pumpN pumps msg (and results); when keep > 0 the first returned cmd
// is deferred and returned instead of being run.
func (r *settingsTestRoot) pumpN(msg tea.Msg, keep int) tea.Cmd {
	var deferred tea.Cmd
	queue := []tea.Msg{msg}
	for i := 0; i < 32 && len(queue) > 0; i++ {
		cmd := r.upd(queue[0])
		queue = queue[1:]
		if keep > 0 && deferred == nil && cmd != nil {
			deferred = cmd

			continue
		}
		queue = append(queue, flattenMsgs(cmd)...)
	}

	return deferred
}

func (r *settingsTestRoot) upd(msg tea.Msg) tea.Cmd {
	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		panic("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

// gotoPage jumps to §L through the palette resolution (no hotkey) and
// nudges the size.
func (r *settingsTestRoot) gotoPage() {
	r.pump(palette.GoToPageMsg{ID: pages.SettingsPageID})
	r.pump(tea.WindowSizeMsg{Width: 121, Height: 41})
}

func (r *settingsTestRoot) body() string { return r.m.View().Content }

func (r *settingsTestRoot) commit(key, value string) {
	r.pump(pages.SettingsCommitMsg{Key: key, Value: value})
}

func TestSettingsEntryLoadsSnapshot(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()

	body := r.body()
	for _, want := range []string{
		"SETTINGS", "reconnect-attempts", "3", "listen-timeout",
		"5m", "hex output", "off", "applies to next operation",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page body lacks %q", want)
		}
	}
	if fake.currentN != 1 {
		t.Fatalf("snapshot loads = %d, want 1", fake.currentN)
	}
	if fake.applyN != 0 || fake.saveN != 0 {
		t.Fatalf("entry must not apply or save: %d/%d", fake.applyN, fake.saveN)
	}
}

func TestSettingsCommitValidLiveApplies(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()

	r.commit(app.SettingConnectTimeout, "8s")

	if fake.applyN != 1 || len(fake.applyPatches[0]) != 1 {
		t.Fatalf("applyN = %d patches = %v, want one one-key patch", fake.applyN, fake.applyPatches)
	}
	if fake.applyPatches[0][app.SettingConnectTimeout] != "8s" {
		t.Fatalf("patch = %v", fake.applyPatches[0])
	}
	if fake.currentN != 2 {
		t.Fatalf("snapshot reloads = %d, want 2 (apply dirties the cache)", fake.currentN)
	}
	if body := r.body(); !strings.Contains(body, "8s") || strings.Contains(body, "invalid") {
		t.Fatalf("live value not visible:\n%s", body)
	}
}

func TestSettingsCommitInvalidShowsInlineError(t *testing.T) {
	fake := fakeSettingsFixture()
	fake.applyErrs = map[string]string{app.SettingConnectTimeout: "invalid duration \"bogus\""}
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()

	r.commit(app.SettingConnectTimeout, "bogus")

	if body := r.body(); !strings.Contains(body, "invalid duration") {
		t.Fatalf("inline error missing:\n%s", body)
	}
	// A rejected commit records no change: w only notes, never saves.
	r.pump(pages.SettingsSaveMsg{})
	if fake.saveN != 0 {
		t.Fatalf("saveN = %d, want 0", fake.saveN)
	}
	if body := r.body(); !strings.Contains(body, "no changes to save") {
		t.Fatalf("note missing:\n%s", body)
	}
}

func TestSettingsSaveOverlayWritesChangedOnly(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()

	r.commit(app.SettingConnectTimeout, "8s")
	r.commit(app.SettingHex, "on")
	r.pump(pages.SettingsSaveMsg{})

	body := r.body()
	for _, want := range []string{
		"SAVE USER CONFIG", "target: ./user/config.yaml",
		"connect-timeout: 5s -> 8s", "hex output: off -> on",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overlay lacks %q:\n%s", want, body)
		}
	}

	r.pump(pages.SettingsSaveConfirmMsg{})

	if fake.saveN != 1 {
		t.Fatalf("saveN = %d, want 1", fake.saveN)
	}
	if len(fake.savePatch) != 2 || fake.savePatch[app.SettingConnectTimeout] != "8s" ||
		fake.savePatch[app.SettingHex] != "on" {
		t.Fatalf("save patch = %v, want the two changed keys only", fake.savePatch)
	}
	if body := r.body(); !strings.Contains(body, "saved 2 key(s) to ./user/config.yaml") {
		t.Fatalf("success line missing:\n%s", body)
	}
}

func TestSettingsSaveCancelWritesNothing(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()

	r.commit(app.SettingConnectTimeout, "8s")
	r.pump(pages.SettingsSaveMsg{})
	if !strings.Contains(r.body(), "SAVE USER CONFIG") {
		t.Fatal("overlay must be open")
	}
	r.pump(pages.SettingsSaveCancelMsg{})

	if fake.saveN != 0 {
		t.Fatalf("saveN = %d, want 0 (cancel writes nothing)", fake.saveN)
	}
	if strings.Contains(r.body(), "SAVE USER CONFIG") {
		t.Fatal("overlay must close on Esc")
	}
}

func TestSettingsSaveFailureKeepsChanges(t *testing.T) {
	fake := fakeSettingsFixture()
	fake.saveErr = context.DeadlineExceeded
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()

	r.commit(app.SettingConnectTimeout, "8s")
	r.pump(pages.SettingsSaveMsg{})
	r.pump(pages.SettingsSaveConfirmMsg{})

	if body := r.body(); !strings.Contains(body, "save failed") {
		t.Fatalf("failure line missing:\n%s", body)
	}
	// The change is still pending: w reopens the overlay.
	r.pump(pages.SettingsSaveMsg{})
	if !strings.Contains(r.body(), "connect-timeout: 5s -> 8s") {
		t.Fatal("changes must stay pending after a failed save")
	}
}

func TestSettingsStaleApplyDropped(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()

	deferred := r.pumpN(pages.SettingsCommitMsg{Key: app.SettingConnectTimeout, Value: "8s"}, 1)
	r.pump(palette.GoToPageMsg{ID: pages.DashboardPageID}) // leave mid-apply
	if deferred != nil {
		r.pump(deferred()) // stale result arrives
	}
	r.gotoPage()

	if fake.saveN != 0 {
		t.Fatalf("saveN = %d, want 0", fake.saveN)
	}
	if body := r.body(); strings.Contains(body, "SAVE USER CONFIG") {
		t.Fatalf("stale apply must not record a change:\n%s", body)
	}
}

func TestSettingsRefreshReloads(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()
	before := fake.currentN

	r.pump(pages.SettingsRefreshMsg{})

	if fake.currentN != before+1 {
		t.Fatalf("snapshot loads = %d, want %d", fake.currentN, before+1)
	}
}

func TestSettingsNoAppEmptyState(t *testing.T) {
	r := newSettingsTestRoot(t, nil)
	r.gotoPage()

	if body := r.body(); !strings.Contains(body, "SETTINGS") {
		t.Fatalf("empty-state body lacks the title:\n%s", body)
	}
}

// --- E5-FIX/M3 regression tests --------------------------------------

// TestSettingsStaleLoadClearsWaitAndReArms: a commit bumps the seq
// while the entry snapshot load is in flight; the stale result must
// clear settingsLoadWait so the wrapper re-arms — the old stale-return
// froze armSettings for the session (the §K ctfListWait wedge).
func TestSettingsStaleLoadClearsWaitAndReArms(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)

	loadCmd := r.pumpN(palette.GoToPageMsg{ID: pages.SettingsPageID}, 1) // arm & hold
	if loadCmd == nil {
		t.Fatal("entry must arm the snapshot load")
	}
	r.pump(tea.WindowSizeMsg{Width: 121, Height: 41})
	if !r.m.settingsLoadWait {
		t.Fatal("snapshot load must be in flight")
	}

	r.commit(app.SettingReconnectAttempts, "7") // bumps the seq; apply lands
	if fake.applyN != 1 {
		t.Fatalf("applyN = %d, want 1", fake.applyN)
	}
	if fake.currentN != 0 {
		t.Fatalf("a second load armed while the first was still awaited: currentN = %d", fake.currentN)
	}

	r.pump(loadCmd()) // the now-stale load must NOT wedge the loader
	// 2 = the stale leg's own call + the wrapper's re-arm (the wedge
	// fix: the old stale-return left settingsLoadWait true and the
	// re-arm never ran).
	if fake.currentN != 2 {
		t.Fatalf("snapshot calls = %d, want the stale leg + re-arm (2)", fake.currentN)
	}
	if r.m.settingsView == nil {
		t.Fatal("re-armed snapshot never landed")
	}
}

// TestSettingsPopBumpsSeqAndDropsStaleLeg: Esc-pop must run
// leaveSettings (the analyze abort precedent) — without the bump an
// in-flight leg lands on the page the user left.
func TestSettingsPopBumpsSeqAndDropsStaleLeg(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)

	r.m.Push(r.m.settings) // depth 2, §L current
	loadCmd := r.pumpN(tea.WindowSizeMsg{Width: 121, Height: 41}, 1)
	if loadCmd == nil {
		t.Fatal("pushed §L must arm its snapshot load")
	}
	seqBefore := r.m.settingsSeq

	r.pump(pages.SettingsPopMsg{})
	if r.m.settingsSeq == seqBefore {
		t.Fatal("Esc-pop must bump the seq (leaveSettings)")
	}
	wantStack(t, r.m, "dashboard")

	r.pump(loadCmd()) // stale: must not mutate state on the page left
	if r.m.settingsLoaded || r.m.settingsView != nil {
		t.Fatalf("stale snapshot folded after pop: loaded=%v view=%v", r.m.settingsLoaded, r.m.settingsView)
	}
}

// TestSettingsSaveConfirmWaitsForApply: w inside the save overlay while
// an apply leg is in flight must be ignored — the just-committed key
// has not landed in settingsChanged yet and would miss the save.
func TestSettingsSaveConfirmWaitsForApply(t *testing.T) {
	fake := fakeSettingsFixture()
	r := newSettingsTestRoot(t, fake)
	r.gotoPage()
	r.commit(app.SettingReconnectAttempts, "7") // lands: changed recorded

	// Open the overlay, then arm an apply leg and hold it.
	r.pump(pages.SettingsSaveMsg{})
	if !r.m.settingsSaveOpen {
		t.Fatal("w must open the save overlay")
	}
	applyCmd := r.pumpN(pages.SettingsCommitMsg{Key: app.SettingConnectTimeout, Value: "9s"}, 1)
	if applyCmd == nil || !r.m.settingsApplyWait {
		t.Fatal("apply leg not in flight")
	}

	r.pump(pages.SettingsSaveConfirmMsg{}) // w inside the overlay
	if fake.saveN != 0 {
		t.Fatal("save must wait for the in-flight apply (would miss the just-committed key)")
	}
	if applyCmd == nil {
		t.Fatal("apply cmd missing")
	}
	r.pump(applyCmd()) // the apply lands; save can proceed
	r.pump(pages.SettingsSaveConfirmMsg{})
	if fake.saveN != 1 {
		t.Fatalf("saveN = %d, want 1 after the apply landed", fake.saveN)
	}
	if _, ok := fake.savePatch[app.SettingConnectTimeout]; !ok {
		t.Fatalf("save patch lost the just-committed key: %v", fake.savePatch)
	}
}
