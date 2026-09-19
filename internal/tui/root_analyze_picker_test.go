// root_analyze_picker_test.go owns the §J file-picker seam: the capture
// step's [f] opens the .pcap picker, the spec step's [f] opens the .json
// picker (never the capture one), the empty-spec Enter escape stays
// additive, and a forward arrival at a file step with a previously
// chosen file auto-opens that picker positioned on the pick.
package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
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

// pickDir writes a two-.json fixture dir (plus one .pcap decoy) and
// returns (dir, the visa.json path).
func pickDir(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"visa.json", "other.json", "decoy.pcap"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o644); err != nil {
			t.Fatalf("temp %s: %v", n, err)
		}
	}

	return dir, filepath.Join(dir, "visa.json")
}

// A forward re-arrival at the spec step with a previously chosen file
// opens the shared browser on that file's dir, cursor seated on the
// file; the first arrival (nothing chosen yet) keeps the inline scan;
// esc closes the browser and the inline list stays fully usable.
func TestAnalyzeSpecReArrivalAutoOpensOnLastPick(t *testing.T) {
	f := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, f)
	r.gotoAnalyze()
	if r.m.filePick != nil {
		t.Fatal("entering the wizard must not open the browser")
	}
	r.commitCapture(r.pcap)
	r.mustStep(t, pages.StepSpec)
	if r.m.filePick != nil {
		t.Fatal("the first spec arrival has no previous pick — the inline scan stays")
	}

	dir, spec := pickDir(t)
	r.m.analyzeSpecPath = spec

	// Back out to capture (backward arrival: no auto-open), Enter
	// re-commits the capture — the forward re-arrival lands with the
	// browser open on the previously picked spec file.
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	r.mustStep(t, pages.StepCapture)
	if r.m.filePick != nil {
		t.Fatal("a backward arrival must not auto-open the browser")
	}

	r.enter()
	r.mustStep(t, pages.StepSpec)
	if r.m.filePick == nil {
		t.Fatalf("re-arrival with a previous pick must open the browser:\n%s", r.view())
	}
	if r.m.filePickTarget != analyzeSpecPickTarget {
		t.Fatalf("auto-open target = %q, want %q", r.m.filePickTarget, analyzeSpecPickTarget)
	}
	if got := r.m.filePick.CurrentDir(); got != dir {
		t.Fatalf("browser start = %q, want the previous pick's dir %q", got, dir)
	}
	if got := r.m.filePick.CursorName(); got != "visa.json" {
		t.Fatalf("browser cursor = %q, want the previous pick", got)
	}

	// Esc from the auto-opened browser: picker gone, the inline
	// candidate list is fully keyboard-usable again — j/k move it and
	// [f] re-opens the browse (today's code paths).
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	if r.m.filePick != nil {
		t.Fatal("esc must close the auto-opened browser")
	}
	r.pump(ch('j'))
	if got := r.m.analyze.ListCursor(); got != 1 {
		t.Fatalf("inline list cursor = %d, want j to move it", got)
	}
	r.pump(ch('f'))
	if r.m.filePick == nil {
		t.Fatal("[f] must still re-open the browse")
	}
	if got := r.m.filePick.CurrentDir(); got != dir {
		t.Fatalf("[f] re-open start = %q, want the spec dir", got)
	}

	// The typed draft path survives too: after esc, printables reach
	// the step's filter/typed draft.
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	if r.m.filePick != nil {
		t.Fatal("esc must close the re-opened browser")
	}
	r.typeText("vi")
	if d, editing := r.m.analyze.Draft(); !editing || d != "vi" {
		t.Fatalf("draft after esc = %q/%v, want the typed vi", d, editing)
	}
}

// Opening §J with a previous capture pick shows the browser seated on
// that pcap — the step's ENTRY is the capture arm's arrival; a fresh
// entry keeps the inline scan; esc-then-leave-then-re-enter fires again
// (the guard bool is entry-scoped).
func TestAnalyzeEntryAutoOpensCaptureBrowser(t *testing.T) {
	f := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, f)
	r.gotoAnalyze()
	if r.m.filePick != nil {
		t.Fatal("entering fresh (nothing picked) keeps the inline capture scan")
	}
	r.commitCapture(r.pcap) // the real commit leg records the pick
	r.mustStep(t, pages.StepSpec)

	r.pump(ch('1')) // leave mid-wizard; the path survives the leave
	r.pump(ch('7')) // re-entry: the browser opens on the previous capture pick
	r.mustStep(t, pages.StepCapture)
	if r.m.filePick == nil {
		t.Fatalf("re-entry with a previous capture pick must open the browser:\n%s", r.view())
	}
	if r.m.filePickTarget != analyzePickTarget {
		t.Fatalf("entry target = %q, want %q", r.m.filePickTarget, analyzePickTarget)
	}
	if got, want := r.m.filePick.CurrentDir(), filepath.Dir(r.pcap); got != want {
		t.Fatalf("entry start dir = %q, want the pick's dir %q", got, want)
	}
	if got := r.m.filePick.CursorName(); got != "cap.pcap" {
		t.Fatalf("entry cursor = %q, want the previous pick", got)
	}

	// esc closes the browser; esc again aborts (step 1 leaves the
	// wizard); a fresh re-entry fires again.
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	if r.m.filePick != nil {
		t.Fatal("esc must close the entry browser")
	}
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	r.pump(ch('7'))
	if r.m.filePick == nil {
		t.Fatal("a fresh entry fires again")
	}
	if got := r.m.filePick.CursorName(); got != "cap.pcap" {
		t.Fatalf("second entry cursor = %q, want the previous pick", got)
	}
}

// Entry while a browse is already open (a page jump can arrive as a
// GoToPageMsg over an open picker — keys stay with the picker): the open
// picker is the surface the operator is using — entry never burns or
// re-opens it.
func TestAnalyzeEntryLeavesOpenBrowseAlone(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	dir, _ := pickDir(t)
	r.m.analyzeCapturePath = r.pcap
	r.pump(OpenFilePickerMsg{Target: settingsSpecForFileTarget, Root: dir, RootLabel: "fixture/"})
	pick := r.m.filePick
	if pick == nil {
		t.Fatal("precondition: a browse must be open before the page jump")
	}

	r.pump(palette.GoToPageMsg{ID: pages.AnalyzePageID})
	if r.m.Current().ID() != pages.AnalyzePageID {
		t.Fatalf("the jump must land on §J, got %q", r.m.Current().ID())
	}
	if r.m.filePick != pick {
		t.Fatal("entry must not re-open or replace the already-open browse")
	}
	if r.m.filePickTarget != settingsSpecForFileTarget {
		t.Fatalf("entry burned the open browse target %q", r.m.filePickTarget)
	}
}

// One forward arrival opens the browser exactly once (resize churn and
// the cmd pump re-entrant create no second picker); after esc the next
// forward arrival fires again — the browser is the step's default
// surface, re-arming per arrival, never looping within one.
func TestAnalyzeSpecAutoOpenFiresOncePerArrival(t *testing.T) {
	f := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, f)
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	_, spec := pickDir(t)
	r.m.analyzeSpecPath = spec

	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape}) // back to capture
	r.enter()                                    // forward arrival: auto-open
	if r.m.filePick == nil {
		t.Fatal("precondition: the auto-open must have fired")
	}
	pick := r.m.filePick

	r.pump(tea.WindowSizeMsg{Width: 122, Height: 42}) // churn resizes, never re-opens
	if r.m.filePick != pick {
		t.Fatal("one arrival must open the browser exactly once")
	}
	if got := r.m.filePick.CursorName(); got != "visa.json" {
		t.Fatalf("the positioned cursor must survive the churn: %q", got)
	}

	// esc-back-and-forth: close, back, re-commit — the SECOND arrival
	// fires again (deterministic: the browser is the default surface).
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	if r.m.filePick != nil {
		t.Fatal("esc must close the browser")
	}
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape}) // back a step (backward)
	r.mustStep(t, pages.StepCapture)
	r.enter()
	if r.m.filePick == nil {
		t.Fatal("a later forward arrival fires again")
	}
	if got := r.m.filePick.CursorName(); got != "visa.json" {
		t.Fatalf("second auto-open cursor = %q, want the previous pick", got)
	}
}

// Backward step movement never auto-opens: backing run ▸ header ▸ spec ▸
// capture over chosen files keeps every step's inline list.
func TestAnalyzeBackwardArrivalsNeverAutoOpen(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.walkToRun(t)
	_, spec := pickDir(t)
	r.m.analyzeSpecPath = spec

	for _, want := range []int{pages.StepHeader, pages.StepSpec, pages.StepCapture} {
		r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
		r.mustStep(t, want)
		if r.m.filePick != nil {
			t.Fatalf("backward arrival at step %d opened the browser", want)
		}
	}
}
