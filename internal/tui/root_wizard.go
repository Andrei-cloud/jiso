// root_wizard.go owns the send-wizard legs: the modal is
// a pages.SendWizard overlay (like m.dlg — the page stack is untouched);
// the root builds its snapshots from the same sources every other screen
// uses (config spec/tx paths, the tx file's transaction entries, the
// connection truth), runs the connect step through the shared
// armConnectAttempt loop, commits the spec/tx-file picks with the §L
// ApplySettings semantics on Enter of the send step, and then starts the
// existing §D walk — the wizard never implements a send path of its own.
package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
)

// wizardTemplateLimit caps the template list (a tx file with hundreds of
// entries is a data-file mistake; the filter still reaches them via path).
const wizardTemplateLimit = 200

// openWizard opens (or re-focuses) the send wizard: three steps when a
// connection is live, four (connect first) when it is not — "send selected
// with connection settings undefined opens connection settings first".
func (m *RootModel) openWizard() (tea.Model, tea.Cmd) {
	if m.wizard != nil {
		return m, nil
	}
	th := m.themeOrNil()
	w := pages.NewSendWizard(th)
	st := pages.WizardState{
		Steps: []string{pages.WizardStepSpec, pages.WizardStepFile, pages.WizardStepSend},
	}
	if cfg := m.configOrNil(); cfg != nil {
		st.SpecItems = wizardSpecItems(cfg.GetSpec())
		st.FileItems = wizardFileItems(m.themeOrNil(), m.wizardFileValue(cfg), m.wizardFiles)
		if host := cfg.GetHost(); host != "" {
			st.Target = host + ":" + cfg.GetPort()
		}
	}
	if m.connectionLive() {
		st.TargetOK = true
	} else {
		// No live connection: the wizard opens with the connect step
		// first.
		st.Steps = append([]string{pages.WizardStepConnect}, st.Steps...)
		form := m.buildConnectForm()
		applyConnectRules(&form)
		m.stampTLSNote(&form)
		w.SetConnectForm(form)
	}
	if f := cfg0(m); f != "" {
		st.Templates = wizardTemplates(m.themeOrNil(), f)
	}
	m.pushWizardFileRecents()
	w.SetState(st)
	w.HomeCursor()
	// With a live connection, a loaded spec and a tx file
	// that carries templates, the wizard opens on the SEND step — the
	// operator only re-walks spec/file when one of them is actually
	// missing (Esc still steps back through the rail to change a pick).
	if m.connectionLive() && len(st.Templates) > 0 {
		if cfg := m.configOrNil(); cfg != nil && strings.TrimSpace(cfg.GetSpec()) != "" {
			w.HomeOnSend()
		}
	}
	if m.width > 0 {
		_, _ = w.Update(m.innerWS())
	}
	m.wizard = m.withWizardSpecDefaults(w)
	m.debug.logf("send wizard open steps=%d", len(st.Steps))

	return m, nil
}

// cfg0 is the config's current tx-file path ("" when unwired).
func cfg0(m *RootModel) string {
	if cfg := m.configOrNil(); cfg != nil {
		return strings.TrimSpace(cfg.GetFile())
	}

	return ""
}

// withWizardSpecDefaults seeds wizardSpec/wizardFile from the live config
// so Enter sends the current selection even when the user never touches
// steps 1-2.
func (m *RootModel) withWizardSpecDefaults(w *pages.SendWizard) *pages.SendWizard {
	if cfg := m.configOrNil(); cfg != nil {
		m.wizardSpec = strings.TrimSpace(cfg.GetSpec())
		m.wizardFile = strings.TrimSpace(cfg.GetFile())
	}

	return w
}

// pushWizardFileRecents remembers the current tx file for the step-2
// recents (session-only, deduped, newest first).
func (m *RootModel) pushWizardFileRecents() {
	f := cfg0(m)
	if f == "" {
		return
	}
	m.wizardFiles = dedupePaths(append([]string{f}, m.wizardFiles...))
}

// updateWizardKey routes one key while the wizard owns the keyboard.
func (m *RootModel) updateWizardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// An empty file step is a dead end for Enter: nothing is listed to
	// pick. Send the user straight into the browser instead (a tx
	// file that was never set could not be chosen).
	if key.Matches(msg, wizardEnterKey) && m.wizard.CurrentStepID() == pages.WizardStepFile &&
		len(m.wizard.State().FileItems) == 0 {
		return m.wizardBrowse(false)
	}
	// An empty send step (typed path with no transactions) gets the
	// same escape: Enter browses for a real tx file.
	if key.Matches(msg, wizardEnterKey) && m.wizard.CurrentStepID() == pages.WizardStepSend &&
		len(m.wizard.State().Templates) == 0 {
		return m.wizardBrowse(false)
	}
	_, cmd := m.wizard.Update(msg)

	return m, cmd
}

// wizardFileValue is the file step's current: the wizard's own pick
// wins over the config (same precedence the browse uses), so backing
// out of the send step still homes the cursor on what was just chosen.
func (m *RootModel) wizardFileValue(cfg *config.Config) string {
	if m.wizardFile != "" {
		return m.wizardFile
	}

	return cfg.GetFile()
}

// wizardEnterKey matches Enter for the empty-file-step intercept.
var wizardEnterKey = key.NewBinding(key.WithKeys(theme.KeyEnter))

// wizardPick targets route a picker selection back into the wizard
// instead of the settings commit seam.
const (
	wizardPickSpecTarget = "wizard:spec"
	wizardPickFileTarget = "wizard:file"
)

// wizardBrowse opens the shared root file picker over the wizard,
// starting at the step's current value (the wizard's own pick wins over
// the config). The wizard stays open underneath; Esc returns to it and
// a selection flows through applyFilePicked.
func (m *RootModel) wizardBrowse(spec bool) (tea.Model, tea.Cmd) {
	if m.wizard == nil || m.filePick != nil {
		return m, nil
	}
	value := ""
	if cfg := m.configOrNil(); cfg != nil {
		if spec {
			value = cfg.GetSpec()
		} else {
			value = cfg.GetFile()
		}
	}
	if spec && m.wizardSpec != "" {
		value = m.wizardSpec
	}
	if !spec && m.wizardFile != "" {
		value = m.wizardFile
	}
	start := "."
	if value != "" {
		start = filepath.Dir(value)
	}
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}
	target := wizardPickFileTarget
	if spec {
		target = wizardPickSpecTarget
	}

	return m.openFilePicker(OpenFilePickerMsg{
		Target: target, Root: "/", RootLabel: start + string(filepath.Separator),
		Start: start, Exts: []string{jsonExt},
	})
}

// handleWizardMsg interprets one wizard message.
func (m *RootModel) handleWizardMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case palette.OpenSendWizardMsg:
		next, cmd := m.openWizard()

		return next, cmd, true

	case palette.DirectSendMsg:
		next, cmd := m.directSend()

		return next, cmd, true

	case pages.WizardConnectAttemptMsg:
		if m.wizard == nil || m.wizard.ConnectForm() == nil {
			return m, nil, true
		}
		next, cmd := m.armConnectAttempt(m.wizard.ConnectForm().State(), m.wizard.ConnectForm())

		return next, cmd, true

	case pages.WizardCancelMsg:
		m.closeWizard()

		return m, nil, true

	case pages.WizardChooseSpecMsg:
		next, cmd := m.wizardChooseSpec(msg.Path)

		return next, cmd, true

	case pages.WizardChooseFileMsg:
		next, cmd := m.wizardChooseFile(msg.Path)

		return next, cmd, true

	case pages.WizardSendMsg:
		next, cmd := m.wizardSend(msg.Name)

		return next, cmd, true

	case pages.WizardBrowseMsg:
		next, cmd := m.wizardBrowse(msg.IsSpec)

		return next, cmd, true
	}

	return m, nil, false
}

// directSend is the one-keystroke send: with a live
// connection and a loaded spec + tx file it starts the send without
// walking the wizard at all — from the dashboard the operator stays put
// and the LAST SEND tile carries the outcome; from any other page the §D
// walk opens as before. Anything unresolved (offline, no file, empty
// file) falls back to the wizard, which asks only for what is missing.
func (m *RootModel) directSend() (tea.Model, tea.Cmd) {
	name, ok := m.resolveSessionTemplate()
	if !ok || !m.connectionLive() {
		return m.openWizard()
	}
	if cfg := m.configOrNil(); cfg == nil || strings.TrimSpace(cfg.GetSpec()) == "" {
		return m.openWizard() // no spec: the wizard's spec step must run first
	}
	m.lastSentTemplate = name
	if m.Current().ID() == pages.DashboardPageID {
		// The dashboard send keeps the operator on the
		// dashboard; the LAST SEND tile carries the outcome.
		return m, m.startSendDetached(name)
	}

	return m, m.startSend(name)
}

// resolveSessionTemplate picks the one-keystroke send's template: the
// last one sent this session when the loaded file still carries it, else
// the file's first entry. ok=false when no tx file is
// loaded or it holds no transaction entries.
func (m *RootModel) resolveSessionTemplate() (string, bool) {
	if m.app == nil {
		return "", false
	}
	tpls := wizardTemplates(m.themeOrNil(), cfg0(m))
	if len(tpls) == 0 {
		return "", false
	}
	if m.lastSentTemplate != "" {
		for _, t := range tpls {
			if t.Name == m.lastSentTemplate {
				return t.Name, true
			}
		}
	}

	return tpls[0].Name, true
}

// closeWizard dismisses the modal and cancels an in-flight connect step
// (the attempt loop keeps the only cancel handle; applyConnectResult
// turns the cancel into a silent stamp after the wizard is gone).
func (m *RootModel) closeWizard() {
	if m.wizard != nil && m.wizard.ConnectForm() != nil && m.connectRun != nil && m.connectHost == m.wizard.ConnectForm() {
		m.connectRun.cancel()
		m.connectRun, m.connectHost = nil, nil
	}
	m.wizard = nil
	m.connectInitiated = false // same retirement as the §E dialog esc
	m.debug.logf("send wizard close")
}

// wizardChooseSpec records the step-1 pick, advances, and refreshes the
// step-2 recents (the tx files that live next to the spec are as good a
// "recent" heuristic as any until the session DB grows a history).
func (m *RootModel) wizardChooseSpec(path string) (tea.Model, tea.Cmd) {
	m.wizardSpec = path
	m.wizard.AdvanceStep()
	st := m.wizard.State()
	if cfg := m.configOrNil(); cfg != nil {
		st.FileItems = wizardFileItems(m.themeOrNil(), m.wizardFileValue(cfg), m.wizardFiles)
	}
	m.wizard.SetState(st)

	return m, nil
}

// wizardChooseFile validates the step-2 pick, loads its templates, and
// advances; a missing file stays on the step with the inline error line.
func (m *RootModel) wizardChooseFile(path string) (tea.Model, tea.Cmd) {
	abs, err := filepath.Abs(path)
	if err == nil {
		if fi, serr := os.Stat(abs); serr != nil || fi.IsDir() {
			abs = path
		}
	}
	if _, serr := os.Stat(abs); serr != nil {
		if _, serr2 := os.Stat(path); serr2 != nil {
			st := m.wizard.State()
			st.Error = "no such file: " + path
			m.wizard.SetState(st)
			m.debug.logf("wizard file pick missing %s", path)
			// The listing was stale — the picked file is gone at apply
			// time. The modal names the full path; the step keeps its
			// inline line.
			m.openErrorModal("cannot open file", errors.New(st.Error))

			return m, nil
		}
		abs = path
	}
	tpls := wizardTemplates(m.themeOrNil(), abs)
	if len(tpls) == 0 {
		// Not a tx file (a spec, a lock file, an empty pool): stay on
		// the step with a line naming it instead of advancing into an
		// empty send step ("no templates" dead end).
		st := m.wizard.State()
		st.Error = "no transactions in " + filepath.Base(abs) + " - pick a tx file"
		m.wizard.SetState(st)
		m.debug.logf("wizard file pick rejected %s: no templates", abs)

		return m, nil
	}
	m.wizardFile = abs
	m.wizardFiles = dedupePaths(append([]string{abs}, m.wizardFiles...))
	st := m.wizard.State()
	st.Error = ""
	st.Templates = tpls
	m.wizard.AdvanceStep()
	m.wizard.SetState(st)
	m.debug.logf("wizard file pick %s templates=%d", abs, len(st.Templates))

	return m, nil
}

// wizardSend commits the picks (the same ApplySettings validation §L uses,
// per-field errors surfaced inline) and starts the §D walk, closing the
// wizard. A failed commit keeps the wizard open on the send step with the
// validation line.
func (m *RootModel) wizardSend(name string) (tea.Model, tea.Cmd) {
	if m.app == nil {
		st := m.wizard.State()
		st.Error = errNoAppWired
		m.wizard.SetState(st)

		return m, nil
	}
	patch := map[string]string{}
	if cfg := m.configOrNil(); cfg != nil {
		if m.wizardSpec != "" && m.wizardSpec != strings.TrimSpace(cfg.GetSpec()) {
			patch[app.SettingSpec] = m.wizardSpec
		}
		if m.wizardFile != "" && m.wizardFile != strings.TrimSpace(cfg.GetFile()) {
			patch[app.SettingTxFile] = m.wizardFile
		}
	}
	if len(patch) > 0 {
		if errs := m.app.ApplySettings(context.Background(), patch); len(errs) > 0 {
			st := m.wizard.State()
			msgs := make([]string, 0, len(errs))
			for _, e := range errs {
				msgs = append(msgs, e)
			}
			st.Error = strings.Join(msgs, "; ")
			m.wizard.SetState(st)
			m.debug.logf("wizard commit failed %s", st.Error)

			return m, nil
		}
	}
	m.wizard = nil
	m.lastSentTemplate = name
	m.pushWizardFileRecents()
	m.debug.logf("wizard send tx=%s", name)

	return m, m.startSend(name)
}
