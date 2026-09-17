// root_routes.go holds the page-action message routers split out of update's
// giant type switch. Each returns (nil, nil) for a message it does not own, so
// update can try them in order and forward anything none claims.
package tui

import (
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"

	tea "charm.land/bubbletea/v2"
)

// routeCoreMsg routes the coremsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeCoreMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:

		return m.updateKey(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case resizeFlushMsg:
		return m.handleResizeFlush()
	case bridge.Msg:
		return m.updateBridgeMsg(msg)
	case palette.GoToPageMsg:
		m.jumpToID(msg.ID)

		return m, nil

	case palette.PushPageMsg:
		m.pushByID(msg.ID)

		return m, nil

	default:
		return nil, nil
	}
}

// routeExchangeMsg routes the exchangemsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeExchangeMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.TxPopMsg:
		m.popPage()

		return m, nil

	case pages.TxDetailMsg, pages.TxSendMsg, pages.TxPickFileMsg, pages.TxComposeMsg:
		return m.handleTxMsg(msg)

	case pages.InspectorPopMsg:
		// Esc on the inspector: root owns the stack; at depth 1 (hotkey 3
		// jump) the stack never empties, so this is a no-op there.
		m.popPage()

		return m, nil

	case pages.SendPopMsg:
		// Esc on the §D exchange view (same pop rule; the in-flight op, if
		// any, keeps reporting into sendRun — its stages stay truthful).
		m.popPage()

		return m, nil

	case pages.SendHistoryPickMsg:
		// Enter on a send-history row: freeze §D on that run
		// (its h toggle is the detail ↔ hex view).
		return m.sendHistoryDetail(msg)

	case pages.SendHistoryPopMsg:
		// Esc on the send-history overlay.
		m.popPage()

		return m, nil

	default:
		return nil, nil
	}
}

// routeSendConsoleMsg routes the sendconsolemsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeSendConsoleMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SendStageMsg:
		return m.applySendStage(msg)

	case sendElapsedMsg:
		return m.tickSendElapsed(msg)

	case consoleLineMsg:
		// System output from the connection manager: stamped into the
		// bottom console strip, never stderr.
		m.appendConsoleLine(msg.text)

		return m, nil

	case serverLineMsg:
		// Mock-server output stays inside the server page's own LOG
		// ring, never the global strip; lines are receipt-timestamped
		// so the log reads as a timeline.
		m.serverLog = append(m.serverLog, m.stampLine(msg.text))
		if len(m.serverLog) > consoleRingMax {
			m.serverLog = m.serverLog[len(m.serverLog)-consoleRingMax:]
		}

		return m, nil

	case palette.OpenConnectMsg:
		// Palette ":connect" and the dashboard quick action land here —
		// the same entry point as the global "c" hotkey (openConnect).
		return m.openConnect()

	case palette.DisconnectMsg:
		// Palette ":disconnect" and the §A "D" quick key land here —
		// the router decides (leg, confirm, or sane no-op).
		return m.handleDisconnect()

	case palette.QuitRequestMsg:
		// ":quit" confirms before the application exits.
		return m.requestQuit()

	case palette.LastSendViewMsg:
		// Reopen the last completed §D snapshot (Enter there resends
		// through the normal TxSendMsg path).
		return m.viewLastSend()

	case palette.SendHistoryMsg:
		// Open the session send-history overlay (Enter freezes §D on
		// a row; the router toasts when nothing sent yet).
		return m.openSendHistory()

	case palette.LastStressSummaryMsg:
		// Reopen the last completed stress run's summary overlay
		// (the §A LAST STRESS card's row; toasts when none completed).
		return m.viewLastStressSummary()

	default:
		return nil, nil
	}
}

// routeConnectResultMsg routes the connectresultmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeConnectResultMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ConnectAttemptMsg:
		return m.applyConnectAttempt(msg)

	case ConnectResultMsg:
		return m.applyConnectResult(msg)

	case disconnectResultMsg:
		return m.applyDisconnectResult(msg)

	default:
		return nil, nil
	}
}

// routeScenarioMsg routes the scenariomsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeScenarioMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.ScenarioRunMsg:
		m.debug.logf("scenario run id=%s", msg.ID)

		return m.startScenarioRun(msg.ID)

	case pages.ScenarioExportMsg:
		return m.exportScenarioReport()

	case pages.ScenarioStepDetailMsg:
		m.debug.logf("scenario step detail id=%s step=%d", msg.ScenarioID, msg.StepIndex)

		return m.handleScenarioStepDetail(msg)

	case scenarioStepDetailLoadedMsg:
		return m.applyScenarioStepDetail(msg)

	case pages.ScenarioPopMsg:
		// Esc on the §F page (same pop rule as the inspector and the
		// §D exchange view: at depth 1 the stack never empties).
		m.popPage()

		return m, nil

	case scenarioStepMsg:
		return m.applyScenarioStep(msg)

	case scenarioDoneMsg:
		return m.applyScenarioDone(msg)

	case scenarioExportedMsg:
		return m.applyScenarioExported(msg)

	case scenarioExportStatMsg:
		return m.applyScenarioExportStat(msg)

	default:
		return nil, nil
	}
}

// routeServerMsg routes the servermsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeServerMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.ServerStopMsg:
		// `s` on the §G page: confirm first when connections are live
		// (widgets.ConfirmDialog), stop directly otherwise.
		return m.handleServerStop()

	case pages.ServerPopMsg:
		// Esc on the §G page (same pop rule as the other merged pages:
		// at depth 1 the stack never empties).
		m.popPage()

		return m, nil

	case serverStatsTickMsg:
		return m.applyServerStatsTick(msg)

	case serverStartResultMsg:
		return m.applyServerStartResult(msg)

	case serverStopResultMsg:
		return m.applyServerStopResult(msg)

	default:
		return nil, nil
	}
}

// routeWorkerMsg routes the workermsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeWorkerMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.WorkersOpenFormMsg:
		return m.openWorkersForm(msg.Kind)

	case pages.WorkerWizardBrowseMsg:
		// [f] on the wizard's tx step: the shared file picker over
		// .json tx files (the wizard stays open underneath).
		return m.workerWizBrowse()

	case pages.WorkerWizardCloseMsg:
		// Esc on the wizard's first step: drop the modal (an in-flight
		// start leg keeps reporting into the guarded apply seam).
		m.closeWorkerWizard()

		return m, nil

	case pages.WorkerWizardStartMsg:
		return m.startWorkerRun(msg.Run)

	case pages.WorkersStopMsg:
		return m.handleWorkerStop(msg.ID)

	case pages.WorkersStopAllMsg:
		return m.handleWorkerStopAll()

	case pages.WorkersPopMsg:
		// Esc on the §H page (same pop rule as the other merged pages;
		// the summary overlay owns Esc earlier, inside the page).
		m.popPage()

		return m, nil

	case workerRuntimeTickMsg:
		return m.applyWorkersRuntimeTick(msg)

	default:
		return nil, nil
	}
}

// routeSessionsMsg routes the sessionsmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeSessionsMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.SessionsSelectMsg:
		return m.handleSessionsSelect(msg)

	case pages.SessionsFocusMsg:
		return m.handleSessionsFocus(msg.ID)

	case pages.SessionsReviewMsg:
		return m.handleSessionsReview(msg)

	case pages.SessionsRefreshMsg:
		return m.handleSessionsRefresh()

	default:
		return nil, nil
	}
}

// routeAnalyzeMsg routes the analyzemsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeAnalyzeMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.AnalyzeNextMsg:
		return m.handleAnalyzeNext()

	case pages.AnalyzeStepDeltaMsg:
		return m.handleAnalyzeStepDelta(msg)

	case pages.AnalyzeCommitSpecMsg:
		return m.handleAnalyzeCommitSpec(msg)

	case pages.AnalyzeCommitCaptureMsg:
		return m.handleAnalyzeCommitCapture(msg)

	case pages.AnalyzeChooseGoalMsg:
		return m.handleAnalyzeChooseGoal(msg)

	case pages.AnalyzeChooseHeaderMsg:
		return m.handleAnalyzeChooseHeader(msg)

	case pages.AnalyzeChooseMaskMsg:
		return m.handleAnalyzeChooseMask(msg)

	case pages.AnalyzeRunMsg:
		return m.handleAnalyzeRunMsg(msg)

	case pages.AnalyzeFlowToggleMsg:
		return m.handleAnalyzeFlowToggleMsg(msg)

	case pages.AnalyzeFlowToggleAllMsg:
		return m.handleAnalyzeFlowToggleAllMsg(msg)

	case pages.AnalyzeBrowseMsg:
		return m.handleAnalyzeBrowse(msg.IsSpec)

	case pages.AnalyzeOutBrowseMsg:
		return m.handleAnalyzeOutBrowse(msg.Draft)

	case pages.AnalyzeWriteMsg:
		return m.handleAnalyzeWrite()

	case pages.AnalyzeOutCommitMsg:
		return m.handleAnalyzeOutCommit(msg)

	case pages.AnalyzeItemsApplyMsg:
		return m.handleAnalyzeItemsApply(msg)

	case pages.AnalyzeAbortMsg:
		return m.handleAnalyzeAbort()

	default:
		return nil, nil
	}
}

// routeMouseMsg routes the mouse hit-map message family: cells resolved
// by the per-frame hitMap arrive as these root-owned msgs and are
// consumed here, never reaching a page.
//
// The raw tea.MouseMsg case is load-bearing, not defensive: bubbletea
// hands every mouse event to View.OnMouse and then to Update too.
// Swallowing the raw msg here keeps the synthetic msgs the only mouse
// truth — otherwise a mouse-aware page consumer would act on the raw
// wheel *and* the synthetic scrollMsg, double-firing.
func (m *RootModel) routeMouseMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case scrollMsg:
		return m.handleScrollMsg(msg)

	case selectMsg:
		return m.handleSelectMsg(msg)

	case focusMsg:
		return m.handleFocusMsg(msg)

	case tea.MouseMsg:
		// Raw terminal mouse: already resolved by View.OnMouse against the
		// last rendered frame; replaying it into Update would double-handle.
		return m, nil

	default:
		return nil, nil
	}
}
