// root_settings.go owns the §L settings truth. The page is
// presentation-only: root queries the App settings façade
// (CurrentSettings/ApplySettings/SaveSettings — the same userconfig
// loader the CLI-104 precedence layer reads) OFF the UI thread: every
// leg runs in a tea.Cmd and reports back as a seq-tokened msg, so
// Update never blocks and never touches the filesystem. A committed
// field is applied as a one-key patch (per-field validation returns
// inline; valid siblings still mutate the session config, so the NEXT
// connect/send/serve sees them — already-dialed connections keep
// their values, which the page footer states). w opens the save
// confirm overlay with the changed-keys diff; w inside it persists
// ONLY those keys through SaveSettings, Esc closes it and writes
// nothing. settingsSrc overrides the app legs for tests (fake façade;
// no optimistic state — changes land only when a result msg arrives).
package tui

import (
	"context"
	"errors"
	"strconv"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// settingsSource is the §L façade leg: the App methods match it
// structurally, and tests inject a fake (no real user file above the
// seam — the fake never touches disk).
type settingsSource interface {
	CurrentSettings() (app.SettingsView, error)
	ApplySettings(ctx context.Context, patch map[string]string) map[string]string
	SaveSettings(ctx context.Context, patch map[string]string) (string, error)
}

// settingsSource resolves the injectable leg (nil = the App façade;
// nil App = no leg, the page keeps its empty state).
func (m *RootModel) settingsSource() settingsSource {
	if m.settingsSrc != nil {
		return m.settingsSrc
	}
	if m.app == nil {
		return nil
	}

	return m.app
}

// Result messages from the tea.Cmd goroutines; seq marks the load
// generation (a stale seq is ignored — the serverTickSeq lifecycle).
type (
	settingsLoadedMsg struct {
		seq  uint64
		view app.SettingsView
		err  error
	}
	settingsAppliedMsg struct {
		seq   uint64
		patch map[string]string
		errs  map[string]string
	}
	settingsSavedMsg struct {
		seq   uint64
		path  string
		count int
		err   error
	}
)

// armSettings runs the snapshot query while the page is current: on
// entry, after `r`, and after an apply dirties the values. Returns nil
// otherwise (the query never runs for an unfocused page).
func (m *RootModel) armSettings() tea.Cmd {
	if m.Current() == nil || m.Current().ID() != pages.SettingsPageID {
		return nil
	}
	src := m.settingsSource()
	if src == nil {
		return nil
	}
	if m.settingsLoadWait || !m.settingsDirty {
		return nil
	}
	// Arm the single-flight flag (the arm side used to write
	// false, so nothing ever marked the load in flight — the stale
	// wedge the reviewer flagged could not even be observed, and two
	// dirtying events in one round-trip armed two concurrent loads).
	m.settingsLoadWait, m.settingsDirty = true, false
	m.settingsSeq++
	seq := m.settingsSeq

	return func() tea.Msg {
		view, err := src.CurrentSettings()

		return settingsLoadedMsg{seq: seq, view: view, err: err}
	}
}

// applySettingsLoaded folds a snapshot result: a malformed user config
// file degrades to the Note line (rows still render), and the pending
// save-overlay diff is rebuilt against the fresh file values. The wait
// flag clears BEFORE the stale check (the
// applySessionsDetail pattern): a commit or save bumps the seq while a
// load is in flight, and a stale-return that kept settingsLoadWait true
// permanently froze armSettings for the session.
func (m *RootModel) applySettingsLoaded(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	m.settingsLoadWait = false
	if msg.seq != m.settingsSeq {
		return m, nil
	}
	m.settingsLoaded = true
	m.settingsView = &msg.view
	m.settingsNote = ""
	if msg.err != nil {
		// A malformed user config file degrades the config layer
		// (the façade still returns the session rows); name it.
		m.settingsNote = "user config unreadable: " + msg.err.Error()
	}

	return m, nil
}

// handleSettingsCommit is Enter inside a field: validate + live-apply
// the one-key patch off the UI thread. A pending apply/save ignores
// the commit (the page keeps its draft visible).
func (m *RootModel) handleSettingsCommit(msg pages.SettingsCommitMsg) (tea.Model, tea.Cmd) {
	if m.Current().ID() != pages.SettingsPageID || m.settingsApplyWait || m.settingsSaveWait {
		return m, nil
	}

	return m.commitSettingKey(msg.Key, msg.Value)
}

// commitSettingKey runs the shared one-key apply leg: this IS the §L
// commit path, extracted so a file-picker selection on another page
// (the §B tx-file target) commits through exactly the same
// validation/apply semantics instead of a parallel implementation.
func (m *RootModel) commitSettingKey(key, value string) (tea.Model, tea.Cmd) {
	src := m.settingsSource()
	if src == nil {
		m.settingsNote = "settings unavailable: no app session"

		return m, nil
	}
	delete(m.settingsErrs, key)
	m.settingsNote = ""
	m.settingsSavedLine = ""

	return m.commitSettingPatch(src, map[string]string{key: value})
}

// commitSettingPatch arms the async ApplySettings leg shared by every
// commit shape; multi-key ordering belongs to the App side.
func (m *RootModel) commitSettingPatch(src settingsSource, patch map[string]string) (tea.Model, tea.Cmd) {
	m.settingsApplyWait = true
	m.settingsSeq++
	seq := m.settingsSeq

	return m, func() tea.Msg {
		return settingsAppliedMsg{seq: seq, patch: patch, errs: src.ApplySettings(context.Background(), patch)}
	}
}

// applySettingsApplied folds the per-field validation result: errored
// fields surface inline (the snapshot keeps the old value; the page
// shows the attempted draft beside the error), accepted fields are
// recorded as changed and dirty the snapshot for the wrapper's arm.
func (m *RootModel) applySettingsApplied(msg settingsAppliedMsg) (tea.Model, tea.Cmd) {
	m.settingsApplyWait = false
	if msg.seq != m.settingsSeq {
		return m, nil // a newer commit or a step jump superseded this result
	}
	// A tx-file picked from §B (not the §L settings grid): applyTxFileSetting
	// already swapped the live collection on success; when it was rejected we
	// must surface WHY on the transactions page instead of falling back to
	// the empty state in silence.
	if m.txFilePickFromB {
		m.txFilePickFromB = false
		m.txFileLoadErr = msg.errs[app.SettingTxFile]
		if m.txFileLoadErr != "" {
			// An explicit load the user just asked for failed: the modal
			// makes the whole reason readable over §B's empty state.
			m.openErrorModal("cannot load transaction file", errors.New(m.txFileLoadErr))
		} else if e, bad := msg.errs[app.SettingSpec]; bad {
			// A chained spec pick whose SPEC failed to load: the apply loop
			// is per-field, so the tx file landed against the previous live
			// spec — the modal names the rejected spec (§L's shape). The
			// inline line stays empty on purpose: the loaded file keeps its
			// table under the true spec chip, where a load-error body would
			// claim nothing loaded.
			m.openErrorModal("cannot load specification file", errors.New(e))
		} else {
			m.debug.logf("tx file loaded from §B: %s", msg.patch[app.SettingTxFile])
		}

		return m, nil
	}
	if m.Current().ID() != pages.SettingsPageID {
		return m, nil
	}
	if m.settingsErrs == nil {
		m.settingsErrs = map[string]string{}
	}
	for key, err := range msg.errs {
		m.settingsErrs[key] = err
	}
	// The two file-load keys report through the error screen too (the grid
	// keeps its inline row error beside the draft): a rejected spec or
	// tx-file is a failed load, not a malformed keystroke.
	if e, bad := msg.errs[app.SettingTxFile]; bad {
		m.openErrorModal("cannot load transaction file", errors.New(e))
	} else if e, bad := msg.errs[app.SettingSpec]; bad {
		m.openErrorModal("cannot load specification file", errors.New(e))
	}
	for key := range msg.patch {
		if _, bad := msg.errs[key]; bad {
			continue
		}
		delete(m.settingsErrs, key)
		m.settingsChanged[key] = msg.patch[key]
	}
	m.settingsDirty = true // re-read the snapshot (new values + sources)

	return m, m.armSettings()
}

// handleSettingsSave is w on the grid: open the save confirm overlay
// with the changed-keys diff; a clean session only stamps the Note
// (no overlay, no write).
func (m *RootModel) handleSettingsSave() (tea.Model, tea.Cmd) {
	if m.Current().ID() != pages.SettingsPageID || m.settingsSaveWait || m.settingsApplyWait {
		return m, nil
	}
	m.settingsSavedLine = ""
	if len(m.settingsChanged) == 0 {
		m.settingsNote = "no changes to save"

		return m, nil
	}
	m.settingsSaveOpen = true

	return m, nil
}

// handleSettingsSaveConfirm is w inside the overlay: persist exactly
// the changed keys (SaveSettings re-validates and writes ONLY them;
// one invalid key aborts the write and surfaces as the failure line).
// An apply still in flight also blocks the save: its key
// has not landed in settingsChanged yet, and saving now would miss the
// just-committed value.
func (m *RootModel) handleSettingsSaveConfirm() (tea.Model, tea.Cmd) {
	if m.Current().ID() != pages.SettingsPageID || !m.settingsSaveOpen ||
		m.settingsSaveWait || m.settingsApplyWait {
		return m, nil
	}
	src := m.settingsSource()
	if src == nil {
		m.settingsSaveOpen = false
		m.settingsSavedLine = "save failed: no app session"
		m.settingsSavedOK = false

		return m, nil
	}
	patch := make(map[string]string, len(m.settingsChanged))
	for k, v := range m.settingsChanged {
		patch[k] = v
	}
	m.settingsSaveOpen = false
	m.settingsSaveWait = true
	m.settingsSeq++
	seq := m.settingsSeq

	return m, func() tea.Msg {
		path, err := src.SaveSettings(context.Background(), patch)

		return settingsSavedMsg{seq: seq, path: path, count: len(patch), err: err}
	}
}

// applySettingsSaved folds the write result: success clears the
// changed set (the file now agrees), marks the snapshot dirty so the
// sources refresh, and stamps the toast-style line; failure keeps the
// changes pending and names the path in the line.
func (m *RootModel) applySettingsSaved(msg settingsSavedMsg) (tea.Model, tea.Cmd) {
	m.settingsSaveWait = false
	if msg.seq != m.settingsSeq || m.Current().ID() != pages.SettingsPageID {
		return m, nil
	}
	if msg.err != nil {
		m.settingsSavedLine = "save failed: " + msg.err.Error()
		m.settingsSavedOK = false

		return m, nil
	}
	m.settingsSavedLine = "saved " + strconv.Itoa(msg.count) + " key(s) to " + msg.path
	m.settingsSavedOK = true
	m.settingsChanged = map[string]string{}
	m.settingsDirty = true
	// The save-success line also surfaces as a transient
	// toast (bottom-right); the page's own footer line stays (the
	// page owns that UX). Timestamped with the injectable now; the root's
	// prune tick expires it.
	m.pushToast(m.settingsSavedLine, widgets.ToastSuccess)

	return m, m.armSettings()
}

// handleSettingsSaveCancel is Esc inside the overlay: nothing is
// written; the live edits stay session-only (the save step's
// "Esc discards").
func (m *RootModel) handleSettingsSaveCancel() (tea.Model, tea.Cmd) {
	m.settingsSaveOpen = false

	return m, nil
}

// handleSettingsRefresh marks the snapshot dirty (`r`); the wrapper's
// arm re-queries on the spot (the page is current by construction).
func (m *RootModel) handleSettingsRefresh() (tea.Model, tea.Cmd) {
	m.settingsDirty = true

	return m, m.armSettings()
}

// leaveSettings bumps the seq when navigation replaces/pushes away
// from the §L page (the leave-side cancel pattern).
func (m *RootModel) leaveSettings() {
	if m.Current() != nil && m.Current().ID() == pages.SettingsPageID {
		m.settingsSeq++
		m.settingsLoadWait, m.settingsApplyWait, m.settingsSaveWait = false, false, false
		m.settingsSaveOpen = false
		m.closeFilePicker()
	}
}
