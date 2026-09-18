// root_wizard_specboth_test.go verifies the send wizard's connect-first
// walk needs no spec prompt of its own: it collects the spec step before
// the file step and commits BOTH keys through one settings apply, so a
// specless transaction file lands on the explicitly chosen spec.
package tui

import (
	"os"
	"path/filepath"
	"testing"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/widgets"
)

func TestRootWizardCollectsSpecBeforeFileAppliesBoth(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})
	if r.m.wizard == nil {
		t.Fatal("wizard must open")
	}

	flex := filepath.Join("..", "..", "specs", "flex.json")
	r.upd(pages.WizardBrowseMsg{IsSpec: true})
	if r.m.filePickTarget != wizardPickSpecTarget {
		t.Fatalf("first browse target = %q, want the wizard spec leg", r.m.filePickTarget)
	}
	r.upd(widgets.FilePickedMsg{Path: flex})
	if r.m.wizardSpec != flex {
		t.Fatalf("the wizard must collect the spec first, wizardSpec = %q", r.m.wizardSpec)
	}

	r.upd(pages.WizardBrowseMsg{IsSpec: false})
	if r.m.filePickTarget != wizardPickFileTarget {
		t.Fatalf("second browse target = %q, want the wizard file leg", r.m.filePickTarget)
	}
	tx := filepath.Join(t.TempDir(), "specless-walk.json")
	if err := os.WriteFile(tx, []byte(gateSpeclessJSON), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}
	r.upd(widgets.FilePickedMsg{Path: tx})
	if r.m.wizardFile != tx || len(r.m.wizard.State().Templates) == 0 {
		t.Fatal("a specless tx file must load its templates on the wizard's file step")
	}

	cmd := r.upd(pages.WizardSendMsg{Name: "Echo"})
	if cmd != nil {
		_, _ = r.m.Update(cmd()) // let the send page command run
	}
	cfg := r.m.app.Config()
	if got := cfg.GetSpec(); got != flex {
		t.Fatalf("cfg.GetSpec() = %q, want the wizard's spec pick", got)
	}
	if got := cfg.GetFile(); got != tx {
		t.Fatalf("cfg.GetFile() = %q, want the wizard's file pick", got)
	}
	if r.m.wizard != nil {
		t.Fatal("a committed send must close the wizard")
	}
	if names := r.m.app.Transactions().ListNames(); len(names) != 1 || names[0] != "Echo" {
		t.Fatalf("live transactions = %v, want the wizard file loaded", names)
	}
}
