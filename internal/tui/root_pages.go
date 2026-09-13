package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// newPages constructs the §A-§L pages and initializes the worker/settings maps.
func (m *RootModel) newPages() {
	m.dash = pages.NewDashboard(nil)
	m.tx = pages.NewTransactions(nil)
	m.inspector = pages.NewInspector(nil)
	m.send = pages.NewSend(nil)
	m.sendHistory = pages.NewSendHistory(nil)
	m.scenarios = pages.NewScenarios(nil)
	m.server = pages.NewServer(nil)
	m.workers = pages.NewWorkers(nil)
	m.sessions = pages.NewSessions(nil)
	m.analyze = pages.NewAnalyze(nil)
	m.ctf = pages.NewCtf(nil)
	m.settings = pages.NewSettings(nil)
	m.workerRows = map[string]*workerRowState{}
	m.workerRuns = map[string]workerRunParams{}
	m.toastTTL = toastDefaultTTL
	m.homeDir, _ = os.UserHomeDir()
	// SCR-512: the §L snapshot loads on first entry (dirty until the
	// first settingsLoadedMsg folds).
	m.settingsDirty = true
	m.settingsErrs = map[string]string{}
	m.settingsChanged = map[string]string{}
}

// registerPages fills the hotkey registry in PageIDs (wireframe) order, then
// appends the inspector and settings pages that claim no digit, and seeds the
// navigation stack with the first page.
func (m *RootModel) registerPages() {
	// Hotkey slots first, in PageIDs (wireframe) order...
	for _, id := range PageIDs {
		switch id {
		case pages.DashboardPageID:
			m.registry = append(m.registry, m.dash)
		case pages.TransactionsPageID:
			m.registry = append(m.registry, m.tx)
		case pages.ScenariosPageID:
			m.registry = append(m.registry, m.scenarios)
		case pages.ServerPageID:
			m.registry = append(m.registry, m.server)
		case pages.WorkersPageID:
			m.registry = append(m.registry, m.workers)
		case pages.SessionsPageID:
			m.registry = append(m.registry, m.sessions)
		case pages.AnalyzePageID:
			m.registry = append(m.registry, m.analyze)
		case pages.CtfPageID:
			m.registry = append(m.registry, m.ctf)
		}
	}
	// ...then the pages that claim no digit: the §C inspector (entered
	// with Enter on a transactions row) and the §L settings page
	// (palette ":settings" jump via jumpToID).
	m.registry = append(m.registry, m.inspector, m.settings)
	m.stack = []Page{m.registry[0]}
}

// syncAll pushes the initial snapshot into every page.
func (m *RootModel) syncAll() {
	m.syncDashboard()
	m.syncTransactions()
	m.syncScenarios()
	m.syncServer()
	m.syncWorkers()
	m.syncSessions()
	m.syncAnalyze()
	m.syncCtf()
	m.syncSettings()
}

// syncPages pushes the current snapshot into every page and open overlay.
func (m *RootModel) syncPages() {
	m.syncDashboard()
	m.syncTransactions()
	m.syncSend()
	m.syncScenarios()
	m.syncServer()
	m.syncWorkers()
	m.syncSessions()
	m.syncAnalyze()
	m.syncCtf()
	m.syncSettings()
}

// updateOverlays forwards window-size messages to the open dialog and wizard
// overlays and re-syncs the connect dialog.
func (m *RootModel) updateOverlays(msg tea.Msg) {
	if m.dlg != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			_, _ = m.dlg.Update(innerWSOf(ws))
		}
		m.syncConnect()
	}
	if m.wizard != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			_, _ = m.wizard.Update(innerWSOf(ws))
		}
	}
	if m.serverDlg != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			_, _ = m.serverDlg.Update(innerWSOf(ws))
		}
	}
	if m.workerWiz != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			_, _ = m.workerWiz.Update(innerWSOf(ws))
		}
	}
}

// armBatches batches every currently-due per-page arm command (event bridge,
// ticks and loads) onto cmd.
func (m *RootModel) armBatches(cmd tea.Cmd) tea.Cmd {
	if arm := m.armBridgeCmd(); arm != nil {
		cmd = tea.Batch(arm, cmd)
	}
	// The §G stats tick runs while a snapshot consumer (§A dashboard or
	// the §G server page) is current and the server is running;
	// arm/disarm bookkeeping lives in armServerTick.
	if tick := m.armServerTick(); tick != nil {
		cmd = tea.Batch(tick, cmd)
	}
	// The §H runtime-refresh tick runs only while the page is current
	// and a worker is active (the same seq-token lifecycle).
	if tick := m.armWorkersTick(); tick != nil {
		cmd = tea.Batch(tick, cmd)
	}
	// The §I queries run only while the page is current, off the UI
	// thread (tea.Cmd), on entry / after r / after a DB-writing event.
	if load := m.armSessions(); load != nil {
		cmd = tea.Batch(load, cmd)
	}
	// The §K queries run only while the page is current, off the UI
	// thread (tea.Cmd), on entry and after r.
	if load := m.armCtf(); load != nil {
		cmd = tea.Batch(load, cmd)
	}
	// The §L snapshot load runs only while the page is current, off
	// the UI thread (tea.Cmd), on entry / after r / after an apply.
	if load := m.armSettings(); load != nil {
		cmd = tea.Batch(load, cmd)
	}
	// The toast prune tick runs only while toasts are visible (TUI-406b).
	if tick := m.armToastTick(); tick != nil {
		cmd = tea.Batch(tick, cmd)
	}
	// The §A SESSION stats read runs off the UI thread (tea.Cmd) on the
	// ~2s tick — re-armed only while the dashboard is current or a
	// DB-writing event dirtied it (the §G seq-token lifecycle).
	if tick := m.armSessionStatsTick(); tick != nil {
		cmd = tea.Batch(tick, cmd)
	}

	return cmd
}
