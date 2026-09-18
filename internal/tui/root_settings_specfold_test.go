// root_settings_specfold_test.go pins the §B fold branch of the settings
// apply result: a chained spec pick (spec + tx file in one patch) whose
// SPEC fails to load must open the same error screen §L raises — the
// apply loop is per-field, so the tx file does land against the previous
// live spec, and the modal is what names the rejected specification.
// The §B inline error line stays empty: the loaded file has content and
// must stay listed under its true spec chip, never masked by an
// error-body page claiming nothing loaded.
package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/widgets"
)

// TestRootSpecFoldSurfacesSpecLoadFailure: bad spec path through the §B
// chained pick → modal fired naming the spec; the spec itself did not
// land while the tx file did (the non-aborting apply loop, surfaced by
// the modal instead of hidden).
func TestRootSpecFoldSurfacesSpecLoadFailure(t *testing.T) {
	f := newSpecGateFix(t, false)
	f.gateOpen() // §B f → txload.json → the chained spec browse is open

	bad := filepath.Join(f.dir, "missing.json")
	f.r.pump(widgets.FilePickedMsg{Path: bad})

	m := f.r.m
	if m.errModal == nil {
		t.Fatal("a chained spec pick that fails to load must open the error screen")
	}
	if m.errModal.title != "cannot load specification file" {
		t.Errorf("modal title = %q, want the §L spec-load title", m.errModal.title)
	}
	if !strings.Contains(m.errModal.body, bad) {
		t.Errorf("the modal must name the rejected spec, got %q", m.errModal.body)
	}

	cfg := m.app.Config()
	if got := cfg.GetSpec(); got != "" {
		t.Errorf("the rejected spec must not land: cfg.GetSpec() = %q", got)
	}
	if got := cfg.GetFile(); got != f.txload {
		t.Fatalf("the per-field apply loop lands the tx file against the previous spec (the modal names that): cfg.GetFile() = %q", got)
	}
	if m.txFileLoadErr != "" {
		t.Errorf("the loaded file must stay listed, not masked by a load-error body: %q", m.txFileLoadErr)
	}

	f.r.pump(special(tea.KeyEscape))
	if m = f.r.m; m.errModal != nil {
		t.Fatal("esc must close the screen")
	}
}
