// root_workers_form.go owns the §H worker start wizard's root legs
// (UAT round 4): both start options leave the empty pane's two-option
// line and the §E/§G ConnectDialog forms, and open ONE
// pages.WorkerWizard modal — b for background send, t for stress,
// tx ▸ rate/params ▸ run. This file keeps the shared glue (open /
// close / key routing / [f] browse / tx-file pick / run dispatch) and
// the bgsend leg; the stress leg and its bounds gate live in
// root_stress_form.go. Prefill sources stay the SAME ones the legacy
// paths use (bgsend = REPL bgsend survey defaults + repository
// ListNames; stress = PAR-306 `jiso stress` flag defaults — the
// wizard's pages.WorkerDefault* constants). Enter on the run step
// starts through the App worker manager as a tea.Cmd (WorkerStart —
// the entry the CLI worker shim drives); a failure keeps the wizard
// open with the error line, and NO row is inserted optimistically —
// the WorkerStarted bus event owns the row's arrival.
package tui

import (
	"context"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// bgPickFileTarget routes the bgsend wizard's [f] tx-file pick.
const bgPickFileTarget = "bg:file"

// bgStartResultMsg is the terminal verdict of a bgsend start attempt;
// the id rides along for debug logging only — the table row arrives
// via the bus, not from this message.
type bgStartResultMsg struct {
	id  string
	err error
}

// openWorkersForm opens (or re-focuses) the worker start wizard for
// the kind ("bgsend" via b, "stress" via t) — the openServerForm
// pattern, page stack untouched.
func (m *RootModel) openWorkersForm(kind string) (tea.Model, tea.Cmd) {
	switch kind {
	case "bgsend":
		return m.openWorkerWizard(pages.WorkerModeBg)
	case "stress":
		return m.openWorkerWizard(pages.WorkerModeStress)
	default:
		return m, nil
	}
}

// openWorkerWizard builds the wizard over the repository's loaded
// transaction names (the survey prompts' option source), seeds the
// bgsend single-select on the first candidate (the retired radio's
// prefill), and sizes it to the content area.
func (m *RootModel) openWorkerWizard(mode string) (tea.Model, tea.Cmd) {
	if m.workerWiz != nil {
		return m, nil
	}
	w := pages.NewWorkerWizard(m.themeOrNil(), mode)
	w.SetState(pages.WorkerWizardState{TxItems: m.workerTxItems()})
	w.HomeSelection()
	if m.width > 0 {
		_, _ = w.Update(m.innerWS())
	}
	m.workerWiz = w
	m.debug.logf("worker wizard open mode=%s", mode)

	return m, nil
}

// closeWorkerWizard drops the modal. An in-flight start leg keeps
// reporting into the apply seams, which no-op once the wizard is gone
// (the retired forms' Esc-while-in-flight contract).
func (m *RootModel) closeWorkerWizard() {
	if m.workerWiz == nil {
		return
	}
	m.workerWiz = nil
	m.debug.logf("worker wizard close")
}

// updateWorkerWizKey routes one key while the wizard owns the
// keyboard. The wizard's help escape hatch stays root-side: "?" on
// the wizard's EMPTY filter line (or anywhere on the param step,
// where "?" is never a value byte) opens the §M overlay, and while
// the overlay is open it owns the keys first (Esc closes the overlay,
// not the wizard — §N1). Ctrl+C stays global (claimed above).
// (UAT round 8 scoped the registry-page claim to be total; the wizard
// modal's two-mode edit entry lands with Task 4.2.)
func (m *RootModel) updateWorkerWizKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.help != nil {
		if keyMatches(msg, m.keys.Help) || msg.Code == tea.KeyEscape {
			m.help = nil
			m.debug.logf("help close")
		}

		return m, nil
	}
	if keyMatches(msg, m.keys.Help) && m.workerWiz.FreshDraft() {
		m.openHelp()
		m.debug.logf("help open from worker wizard")

		return m, nil
	}
	_, cmd := m.workerWiz.Update(msg)

	return m, cmd
}

// workerWizBrowse opens the shared file picker over .json tx files
// ([f] on the wizard's tx step): the retired stress form's picker
// parameters kept verbatim (RootLabel "./", start at the loaded tx
// file's dir). The wizard stays open underneath; Esc returns to it
// and a selection flows through applyFilePicked.
func (m *RootModel) workerWizBrowse() (tea.Model, tea.Cmd) {
	if m.workerWiz == nil || m.filePick != nil {
		return m, nil
	}
	target := bgPickFileTarget
	if m.workerWiz.Mode() == pages.WorkerModeStress {
		target = stressPickFileTarget
	}

	return m.openFilePicker(OpenFilePickerMsg{
		Target: target, Root: "/", RootLabel: "./",
		Start: stressFormStartDir(m.configOrNil()), Exts: []string{jsonExt},
	})
}

// startWorkerRun dispatches Enter on the wizard's run step to the
// mode's leg; both re-check the resolved parameters before the App
// call (the retired forms' bounds gate, one string source in pages).
func (m *RootModel) startWorkerRun(run pages.WorkerRun) (tea.Model, tea.Cmd) {
	if run.Mode == pages.WorkerModeStress {
		return m.startStressWorker(run)
	}

	return m.startBgWorker(run)
}

// startBgWorker re-checks the wizard-resolved parameters against the
// App's own bounds (the retired form's exact texts), flips the wizard
// into its in-flight line, and returns the start Cmd through the
// injectable leg (nil = app.WorkerStart).
func (m *RootModel) startBgWorker(run pages.WorkerRun) (tea.Model, tea.Cmd) {
	switch {
	case run.Name == "":
		return m.workerWizError("select at least one transaction")
	case run.Interval <= 0:
		return m.workerWizError("interval must be greater than 0")
	case run.Count < 1:
		return m.workerWizError("count must be a number greater than 0")
	}

	start := m.workerStartFn
	if start == nil {
		if m.app == nil {
			return m.workerWizError(errNoAppWired)
		}
		start = m.app.WorkerStart
	}

	st := m.workerWiz.State()
	st.InFlight = true
	st.Error = ""
	st.Progress = "starting " + run.Name + " (" + run.Interval.String() + " x" + strconv.Itoa(run.Count) + ")"
	m.workerWiz.SetState(st)
	m.debug.logf("bgsend start requested tx=%s interval=%s count=%d", run.Name, run.Interval, run.Count)

	return m, func() tea.Msg {
		id, err := start(run.Name, run.Count, run.Interval)

		return bgStartResultMsg{id: id, err: err}
	}
}

// workerWizError keeps the wizard open with an inline error line
// (the retired bgFormError/stressFormError seam).
func (m *RootModel) workerWizError(text string) (tea.Model, tea.Cmd) {
	if m.workerWiz != nil {
		st := m.workerWiz.State()
		st.InFlight = false
		st.Progress = ""
		st.Error = text
		m.workerWiz.SetState(st)
	}

	return m, nil
}

// applyBgStartResult closes the run: failure keeps the wizard open
// with the error line (connect-dialog pattern), success closes it and
// toasts. The table row itself arrives on the bus either way — never
// written here.
func (m *RootModel) applyBgStartResult(msg bgStartResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if m.workerWiz != nil {
			st := m.workerWiz.State()
			st.InFlight = false
			st.Progress = ""
			st.Error = msg.err.Error()
			m.workerWiz.SetState(st)
		}
		m.debug.logf("bgsend start failed: %v", msg.err)

		return m, nil
	}
	m.closeWorkerWizard()
	m.pushToast("bgsend started", widgets.ToastSuccess)
	m.debug.logf("bgsend started id=%s", msg.id)

	return m, nil
}

// pickWorkerTxFile commits the wizard's [f] tx-file pick through the
// same ApplySettings seam as §L (the repository reloads live; the
// retired pickFormTxFile idiom), then refreshes the wizard's
// candidates: SetState re-aligns the selections BY NAME, so checked /
// picked rows survive a reload whose names still exist, and the wizard
// stays on step 1 (the UAT round 4 picker contract).
func (m *RootModel) pickWorkerTxFile(path string) (tea.Model, tea.Cmd) {
	if m.workerWiz == nil || m.app == nil {
		return m, nil
	}
	st := m.workerWiz.State()
	errs := m.app.ApplySettings(context.Background(), map[string]string{app.SettingTxFile: path})
	if e, bad := errs[app.SettingTxFile]; bad {
		st.Error = e
		m.workerWiz.SetState(st)

		return m, nil
	}
	st.TxItems = m.workerTxItems()
	st.Error = ""
	m.workerWiz.SetState(st)
	m.pushToast("tx file loaded: "+filepath.Base(path), widgets.ToastSuccess)

	return m, nil
}

// workerTxNames lists the loaded transaction names (the survey
// prompts' option source); a nil app/repo yields none.
func (m *RootModel) workerTxNames() []string {
	if m.app == nil {
		return nil
	}
	repo := m.app.Transactions()
	if repo == nil {
		return nil
	}

	return repo.ListNames()
}

// workerTxItems projects the repository names into wizard candidates
// (label = path = name: the start legs take NAMES, not file paths).
func (m *RootModel) workerTxItems() []pages.WizardItem {
	names := m.workerTxNames()
	items := make([]pages.WizardItem, 0, len(names))
	for _, n := range names {
		items = append(items, pages.WizardItem{Label: n, Path: n})
	}

	return items
}
