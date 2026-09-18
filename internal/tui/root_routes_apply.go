// root_routes_apply.go holds the async-result and confirm-dialog message routers
// split out of update's giant type switch, each returning (nil, nil) for a
// message it does not own.
package tui

import (
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"

	tea "charm.land/bubbletea/v2"
)

// routeAnalyzeApplyMsg routes the analyzeapplymsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeAnalyzeApplyMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case analyzeSpecStatMsg:
		return m.applyAnalyzeSpecStat(msg)

	case analyzeEnumLoadedMsg:
		return m.applyAnalyzeEnum(msg)

	case analyzeRunLoadedMsg:
		return m.applyAnalyzeRun(msg)

	case analyzeWriteLoadedMsg:
		return m.applyAnalyzeWrite(msg)

	case analyzeWriteStatMsg:
		return m.applyAnalyzeWriteStat(msg)

	default:
		return nil, nil
	}
}

// routeSessionsLoadedMsg routes the sessionsloadedmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeSessionsLoadedMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.SessionsPopMsg:
		// Esc on the §I page (same pop rule as the other merged pages;
		// the review overlay and the narrow drill own Esc earlier,
		// inside the page).
		m.popPage()

		return m, nil

	case sessionsListLoadedMsg:
		return m.applySessionsList(msg)

	case sessionsDetailLoadedMsg:
		return m.applySessionsDetail(msg)

	case sessionsReviewLoadedMsg:
		return m.applySessionsReview(msg)

	case sessionStatsTickMsg:
		return m.applySessionStatsTick(msg)

	default:
		return nil, nil
	}
}

// routeCtfMsg routes the ctfmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeCtfMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.CtfGenerateMsg:
		return m.handleCtfGenerate(msg)

	case pages.CtfSelectMsg:
		return m.handleCtfSelect(msg)

	case pages.CtfWriteMsg:
		return m.handleCtfWrite()

	case pages.CtfRefreshMsg:
		return m.handleCtfRefresh()

	case pages.CtfPopMsg:
		// Esc on the §K page (same pop rule as the other merged pages;
		// the preview overlay and the form own Esc earlier, inside the
		// page). leaveCtf runs FIRST: the pop must bump the
		// seq like Push/Replace do, or an in-flight list/preview/write
		// leg lands on the page the user just left.
		m.leaveCtf()
		m.popPage()

		return m, nil

	case ctfListLoadedMsg:
		return m.applyCtfList(msg)

	case ctfPreviewLoadedMsg:
		return m.applyCtfPreview(msg)

	case ctfWriteStatMsg:
		return m.applyCtfWriteStat(msg)

	case ctfWriteLoadedMsg:
		return m.applyCtfWrite(msg)

	default:
		return nil, nil
	}
}

// routeFilePickerMsg routes the filepickermsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeFilePickerMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case OpenFilePickerMsg:
		return m.openFilePicker(msg)

	case CloseFilePickerMsg:
		m.closeFilePicker()

		return m, nil

	case widgets.FilePickedMsg:
		m.debug.logf("filepick picked path=%q target=%q", msg.Path, m.filePickTarget)

		return m.applyFilePicked(msg)

	case widgets.FilePickerCanceledMsg:
		// Esc inside the picker: the widget emits its own cancel; the
		// page underneath keeps its state.
		m.cancelFilePicker()

		return m, nil

	case toastTickMsg:
		return m.applyToastTick()

	default:
		return nil, nil
	}
}

// routeSettingsMsg routes the settingsmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeSettingsMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.SettingsPickFileMsg:
		return m.handleSettingsPickFile(msg)

	case pages.SettingsCommitMsg:
		return m.handleSettingsCommit(msg)

	case pages.SettingsSaveMsg:
		return m.handleSettingsSave()

	case pages.SettingsSaveConfirmMsg:
		return m.handleSettingsSaveConfirm()

	case pages.SettingsSaveCancelMsg:
		return m.handleSettingsSaveCancel()

	case pages.SettingsRefreshMsg:
		return m.handleSettingsRefresh()

	case pages.SettingsPopMsg:
		// Esc on the §L page (same pop rule as the other merged pages;
		// the edit buffer and the save overlay own Esc earlier, inside
		// the page). leaveSettings runs FIRST: the pop must
		// bump the seq like Push/Replace do, or an in-flight
		// load/apply/save leg lands on the page the user just left.
		m.leaveSettings()
		m.popPage()

		return m, nil

	case settingsLoadedMsg:
		return m.applySettingsLoaded(msg)

	case settingsAppliedMsg:
		return m.applySettingsApplied(msg)

	case settingsSavedMsg:
		return m.applySettingsSaved(msg)

	default:
		return nil, nil
	}
}

// routeStressResultMsg routes the stressresultmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeStressResultMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case bgStartResultMsg:
		return m.applyBgStartResult(msg)

	case stressStartResultMsg:
		return m.applyStressStartResult(msg)

	case workerStopResultMsg:
		return m.applyWorkerStopResult(msg)

	case workerStopAllResultMsg:
		return m.applyWorkerStopAllResult(msg)

	default:
		return nil, nil
	}
}

// routeConfirmedMsg routes the confirmedmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeConfirmedMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case widgets.ConfirmedMsg:
		if m.serverConfirm != nil {
			return m.applyServerConfirmed()
		}
		if m.analyzeOverwriteConfirm != nil {
			return m.applyAnalyzeOverwriteConfirmed()
		}
		if m.analyzeConfirm != nil {
			return m.applyAnalyzeConfirmed()
		}
		if m.ctfConfirm != nil {
			return m.applyCtfConfirmed()
		}
		if m.scenarioConfirm != nil {
			return m.applyScenarioExportConfirmed()
		}
		if m.disconnectConfirm != nil {
			return m.applyDisconnectConfirmed()
		}

		return m.applyWorkersConfirmed()

	default:
		return nil, nil
	}
}

// routeCancelledMsg routes the cancelledmsg message family, returning (nil, nil) when a message
// belongs to no family it owns.
func (m *RootModel) routeCancelledMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case widgets.CancelledMsg:
		if m.serverConfirm != nil {
			return m.applyServerCancelled()
		}
		if m.analyzeOverwriteConfirm != nil {
			return m.applyAnalyzeOverwriteCancelled()
		}
		if m.analyzeConfirm != nil {
			return m.applyAnalyzeCancelled()
		}
		if m.ctfConfirm != nil {
			return m.applyCtfCancelled()
		}
		if m.scenarioConfirm != nil {
			return m.applyScenarioExportCancelled()
		}
		if m.disconnectConfirm != nil {
			return m.applyDisconnectCancelled()
		}

		return m.applyWorkersCancelled()

	default:
		return nil, nil
	}
}
