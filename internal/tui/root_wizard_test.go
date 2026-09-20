// root_wizard_test.go pins the root-side send-wizard wiring (proposal
// 04 §B): the step shape follows connection liveness, a missing tx file
// stays inline, and the final pick commits settings and starts the send.
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/widgets"
)

func wizardTestRoot(t *testing.T) *disconnectTestRoot {
	t.Helper()

	r := newDisconnectTestRoot(t)
	r.m.app = newTxFileApp(t)

	return r
}

func TestRootWizardOfflineShape(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(palette.OpenSendWizardMsg{})

	if m.wizard == nil {
		t.Fatal("OpenSendWizardMsg must open the wizard")
	}
	st := m.wizard.State()
	if len(st.Steps) != 4 || st.Steps[0] != pages.WizardStepConnect {
		t.Fatalf("offline steps %v, want [connect spec file send]", st.Steps)
	}
	if m.wizard.ConnectForm() == nil {
		t.Fatal("the connect step needs the connect form wired")
	}
	if !strings.Contains(m.View().Content, "1 connect") {
		t.Errorf("frame lacks the wizard rail:\n%s", m.View().Content)
	}
}

func TestRootWizardLiveSkipsConnectStep(t *testing.T) {
	r := wizardTestRoot(t)
	r.connect()
	r.upd(palette.OpenSendWizardMsg{})

	if r.m.wizard == nil {
		t.Fatal("wizard must open")
	}
	st := r.m.wizard.State()
	if len(st.Steps) != 1 || st.Steps[0] != pages.WizardStepSend {
		t.Fatalf("live steps %v, want [send] (a fully-resolved live session asks for nothing but the send)", st.Steps)
	}
	if !st.TargetOK {
		t.Fatal("a live connection must mark the target ok")
	}
}

func TestRootWizardMissingFileStaysInline(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})
	_, _ = r.m.Update(pages.WizardChooseFileMsg{Path: "/definitely/missing.json"})

	if r.m.wizard == nil {
		t.Fatal("a missing file must keep the wizard open")
	}
	if err := r.m.wizard.State().Error; !strings.Contains(err, "no such file") {
		t.Fatalf("inline error = %q, want \"no such file: …\"", err)
	}
}

func TestRootWizardSendCommitsAndStarts(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})

	other := t.TempDir() + "/pool2.json"
	if err := os.WriteFile(other, []byte(txFixtureJSON), 0o600); err != nil {
		t.Fatalf("write second tx file: %v", err)
	}
	r.m.wizardSpec = "../../specs/spec.json"
	r.m.wizardFile = other

	cmd := r.upd(pages.WizardSendMsg{Name: "Purchase"})
	if cmd != nil {
		_, _ = r.m.Update(cmd()) // let the send page command run
	}
	if r.m.wizard != nil {
		t.Fatal("a committed send must close the wizard")
	}
	if r.m.sendRun == nil {
		t.Fatal("a committed send must start the send run")
	}
	if got := r.m.app.Config().GetFile(); got != other {
		t.Fatalf("commit did not persist the tx file: %q, want %q", got, other)
	}
}

// TestWizardConnectAutoSendsPreset pins the UAT law: "s" on a transaction
// in §B arms that transaction; when the wizard's connect step succeeds the
// send fires — the operator is never asked to choose the transaction they
// already chose.
func TestWizardConnectAutoSendsPreset(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(pages.TxSendMsg{ID: "Purchase"})
	if r.m.wizard == nil {
		t.Fatal("offline s must open the wizard")
	}
	if r.m.wizardPresetTx != "Purchase" {
		t.Fatalf("preset = %q, want Purchase", r.m.wizardPresetTx)
	}

	// The wizard-hosted connect step's terminal success verdict.
	r.m.connectRun = &connectRun{cancel: func() {}, total: 1}
	r.m.connectHost = r.m.wizard.ConnectForm()
	r.m.connectInitiated = true
	cmd := r.upd(ConnectResultMsg{OK: true, Target: "127.0.0.1:9999"})
	if cmd != nil {
		_, _ = r.m.Update(cmd()) // let the armed send command run
	}

	if r.m.wizard != nil {
		t.Error("the fired send must close the wizard (no picker afterwards)")
	}
	if r.m.sendRun == nil {
		t.Error("connect success with an armed preset must start that transaction's send")
	}
	if r.m.wizardPresetTx != "" {
		t.Error("the preset must be consumed by the fire")
	}
}

// The auto-fire only trusts the preset while it still names a loaded
// template; a preset that no longer resolves leaves the picker in place
// rather than sending the wrong thing or nothing at all.
func TestWizardConnectPresetVanishedKeepsPicker(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(pages.TxSendMsg{ID: "Purchase"})
	r.m.wizardPresetTx = "No Such Transaction"

	r.m.connectRun = &connectRun{cancel: func() {}, total: 1}
	r.m.connectHost = r.m.wizard.ConnectForm()
	r.m.connectInitiated = true
	r.upd(ConnectResultMsg{OK: true, Target: "127.0.0.1:9999"})

	if r.m.wizard == nil {
		t.Fatal("a vanished preset must not close the wizard")
	}
	if r.m.sendRun != nil {
		t.Error("a vanished preset must not start a send")
	}
	if r.m.wizardPresetTx != "" {
		t.Error("the vanished preset must be cleared, not retried later")
	}
}

func TestRootWizardBrowseOpensPickerAndFeedsBack(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})
	r.upd(pages.WizardBrowseMsg{IsSpec: true})

	if r.m.filePick == nil {
		t.Fatal("[f] must open the shared file picker over the wizard")
	}
	if r.m.filePickTarget != wizardPickSpecTarget {
		t.Fatalf("picker target = %q, want %q", r.m.filePickTarget, wizardPickSpecTarget)
	}
	if !strings.Contains(r.m.View().Content, "jiso") {
		t.Fatal("picker must render over the wizard")
	}

	r.upd(widgets.FilePickerCanceledMsg{})
	if r.m.filePick != nil {
		t.Fatal("esc must close the picker")
	}
	if r.m.wizard == nil {
		t.Fatal("the wizard must survive the cancelled browse")
	}

	// Re-browse and pick a row (the realistic sequence: a pick only
	// ever arrives while the picker is open).
	r.upd(pages.WizardBrowseMsg{IsSpec: true})
	spec := "../../specs/spec.json"
	r.upd(widgets.FilePickedMsg{Path: spec})
	if r.m.filePick != nil {
		t.Fatal("a selection must close the picker")
	}
	if r.m.wizardSpec != spec {
		t.Fatalf("wizardSpec = %q, want the picked path", r.m.wizardSpec)
	}
	if r.m.wizard == nil || r.m.wizard.Step() != 1 {
		t.Fatal("a spec selection must advance the wizard to the file step")
	}
}

func TestRootWizardCancelCloses(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})
	r.upd(pages.WizardCancelMsg{})

	if r.m.wizard != nil {
		t.Fatal("WizardCancelMsg must close the wizard")
	}
	wantStack(t, r.m, "dashboard")
}

var _ = app.SettingSpec

// TestWizardDirItemsCurrentFirst pins the ordering fix: Enter with no
// navigation on the spec step must keep the live spec, so the item tagged
// current leads the list regardless of alphabetical position.
func TestWizardDirItemsCurrentFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, n := range []string{"example_composed_emv.json", "flex.json", "visa.json"} {
		if err := os.WriteFile(dir+"/"+n, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	items := wizardSpecItems(dir + "/flex.json")
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if !items[0].Current || items[0].Label != "flex.json" {
		t.Fatalf("first item = %q current=%v, want flex.json current", items[0].Label, items[0].Current)
	}
	if items[1].Label != "example_composed_emv.json" || items[2].Label != "visa.json" {
		t.Fatalf("rest = %q, %q, want alphabetical", items[1].Label, items[2].Label)
	}
}

// TestRootWizardSpecStepOpensOnCurrent pins that the opened wizard's
// cursor is on the current spec, so Enter keeps the live spec instead of
// committing the alphabetically-first file in specs/.
func TestRootWizardSpecStepOpensOnCurrent(t *testing.T) {
	r := wizardTestRoot(t)
	specsDir, err := filepath.Abs("../../specs")
	if err != nil {
		t.Fatal(err)
	}
	r.m.configOrNil().SetSpec(specsDir + "/flex.json")
	if _, cmd := r.m.openWizard(); cmd != nil {
		t.Fatalf("openWizard cmd = %v, want nil", cmd)
	}
	w := r.m.wizard
	if w == nil {
		t.Fatal("wizard not open")
	}
	items := w.State().SpecItems
	if len(items) == 0 {
		t.Fatal("no spec items")
	}
	if items[0].Label != "flex.json" {
		t.Fatalf("first spec item = %q, want flex.json", items[0].Label)
	}
	// Enter on the spec step with no navigation must echo the live spec.
	next, _ := w.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = next
	if r.m.wizardSpec != specsDir+"/flex.json" {
		t.Fatalf("wizardSpec = %q, want the live flex.json", r.m.wizardSpec)
	}
}

// TestRootDirectSendStaysOnDashboard: DirectSendMsg with
// a live connection and a loaded spec + tx file sends immediately and
// keeps the operator on the dashboard (the LAST SEND tile carries the
// outcome); no wizard opens and the template is recorded for the next
// one-keystroke send.
func TestRootDirectSendStaysOnDashboard(t *testing.T) {
	r := wizardTestRoot(t)
	r.connect()
	r.upd(palette.DirectSendMsg{})

	if r.m.wizard != nil {
		t.Fatal("a configured session must send without opening the wizard")
	}
	if got := r.m.Current().ID(); got != pages.DashboardPageID {
		t.Fatalf("page = %q, want the dashboard (direct send must not jump to §D)", got)
	}
	if r.m.lastSentTemplate == "" {
		t.Fatal("the sent template must be recorded for the next direct send")
	}
}

// TestRootDirectSendFallsBackToWizard: offline, DirectSendMsg opens the
// wizard at the connect step — the one-keystroke path asks only for
// what is missing.
func TestRootDirectSendFallsBackToWizard(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(palette.DirectSendMsg{})

	if m.wizard == nil {
		t.Fatal("offline DirectSendMsg must fall back to the wizard")
	}
	if got := m.wizard.CurrentStepID(); got != pages.WizardStepConnect {
		t.Fatalf("step = %q, want connect", got)
	}
}

// TestRootWizardSpecFileRejected: a file without transactions (a spec)
// never advances the step — the file step keeps its cursor with a
// naming error line ("no templates" dead end on the send step).
func TestRootWizardSpecFileRejected(t *testing.T) {
	r := wizardTestRoot(t)
	cfg := r.m.app.Config()
	specPath := cfg.GetSpec()
	cfg.Reset()
	cfg.SetSpec(specPath)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	// No tx file: the wizard keeps the file step in the rail — the
	// UAT rule is to ask only for what is unresolved, and this one is.
	spec := t.TempDir() + "/flex.json"
	if err := os.WriteFile(spec, []byte(`{"name":"flex"}`), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	r.connect()
	r.upd(palette.OpenSendWizardMsg{})
	if got := r.m.wizard.CurrentStepID(); got != pages.WizardStepFile {
		t.Fatalf("step = %q, want file (the one unresolved choice opens first)", got)
	}
	_, _ = r.m.Update(pages.WizardChooseFileMsg{Path: spec})

	if r.m.wizard == nil {
		t.Fatal("the wizard must stay open")
	}
	if r.m.wizard.CurrentStepID() != pages.WizardStepFile {
		t.Fatalf("step = %q, want file (must not advance on a tx file with no transactions)", r.m.wizard.CurrentStepID())
	}
	if err := r.m.wizard.State().Error; !strings.Contains(err, "no transactions in flex.json") {
		t.Fatalf("inline error = %q, want \"no transactions in flex.json …\"", err)
	}
}

// TestRootWizardFileItemsAreTxFiles: the fallback candidate list only
// offers files that actually carry transactions (spec/lock files are
// noise; picking one was the dead end).
func TestRootWizardFileItemsAreTxFiles(t *testing.T) {
	t.Parallel()

	for _, it := range wizardFileItems(helpGoldenTheme(colorprofile.ASCII), "", nil) {
		if len(wizardTemplates(helpGoldenTheme(colorprofile.ASCII), it.Path)) == 0 {
			t.Fatalf("candidate %s carries no transactions", it.Path)
		}
	}
}

// a tx file that vanished between listing and apply opens the error
// screen naming the path (an explicit pick failure); the wizard step
// keeps its inline missing-file line and stays open.
func TestRootWizardMissingFileOpensModal(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})
	r.upd(pages.WizardChooseFileMsg{Path: "/definitely/missing.json"})

	if r.m.errModal == nil {
		t.Fatal("a vanished pick must open the error screen")
	}
	mustShow(t, r.m.View().Content, "cannot open file", "/definitely/missing.json")
	if err := r.m.wizard.State().Error; !strings.Contains(err, "no such file") {
		t.Errorf("inline error = %q, want the missing-file line", err)
	}

	r.upd(special(tea.KeyEsc))
	if r.m.errModal != nil {
		t.Fatal("esc must close the screen")
	}
	if r.m.wizard == nil {
		t.Fatal("the wizard must stay open underneath the screen")
	}
}
