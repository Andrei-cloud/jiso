// root_analyze_write.go is the §J write leg: writing the analyze output to the
// user's config directory, including the overwrite check that never silently
// replaces a same-named file. The stat and the write are both commands; the
// confirm decision rides the shared widgets.ConfirmDialog.
package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// handleAnalyzeWrite is w on the run step: stat the output path off the
// UI thread first (the §K root_ctf.go precedent) — the write lands on
// the USER's real config file, so same-named items must never be
// silently replaced: an existing target routes to the §N3
// ConfirmDialog (default No) before anything is written. The write leg
// then carries a cancellable ctx: abort/leave cancel it so a stale leg
// never starts a second SaveItems while an earlier one is still
// rewriting the file.
func (m *RootModel) handleAnalyzeWrite() (tea.Model, tea.Cmd) {
	if m.analyzeStep != pages.StepRun || m.analyzeWriteWait || m.analyzeRunWait ||
		m.analyzeOutput == nil || m.analyzeOverwriteConfirm != nil {
		return m, nil
	}
	if m.analyzeRunStale {
		// The output path (or any selection) changed after the last
		// run: the plan is bound to the path used AT RUN TIME, so
		// writing now would land the items in the stale target
		// (UAT round 5). Re-run first.
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
// intact); a fresh target proceeds straight to the write.
func (m *RootModel) applyAnalyzeWriteStat(msg analyzeWriteStatMsg) (tea.Model, tea.Cmd) {
	m.analyzeWriteWait = false
	if msg.seq != m.analyzeSeq || m.Current().ID() != pages.AnalyzePageID {
		return m, nil
	}
	if msg.exists {
		m.analyzeOverwriteConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "overwrite "+msg.path+"?")

		return m, nil
	}

	return m.armAnalyzeWrite(msg.out, msg.path)
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
	count := len(out.SelectedItems()) // UAT round 6: exactly the picked set lands in the file

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
// drive the §N3 overwrite confirm (default No writes nothing).
func (m *RootModel) applyAnalyzeOverwriteConfirmed() (tea.Model, tea.Cmd) {
	confirm := m.analyzeOverwriteConfirm
	m.analyzeOverwriteConfirm = nil
	if confirm == nil || m.analyzeOutput == nil {
		return m, nil // straggler after a reset
	}

	return m.armAnalyzeWrite(m.analyzeOutput, m.analyzeOutput.OutputFile)
}

func (m *RootModel) applyAnalyzeOverwriteCancelled() (tea.Model, tea.Cmd) {
	m.analyzeOverwriteConfirm = nil
	m.analyzeWriteWait = false

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

		return m, nil
	}
	m.analyzeWriteLine = "wrote " + strconv.Itoa(msg.count) + " item(s) to " + msg.path
	m.analyzeWriteOK = true

	return m, nil
}

// handleAnalyzeOutCommit records the [o] output path (UAT round 5):
// stored as typed (the output file may not exist yet — that is the
// point), absolutized like the wizard's file commit, and the run is
// marked stale because the preview text and the write target are bound
// to the path used at run time. Enter re-runs to refresh both.
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
	m.debug.logf("analyze output set %s", path)

	return m, nil
}

// applyAnalyzeOutputPick commits a run-step picker selection as the
// output path (UAT round 8 finding 6). A file names itself; the [s]
// folder pick names a directory, which the engine cannot write — so
// the effective output's file name (the config file's base, else the
// goal's default) is appended and the operator renames it with [o].
// Either way the commit rides the same [o] leg the typed path uses
// (absolutize, stale flag, write gate), so a selection is immediately
// usable.
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
