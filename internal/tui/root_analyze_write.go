// root_analyze_write.go is the §J write leg: writing the analyze output to
// the user's config directory without ever silently replacing a same-named
// file; the confirm decision rides the shared widgets.ConfirmDialog.
package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// handleAnalyzeWrite is w on the run step: stat the output path off the UI
// thread first; an existing target routes to the §N3 ConfirmDialog before
// anything is written. One write in flight, no queue: the leg carries a
// cancellable ctx so a stale write never restarts SaveItems mid-rewrite.
func (m *RootModel) handleAnalyzeWrite() (tea.Model, tea.Cmd) {
	if m.analyzeStep != pages.StepRun || m.analyzeWriteWait || m.analyzeRunWait ||
		m.analyzeOutput == nil || m.analyzeOverwriteConfirm != nil {
		return m, nil
	}
	if m.analyzeRunStale {
		// The plan is bound to the path used AT RUN TIME; writing now would
		// land the items in the stale target. Re-run first.
		m.analyzeWriteLine = "selections changed - enter re-runs the analysis first"
		m.analyzeWriteOK = false

		return m, nil
	}
	src := m.analyzeSource()
	if src == nil {
		return m, nil
	}
	m.analyzeWriteWait = true
	m.analyzeWriteLine = ""
	// A new write intent supersedes any older note (UAT finding: a note
	// from an earlier cancel sat next to "✓ wrote" and read like a
	// warning about the fresh success).
	m.analyzeNote = ""
	seq := m.analyzeSeq
	out := m.analyzeOutput
	outPath := out.OutputFile
	stat := m.analyzeStat()

	return m, func() tea.Msg {
		exists := false
		if outPath != "" {
			_, serr := stat(filepath.Clean(outPath))
			exists = serr == nil
		}

		return analyzeWriteStatMsg{seq: seq, out: out, path: outPath, exists: exists}
	}
}

// applyAnalyzeWriteStat routes the stat verdict: an existing target
// opens the §N3 overwrite confirm (default No — the user's items stay
// intact); a fresh target with an intact extract proceeds straight to
// the write. Either way, an incomplete scenario extract (F12: no
// scenario item, or no mock routes) is not silently refused — the
// confirm names what is missing and the operator decides.
func (m *RootModel) applyAnalyzeWriteStat(msg analyzeWriteStatMsg) (tea.Model, tea.Cmd) {
	m.analyzeWriteWait = false
	if msg.seq != m.analyzeSeq || m.Current().ID() != pages.AnalyzePageID {
		return m, nil
	}
	if ask := scenarioWriteMissing(msg.out); ask != "" {
		m.analyzeIntegrityAsk = scenarioWriteIntegrity(msg.out)
		if msg.exists {
			return m.confirmIncomplete(ask, "overwrite "+msg.path+"? the extract has "+ask)
		}

		return m.confirmIncomplete(ask, "the extract has "+ask+" - write anyway?")
	}

	if msg.exists {
		m.analyzeOverwriteConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "overwrite "+msg.path+"?")

		return m, nil
	}

	return m.armAnalyzeWrite(msg.out, msg.path)
}

// confirmIncomplete asks the operator to confirm a scenario extract that
// cannot run itself (F12): the box names what is missing; only an
// explicit yes writes it. The dialog owns its decision keys in-body.
func (m *RootModel) confirmIncomplete(missing, question string) (tea.Model, tea.Cmd) {
	m.analyzeOverwriteConfirm = widgets.NewConfirmDialog(m.themeOrNil(), question)

	return m, nil
}

// armAnalyzeWrite launches the cancellable write leg: the stored
// cancel is armed by doAnalyzeAbort/leaveAnalyze, so an aborted or
// left wizard's write is refused before it touches the disk.
func (m *RootModel) armAnalyzeWrite(out *app.AnalyzeOutput, outPath string) (tea.Model, tea.Cmd) {
	src := m.analyzeSource()
	if src == nil {
		return m, nil
	}
	m.cancelAnalyzeWrite()
	ctx, cancel := context.WithCancel(context.Background())
	m.analyzeWriteCancel = cancel
	m.analyzeWriteWait = true
	m.analyzeSeq++
	seq := m.analyzeSeq
	count := len(out.SelectedItems()) // Exactly the picked set lands in the file

	return m, func() tea.Msg {
		return analyzeWriteLoadedMsg{seq: seq, path: outPath, count: count, err: src.WriteAnalyze(ctx, out)}
	}
}

// cancelAnalyzeWrite cancels the in-flight write leg (idempotent; the
// cancel func clears its own slot).
func (m *RootModel) cancelAnalyzeWrite() {
	if m.analyzeWriteCancel != nil {
		m.analyzeWriteCancel()
		m.analyzeWriteCancel = nil
	}
}

// applyAnalyzeOverwriteConfirmed / applyAnalyzeOverwriteCancelled
// drive the §N3 overwrite confirm and the F12 incomplete-extract
// confirm (both live in analyzeOverwriteConfirm; default No writes
// nothing). The integrity note survives only to explain a cancel.
func (m *RootModel) applyAnalyzeOverwriteConfirmed() (tea.Model, tea.Cmd) {
	confirm := m.analyzeOverwriteConfirm
	m.analyzeOverwriteConfirm = nil
	m.analyzeIntegrityAsk = ""
	if confirm == nil || m.analyzeOutput == nil {
		return m, nil // straggler after a reset
	}

	return m.armAnalyzeWrite(m.analyzeOutput, m.analyzeOutput.OutputFile)
}

func (m *RootModel) applyAnalyzeOverwriteCancelled() (tea.Model, tea.Cmd) {
	m.analyzeOverwriteConfirm = nil
	m.analyzeWriteWait = false
	if m.analyzeIntegrityAsk != "" {
		// The operator said no to an incomplete extract: name the gap
		// and the keys that fix it, so the next w can land (the note
		// renders on the run step, done status included).
		m.analyzeNote = m.analyzeIntegrityAsk
		m.analyzeIntegrityAsk = ""
	}

	return m, nil
}

// applyAnalyzeWrite folds the write result into the toast-style line
// shown on the run step.
func (m *RootModel) applyAnalyzeWrite(msg analyzeWriteLoadedMsg) (tea.Model, tea.Cmd) {
	m.analyzeWriteWait = false
	m.cancelAnalyzeWrite() // the leg is over (fresh or stale): drop its cancel
	if msg.seq != m.analyzeSeq || m.Current().ID() != pages.AnalyzePageID {
		return m, nil
	}
	if msg.err != nil {
		m.analyzeWriteLine = "write failed: " + msg.err.Error()
		m.analyzeWriteOK = false
		m.analyzeFileWritten = false
		m.openErrorModal("cannot write analyze output", msg.err)

		return m, nil
	}
	m.analyzeWriteLine = "wrote " + strconv.Itoa(msg.count) + " item(s) to " + msg.path
	m.analyzeWriteOK = true
	m.analyzeFileWritten = true

	return m, nil
}

// handleAnalyzeOutCommit records the [o] output path as typed (it may not
// exist yet), absolutized like the wizard's file commit, and marks the run
// stale: preview and write target are bound to the path used at run time.
func (m *RootModel) handleAnalyzeOutCommit(msg pages.AnalyzeOutCommitMsg) (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(msg.Path)
	if path == "" {
		return m, nil
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	m.analyzeOutputPath = path
	m.analyzeRunStale = true
	m.analyzeWriteLine = "output set: " + path + " - enter re-runs, w writes"
	m.analyzeWriteOK = true
	m.analyzeFileWritten = false // the pending target moved; nothing new is on disk
	m.debug.logf("analyze output set %s", path)

	return m, nil
}

// applyAnalyzeOutputPick commits a picker selection as the output path. A
// file names itself; the [s] folder pick names a directory, which the
// engine cannot write — the effective output's file name is appended
// instead. Either way the commit rides the same [o] leg as the typed path.
func (m *RootModel) applyAnalyzeOutputPick(path string) (tea.Model, tea.Cmd) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		base := filepath.Base(m.analyzeOutputDisplay())
		if base == "" || base == "." || base == string(filepath.Separator) {
			base = filepath.Base(app.AnalyzeOutputFile(nil, analyzeEngineMode(m.analyzeGoal)))
		}
		path = filepath.Join(path, base)
		m.debug.logf("analyze output folder pick %s -> %s", filepath.Dir(path), path)
	}

	return m.handleAnalyzeOutCommit(pages.AnalyzeOutCommitMsg{Path: path})
}

// The F12 gaps, as the short phrases the confirm question carries.
const (
	missingScenarioItem = "no scenario item"
	missingMockRoutes   = "no mock routes"
)

// scenarioWriteMissing names what a scenario-mode selection lacks for
// the F12 self-run contract, as a short noun phrase for the confirm
// question ("" when intact): "no scenario item" or "no mock routes".
func scenarioWriteMissing(out *app.AnalyzeOutput) string {
	if out == nil || out.Mode != "scenario" {
		return ""
	}
	var hasScenario, hasRoute, hasTx bool
	for _, it := range out.SelectedItems() {
		switch it.Type {
		case config.TypeScenario:
			hasScenario = true
		case config.TypeMockRoute:
			hasRoute = true
		case config.TypeTransaction:
			hasTx = true
		}
	}
	switch {
	case hasTx && !hasScenario:
		return missingScenarioItem
	case hasScenario && !hasRoute:
		return missingMockRoutes
	}

	return ""
}

// scenarioWriteIntegrity is the full sentence behind scenarioWriteMissing:
// what the gap means and the keys that fix it. It opens the confirm and
// stays visible as the note when the operator cancels — the deliberate
// subset write is allowed, never silently refused.
func scenarioWriteIntegrity(out *app.AnalyzeOutput) string {
	switch scenarioWriteMissing(out) {
	case missingScenarioItem:
		return "the selection has " + missingScenarioItem +
			" - the extract cannot run as a scenario ([x] reopens the picker, [a] selects all)"
	case missingMockRoutes:
		return "the selection has " + missingMockRoutes +
			" - the mock server cannot start from this extract ([x] reopens the picker, [a] selects all)"
	}

	return ""
}
