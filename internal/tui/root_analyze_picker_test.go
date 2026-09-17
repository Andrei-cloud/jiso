// root_analyze_picker_test.go owns the §J file-picker seam: the capture
// step's [f] opens the .pcap picker, the spec step's [f] opens the .json
// picker (never the capture one), and the empty-spec Enter escape stays
// additive.
package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

func TestAnalyzePickerSelectionCommits(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.gotoAnalyze()
	r.pump(ch('f')) // [f] opens the shared picker
	if r.m.filePick == nil {
		t.Fatalf("f must open the file picker:\n%s", r.view())
	}
	if r.m.filePickTarget != analyzePickTarget {
		t.Fatalf("capture picker target = %q, want %q", r.m.filePickTarget, analyzePickTarget)
	}
	r.pump(widgets.FilePickedMsg{Path: r.pcap, Label: "cap.pcap"})

	r.mustStep(t, pages.StepSpec)
	if r.m.analyzeCapturePath != r.pcap {
		t.Fatalf("captured path = %q, want the pick", r.m.analyzeCapturePath)
	}
}

// [f] on the spec step opens the .json picker (target "analyze:spec",
// never the .pcap one) starting at the spec path's dir; the pick commits
// through the same spec-choose leg Enter uses.
func TestAnalyzeSpecBrowseOpensSpecPicker(t *testing.T) {
	f := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, f)
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	r.mustStep(t, pages.StepSpec)

	// A fixture dir with one legal .json and one decoy .pcap; the
	// picker must start at the spec path's directory.
	dir := t.TempDir()
	json := filepath.Join(dir, "pick.json")
	if err := os.WriteFile(json, []byte("{}"), 0o644); err != nil {
		t.Fatalf("temp json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "decoy.pcap"), []byte("d4c3b2a1"), 0o644); err != nil {
		t.Fatalf("temp pcap: %v", err)
	}
	r.m.analyzeSpecPath = filepath.Join(dir, "draft.json")

	r.pump(ch('f')) // navigate-mode [f] on the spec step
	if r.m.filePick == nil {
		t.Fatalf("f on the spec step must open the file picker:\n%s", r.view())
	}
	if r.m.filePickTarget != analyzeSpecPickTarget {
		t.Fatalf("spec picker target = %q, want %q (the capture picker would re-pick the .pcap)",
			r.m.filePickTarget, analyzeSpecPickTarget)
	}
	if got := r.m.filePick.CurrentDir(); got != dir {
		t.Fatalf("picker start = %q, want the spec dir %q", got, dir)
	}

	// Rows are "..", "decoy.pcap", "pick.json" (dirs first, files by
	// name). Enter on the unselectable .pcap keeps the picker open and
	// commits nothing.
	r.pump(tea.KeyPressMsg{Code: tea.KeyDown})
	r.enter()
	if r.m.filePick == nil {
		t.Fatalf("enter on a .pcap must not select in the .json picker:\n%s", r.view())
	}
	if r.m.analyzeStep != pages.StepSpec {
		t.Fatalf("the refused pick advanced to step %d, want the spec step", r.m.analyzeStep)
	}

	// The .json pick lands through the spec commit leg: validate + advance.
	r.pump(tea.KeyPressMsg{Code: tea.KeyDown})
	r.enter()
	if r.m.filePick != nil {
		t.Fatal("the pick must close the picker")
	}
	r.mustStep(t, pages.StepHeader)
	if r.m.analyzeSpecPath != json {
		t.Fatalf("spec path = %q, want the json pick %q", r.m.analyzeSpecPath, json)
	}
	if f.statN == 0 {
		t.Fatal("the spec pick must run the stat-validate leg")
	}
}

// the spec picker is additive: Enter with an empty spec still lands on
// the engine default without opening the picker.
func TestAnalyzeSpecEmptyEnterEscapeAdditive(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	r.mustStep(t, pages.StepSpec)

	r.enter() // empty spec = the engine default
	r.mustStep(t, pages.StepHeader)
	if r.m.filePick != nil {
		t.Fatal("the empty-spec escape must not open the picker")
	}
}
