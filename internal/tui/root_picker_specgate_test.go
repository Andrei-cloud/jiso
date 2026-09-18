// root_picker_specgate_test.go pins the load-time spec prompt: a tx file
// whose entries declare no specification, picked while no explicit spec is
// set, must not silently bind to the engine default — the shared picker
// re-opens for a specification file with the pick pending. Esc drops the
// pending file (nothing half-applies) and says so; a spec pick applies
// BOTH keys in one settings apply. Files whose entries all declare specs,
// and picks under an explicit spec, stay prompt-free.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/widgets"
)

// gateSpeclessJSON is the candidate file: every entry a specless transaction.
const gateSpeclessJSON = `[{"type":"transaction","name":"Echo","description":"Network Management: Echo","fields":{"0":"0800"}}]`

// specGateRoot feeds the root like the program's command runner: each
// returned cmd runs (batches unpacked) and its result msg re-enters.
type specGateRoot struct {
	t *testing.T
	m *RootModel
}

func (r *specGateRoot) upd(msg tea.Msg) tea.Cmd {
	r.t.Helper()

	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		r.t.Fatal("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

func (r *specGateRoot) pump(msg tea.Msg) {
	r.t.Helper()

	queue := []tea.Msg{msg}
	for i := 0; i < 64 && len(queue) > 0; i++ {
		cmd := r.upd(queue[0])
		queue = queue[1:]
		queue = append(queue, specGateResults(cmd)...)
	}
}

// run feeds one msg and runs its cmd once, returning the result (the
// picker's select cmd yields the msg the root folds next).
func (r *specGateRoot) run(msg tea.Msg) tea.Msg {
	r.t.Helper()

	msgs := specGateResults(r.upd(msg))
	if len(msgs) == 0 {
		return nil
	}

	return msgs[0]
}

// specGateResults runs cmd once and unpacks its result (BatchMsg included).
func specGateResults(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch v := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, sub := range v {
			out = append(out, specGateResults(sub)...)
		}

		return out
	case []tea.Msg:
		return v
	default:
		return []tea.Msg{v}
	}
}

// specGateFix is one temp dir holding: old.json (the loaded tx file),
// txload.json (specless candidate), all.json (every entry declares a
// spec), chosen.json (a flex-spec copy to pick).
type specGateFix struct {
	r      *specGateRoot
	dir    string
	old    string
	txload string
	all    string
	chosen string
}

// newSpecGateFix builds a real app: with keepSpec over old.json plus an
// explicit spec; otherwise built spec-less (the engine-default signal) and
// the old file dropped in afterwards.
func newSpecGateFix(t *testing.T, keepSpec bool) *specGateFix {
	t.Helper()

	t.Setenv("JISO_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	f := &specGateFix{dir: dir}
	f.old = filepath.Join(dir, "old.json")
	f.txload = filepath.Join(dir, "txload.json")
	f.all = filepath.Join(dir, "all.json")
	f.chosen = filepath.Join(dir, "chosen.json")

	flex, err := os.ReadFile(filepath.Join("..", "..", "specs", "flex.json"))
	if err != nil {
		t.Fatalf("read flex spec: %v", err)
	}
	write := func(path, body string) {
		t.Helper()

		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(f.chosen, string(flex))
	write(f.old, txFixtureJSON)
	write(f.txload, gateSpeclessJSON)
	write(f.all, fmt.Sprintf(
		`[{"type":"transaction","name":"Whole","description":"d","fields":{"0":"0800"},"spec":%q}]`, f.chosen))

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	if keepSpec {
		cfg.SetSpec(filepath.Join("..", "..", "specs", "spec.json"))
		cfg.SetFile(f.old)
	}
	// The dropped-spec variant starts New with neither: the startup guard
	// refuses file-without-spec, and SetSpec("") is ignored (a spec is set
	// once), so the old file lands after construction as the pre-pick value.

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	time.Sleep(20 * time.Millisecond) // let app construction goroutines settle

	if !keepSpec {
		a.Config().SetFile(f.old)
	}

	r := &specGateRoot{t: t, m: NewRootModel(a)}
	r.m.toastTickf = func(time.Duration, func() tea.Msg) tea.Cmd { return nil }
	r.pump(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.pump(ch('2')) // §B
	f.r = r

	return f
}

// gateOpen drives the real §B flow into the prompt: `f` opens the picker
// on the fixture dir, keys land on txload.json (dirs sort: ../, all.json,
// chosen.json, old.json, txload.json), and the specless pick re-opens
// the browse for a specification file.
func (f *specGateFix) gateOpen() {
	f.r.pump(pages.TxPickFileMsg{})
	for i := 0; i < 4; i++ {
		f.r.pump(ch('j'))
	}
	f.r.pump(special(tea.KeyEnter))
}

func TestRootTxPickGatesSpeclessFileWithSpecPrompt(t *testing.T) {
	f := newSpecGateFix(t, false)
	f.r.pump(pages.TxPickFileMsg{})
	for i := 0; i < 4; i++ {
		f.r.pump(ch('j')) // down to txload.json
	}
	f.r.pump(special(tea.KeyEnter))

	m := f.r.m
	if m.filePick == nil {
		t.Fatal("a specless pick with no explicit spec must re-open the picker for a specification")
	}
	if m.filePickTarget != "settings:spec-for-file" {
		t.Fatalf("picker target = %q, want settings:spec-for-file", m.filePickTarget)
	}
	if m.pendingTxFile != f.txload {
		t.Fatalf("pending file = %q, want %q", m.pendingTxFile, f.txload)
	}
	if got := m.app.Config().GetFile(); got != f.old {
		t.Fatalf("cfg.GetFile() = %q, must still hold the old file", got)
	}
	if got := m.filePick.CurrentDir(); got != f.dir {
		t.Fatalf("spec browse start dir = %q, want the pending file's dir %q", got, f.dir)
	}
	if m.toast != nil && m.toast.Len() != 0 {
		t.Error("gating alone must not toast")
	}
	if body := m.View().Content; !strings.Contains(body, "chosen.json") {
		t.Errorf("the spec browse must be visible:\n%s", body)
	}
}

func TestRootTxSpecPromptPickAppliesBothKeysOnce(t *testing.T) {
	f := newSpecGateFix(t, false)
	f.gateOpen()
	// chosen.json is listed in the open spec browse; injecting the pick
	// lets the apply leg be inspected before it folds.
	cmd := f.r.upd(widgets.FilePickedMsg{Path: f.chosen})
	msgs := specGateResults(cmd)
	if len(msgs) != 1 {
		t.Fatalf("the spec pick must return just the apply leg, got %d msgs", len(msgs))
	}
	applied, ok := msgs[0].(settingsAppliedMsg)
	if !ok {
		t.Fatalf("spec pick did not take the settings apply leg: %T", msgs[0])
	}
	if len(applied.patch) != 2 || applied.patch[app.SettingSpec] != f.chosen || applied.patch[app.SettingTxFile] != f.txload {
		t.Fatalf("spec pick must apply BOTH keys in one patch, got %v", applied.patch)
	}
	f.r.pump(applied)

	cfg := f.r.m.app.Config()
	if got := cfg.GetSpec(); got != f.chosen {
		t.Fatalf("cfg.GetSpec() = %q, want the picked spec", got)
	}
	if got := cfg.GetFile(); got != f.txload {
		t.Fatalf("cfg.GetFile() = %q, want the pending tx file", got)
	}
	if names := f.r.m.app.Transactions().ListNames(); len(names) != 1 || names[0] != "Echo" {
		t.Fatalf("live transactions = %v, want the pending file loaded", names)
	}
	if f.r.m.filePick != nil || f.r.m.pendingTxFile != "" {
		t.Fatal("a completed spec pick must close the browse and clear the pending file")
	}
	if top := strings.SplitN(f.r.m.View().Content, "\n", 2)[0]; !strings.Contains(top, "spec chosen.json") {
		t.Errorf("the chip must carry the chosen spec base:\n%s", top)
	}
}

func TestRootTxSpecPromptEscDropsWithoutHalfApply(t *testing.T) {
	f := newSpecGateFix(t, false)
	f.gateOpen()
	f.r.pump(special(tea.KeyEscape))

	m := f.r.m
	if m.filePick != nil {
		t.Fatal("esc must close the spec browse")
	}
	if m.pendingTxFile != "" {
		t.Fatalf("esc must drop the pending file, kept %q", m.pendingTxFile)
	}
	cfg := m.app.Config()
	if got := cfg.GetFile(); got != f.old {
		t.Fatalf("esc must not half-apply: cfg.GetFile() = %q, want %q", got, f.old)
	}
	if got := cfg.GetSpec(); got != "" {
		t.Fatalf("esc must not apply a spec either: cfg.GetSpec() = %q", got)
	}
	if m.toast == nil || m.toast.Len() != 1 {
		t.Fatal("the cancel must surface a visible one-line notice")
	}
	notice := strings.Join(m.toast.Lines(), "\n")
	if !strings.Contains(notice, "transaction file not loaded") || !strings.Contains(notice, "pick a specification file first") {
		t.Fatalf("notice text = %q", notice)
	}

	// `f` re-arms the ordinary tx-file browse afterwards.
	f.r.pump(pages.TxPickFileMsg{})
	if m = f.r.m; m.filePick == nil || m.filePickTarget != app.SettingTxFile {
		t.Fatal("f must re-arm the ordinary tx-file picker after the cancel")
	}
}

func TestRootTxPickAllDeclaredSpecsLoadSilently(t *testing.T) {
	f := newSpecGateFix(t, false)
	f.r.pump(pages.TxPickFileMsg{}) // the browse opens on the fixture dir

	cmd := f.r.upd(widgets.FilePickedMsg{Path: f.all})
	applied, ok := cmd().(settingsAppliedMsg)
	if !ok {
		t.Fatalf("a fully specified file must apply on the spot: %T", cmd())
	}
	if len(applied.patch) != 1 || applied.patch[app.SettingTxFile] != f.all {
		t.Fatalf("silent load must be a one-key tx-file patch, got %v", applied.patch)
	}
	f.r.pump(applied)

	m := f.r.m
	if got := m.app.Config().GetFile(); got != f.all {
		t.Fatalf("cfg.GetFile() = %q, want the picked file", got)
	}
	if m.filePick != nil || m.pendingTxFile != "" {
		t.Fatal("per-entry specs must not prompt for a spec")
	}
	if m.toast != nil && m.toast.Len() != 0 {
		t.Error("the silent load must stay quiet")
	}
}

func TestRootTxPickWithExplicitSpecLoadsSpeclessSilently(t *testing.T) {
	f := newSpecGateFix(t, true) // the explicit spec stays set
	f.r.pump(pages.TxPickFileMsg{})
	for i := 0; i < 4; i++ {
		f.r.pump(ch('j')) // down to txload.json
	}
	picked, ok := f.r.run(special(tea.KeyEnter)).(widgets.FilePickedMsg)
	if !ok {
		t.Fatal("the key walk must land a pick on txload.json")
	}

	cmd := f.r.upd(picked)
	applied, ok := cmd().(settingsAppliedMsg)
	if !ok {
		t.Fatalf("an explicit spec must let the specless file load: %T", cmd())
	}
	if len(applied.patch) != 1 {
		t.Fatalf("the user already chose a spec, patch must stay one-key: %v", applied.patch)
	}
	f.r.pump(applied)

	m := f.r.m
	if got := m.app.Config().GetFile(); got != f.txload {
		t.Fatalf("cfg.GetFile() = %q, want the picked file", got)
	}
	if m.filePick != nil || m.pendingTxFile != "" {
		t.Fatal("an explicit spec must not prompt again")
	}
}

// TestRootSettingsTxPickGatesSpecPrompt: the §L path shares the pick-apply
// gate (same target arm) — pick a specless tx file from the settings grid
// and the spec browse opens pending; the spec pick patches BOTH keys
// through one apply.
func TestRootSettingsTxPickGatesSpecPrompt(t *testing.T) {
	f := newSpecGateFix(t, false)
	fake := fakeSettingsFixture()
	f.r.m.settingsSrc = fake
	f.r.pump(palette.GoToPageMsg{ID: pages.SettingsPageID})

	f.r.pump(pages.SettingsPickFileMsg{Key: app.SettingTxFile})
	if f.r.m.filePick == nil {
		t.Fatal("f on the §L tx-file row must open the picker")
	}
	f.r.pump(widgets.FilePickedMsg{Path: f.txload})

	m := f.r.m
	if m.filePick == nil || m.filePickTarget != "settings:spec-for-file" {
		t.Fatalf("§L pick must gate into the spec browse, target = %q", m.filePickTarget)
	}
	if m.pendingTxFile != f.txload {
		t.Fatalf("pending file = %q, want %q", m.pendingTxFile, f.txload)
	}
	if got := m.app.Config().GetFile(); got != f.old {
		t.Fatalf("cfg.GetFile() = %q, must still hold the old file", got)
	}

	f.r.pump(widgets.FilePickedMsg{Path: f.chosen})
	if fake.applyN != 1 {
		t.Fatalf("spec pick must run ONE settings apply, got %d", fake.applyN)
	}
	if patch := fake.applyPatches[0]; len(patch) != 2 || patch[app.SettingSpec] != f.chosen || patch[app.SettingTxFile] != f.txload {
		t.Fatalf("spec pick must apply BOTH keys in one patch, got %v", patch)
	}
}
