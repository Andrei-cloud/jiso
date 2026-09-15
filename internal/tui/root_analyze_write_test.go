// root_analyze_write_test.go is the §J write-leg test file: the write
// gate, the overwrite confirm, and the output-path commit that threads
// the [o] pick to the engine (the harness — fakeAnalyze, analyzeTestRoot
// — lives in root_analyze_test.go, same package).
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// TestAnalyzeRunOpensItemPickerAndWritesSelected UAT round 6: the run
// re-presents the generated-item picker (no dry-run text), space+Enter
// apply the selection, and w writes exactly the selected items.
func TestAnalyzeRunOpensItemPickerAndWritesSelected(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)
	r.enter()

	if f.runN != 1 {
		t.Fatalf("engine ran %d times, want 1", f.runN)
	}
	opts := f.runOpts[0]
	// UAT round 8 finding 6: the unchosen header rides the run opts as ""
	// (the engine default applies in the analyzer, not in the wizard).
	if opts.Mode != app.AnalyzeModeTx || opts.HeaderType != "" {
		t.Fatalf("run opts = %+v", opts)
	}
	if r.m.analyzeStatus != pages.AnalyzeStatusDone {
		t.Fatalf("status = %q, want done", r.m.analyzeStatus)
	}
	v := r.view()
	if strings.Contains(v, "dry-run") {
		t.Fatalf("the redundant dry-run text is back:\n%s", v)
	}
	if r.m.analyzeItemsID != 1 || len(r.m.analyzeItemRows) != 3 {
		t.Fatalf("picker roster = id %d / %d rows, want 1/3 (a run must re-present the items)",
			r.m.analyzeItemsID, len(r.m.analyzeItemRows))
	}
	if !strings.Contains(v, "ITEMS") || !strings.Contains(v, "Captured Flow 0200_0") {
		t.Fatalf("item picker overlay missing after the run:\n%s", v)
	}
	if f.writeN != 0 {
		t.Fatal("the picker must not write")
	}

	// space deselects the roster row under the cursor; Enter applies the
	// selection (page-local toggles fold into root). Navigate to the
	// standalone mock route (row 2) so the deselection is exactly one item —
	// the transaction and its dataset couple (see
	// TestAnalyzeItemPickerCouplesDatasetWrite, UAT round 7).
	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch(' '))
	r.pump(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(r.m.analyzeExcluded) != 1 {
		t.Fatalf("apply did not fold the deselection: %v", r.m.analyzeExcluded)
	}

	r.pump(ch('w'))
	if f.writeN != 1 {
		t.Fatalf("wrote %d times, want exactly 1", f.writeN)
	}
	if len(f.writtenSel) != 1 || f.writtenSel[0] != 2 {
		t.Fatalf("write persisted %v items, want exactly the 2 selected", f.writtenSel)
	}
	if !strings.Contains(r.view(), "wrote 2 item(s) to transactions/transaction.json") || !r.m.analyzeWriteOK {
		t.Fatalf("write line missing or wrong count:\n%s", r.view())
	}

	// Enter again with unchanged selections reuses the preview (no
	// second engine run).
	r.enter()
	if f.runN != 1 {
		t.Fatalf("unchanged selections re-ran the engine %d times, want 1", f.runN)
	}
}

// TestAnalyzePgUpDuringWriteWaitNoDoubleWrite: PgUp while a write leg
// is in flight must be refused ("write in flight") — the old ungated
// backward jump cleared analyzeWriteWait, so PgUp→PgDn→w armed a SECOND
// concurrent config.SaveItems on the same file. The fake write-call
// counter pins exactly one write.
func TestAnalyzePgUpDuringWriteWaitNoDoubleWrite(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)
	r.enter()
	r.closePicker() // the run re-presented the item picker (UAT round 6) // the run
	r.pump(nil)
	r.mustStep(t, pages.StepRun)

	wLeg := r.async(ch('w')) // the write leg is now in flight
	if wLeg == nil || !r.m.analyzeWriteWait {
		t.Fatalf("write leg not in flight: cmd=%v wait=%v", wLeg != nil, r.m.analyzeWriteWait)
	}

	r.pump(special(tea.KeyPgUp)) // backward jump must be refused
	if r.m.analyzeStep != pages.StepRun {
		t.Fatalf("PgUp moved the step while a write was in flight: %d", r.m.analyzeStep)
	}
	if !strings.Contains(r.m.analyzeNote, "write in flight") {
		t.Fatalf("refusal note = %q, want it to name the in-flight write", r.m.analyzeNote)
	}
	r.pump(special(tea.KeyPgDown))
	r.pump(ch('w')) // second w must be gated by analyzeWriteWait

	if f.writeN != 0 {
		t.Fatalf("write ran before the in-flight leg landed: %d", f.writeN)
	}
	r.pump(wLeg()) // the held leg lands
	if f.writeN != 1 {
		t.Fatalf("wrote %d times, want exactly 1 (double SaveItems regression)", f.writeN)
	}
}

// TestAnalyzeWriteOverwriteConfirmFires: w over an EXISTING output
// opens the §N3 confirm (default No) before touching the user's real
// config; cancel writes nothing, y writes once.
func TestAnalyzeWriteOverwriteConfirmFires(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.m.analyzeStatFn = func(string) (os.FileInfo, error) { return nil, nil } // fake "exists"
	r.walkToRun(t)
	r.enter()
	r.closePicker() // the run re-presented the item picker (UAT round 6)
	r.pump(nil)

	r.pump(ch('w'))
	if r.m.analyzeOverwriteConfirm == nil || !r.m.analyzeOverwriteConfirm.Pending() {
		t.Fatalf("w over an existing target must ask §N3 confirm:\n%s", r.view())
	}
	if !strings.Contains(r.view(), "overwrite transactions/transaction.json?") {
		t.Fatalf("confirm question missing:\n%s", r.view())
	}
	if f.writeN != 0 {
		t.Fatal("confirm must gate the write")
	}

	r.pump(ch('n')) // default No — nothing is written
	if r.m.analyzeOverwriteConfirm != nil || f.writeN != 0 {
		t.Fatalf("cancel must write nothing: confirm=%v writeN=%d", r.m.analyzeOverwriteConfirm != nil, f.writeN)
	}

	r.pump(ch('w'))
	if r.m.analyzeOverwriteConfirm == nil {
		t.Fatal("second w must re-ask")
	}
	r.pump(ch('y')) // explicit yes proceeds
	if f.writeN != 1 || !r.m.analyzeWriteOK {
		t.Fatalf("after y: writeN=%d writeOK=%v", f.writeN, r.m.analyzeWriteOK)
	}
}

// TestAnalyzeOutputOpensPicker: UAT round 8 finding 6 — the run-step
// [o] output editor is no longer type-only: the browse affordance [f]
// opens the shared picker with target "analyze:output", a file pick
// names the output file itself, a folder pick yields a usable
// <folder>/<effective base name> path, and Esc in the picker leaves the
// effective path untouched.
func TestAnalyzeOutputOpensPicker(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)
	r.enter() // the run attaches the items (the picker auto-opens)
	r.closePicker()

	r.pump(ch('o')) // the editor opens, seeded with the effective path
	r.pump(ch('f')) // the browse affordance hands the keyboard to the picker
	if r.m.filePick == nil {
		t.Fatalf("[f] in the output editor must open the file picker:\n%s", r.view())
	}
	if r.m.filePickTarget != "analyze:output" {
		t.Fatalf("picker target = %q, want %q", r.m.filePickTarget, "analyze:output")
	}

	// A folder pick must yield a usable output path: the picked folder
	// plus the effective output's file name (the engine writes a file,
	// never a directory).
	dir := t.TempDir()
	r.pump(widgets.FilePickedMsg{Path: dir, Label: "fixture/"})
	wantDir := filepath.Join(dir, "transaction.json")
	if r.m.analyzeOutputPath != wantDir {
		t.Fatalf("folder pick path = %q, want %q", r.m.analyzeOutputPath, wantDir)
	}

	// A file pick names itself; the run goes stale, nothing writes.
	file := filepath.Join(t.TempDir(), "gen.json")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatalf("temp file: %v", err)
	}
	r.pump(ch('o'))
	r.pump(ch('f'))
	if r.m.filePick == nil {
		t.Fatal("the second browse must reopen the picker")
	}
	r.pump(widgets.FilePickedMsg{Path: file, Label: "fixture/gen.json"})
	if r.m.analyzeOutputPath != file {
		t.Fatalf("file pick path = %q, want %q", r.m.analyzeOutputPath, file)
	}
	if !r.m.analyzeRunStale {
		t.Fatal("an output pick must mark the run stale")
	}
	if f.writeN != 0 {
		t.Fatalf("a pick wrote %d times, want 0", f.writeN)
	}

	// Esc in the picker commits nothing (the effective path survives).
	before := r.m.analyzeOutputPath
	r.pump(ch('o'))
	r.pump(ch('f'))
	if r.m.filePick == nil {
		t.Fatal("the third browse must reopen the picker")
	}
	r.pump(widgets.FilePickerCanceledMsg{})
	if r.m.filePick != nil {
		t.Fatal("esc must close the picker")
	}
	if r.m.analyzeOutputPath != before {
		t.Fatalf("cancel changed the output path: %q -> %q", before, r.m.analyzeOutputPath)
	}
}

// TestAnalyzeOutCommitThreadsAndGates: UAT round 5 — the [o] commit
// stores the output path, marks the run stale, REFUSES a write against
// the stale plan, and the next Enter threads the path to the engine.
func TestAnalyzeOutCommitThreadsAndGates(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)
	r.enter()
	r.closePicker() // the run re-presented the item picker (UAT round 6) // fresh run: stale clears

	out := filepath.Join(t.TempDir(), "gen.json")
	r.pump(pages.AnalyzeOutCommitMsg{Path: out})
	if r.m.analyzeOutputPath != out {
		t.Fatalf("output path = %q, want %q", r.m.analyzeOutputPath, out)
	}
	if !r.m.analyzeRunStale {
		t.Fatal("an output change must mark the run stale")
	}

	r.pump(pages.AnalyzeWriteMsg{})
	if f.writeN != 0 {
		t.Fatalf("write ran %d times before a re-run", f.writeN)
	}
	if !strings.Contains(r.m.analyzeWriteLine, "re-runs") {
		t.Errorf("write gate line = %q, want the re-run note", r.m.analyzeWriteLine)
	}

	r.enter() // re-run threads the path through
	if f.runN != 2 {
		t.Fatalf("engine ran %d times, want 2", f.runN)
	}
	if got := f.runOpts[1].OutputFile; got != out {
		t.Errorf("re-run OutputFile = %q, want %q", got, out)
	}
}
