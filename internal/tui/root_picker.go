// root_picker.go wires the shared widgets.FilePicker and widgets.Toast into
// the router. The picker is a root-owned modal overlay; a FilePickedMsg
// commits the path through the target's own commit seam, so validation stays
// in one place. Toasts are root-side truth: the widget never reads the clock
// and a root tick prunes by age with the root's ttl.
package tui

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/tui/frame"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

const (
	// toastTickInterval is the prune poll; toastDefaultTTL is the age a
	// toast survives (owners may override m.toastTTL).
	toastTickInterval = 500 * time.Millisecond
	toastDefaultTTL   = 3 * time.Second
)

// OpenFilePickerMsg opens the picker overlay; Target names the commit seam
// the selection flows through, PickDirKey binds the extra folder-commit key.
type OpenFilePickerMsg struct {
	Target     string
	Root       string
	RootLabel  string
	Start      string
	Exts       []string
	PickDirKey string
}

// CloseFilePickerMsg closes the picker without a selection.
type CloseFilePickerMsg struct{}

// toastTickMsg is the root-side prune tick (tests inject it via
// toastTickf; the widget itself owns no clock).
type toastTickMsg struct{}

// defaultToastTick is the production prune scheduler.
func defaultToastTick(d time.Duration, mk func() tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return mk() })
}

// openFilePicker opens the modal browser sized to the content area.
// jsonExt is the extension the file pickers offer and the scenario export
// appends; both ends must agree or a picked file comes back as
// "spec.json.json".
const jsonExt = ".json"

func (m *RootModel) openFilePicker(msg OpenFilePickerMsg) (tea.Model, tea.Cmd) {
	w, h := frame.ContentSize(m.width, m.height)
	// Size the picker to the modal box's INNER width: lipgloss pads but
	// never truncates, so lines overrunning the box corrupt the frame.
	m.filePick = widgets.NewFilePicker(m.themeOrNil(), modalBoxWidth(w)-2, max(h-2, 5), widgets.FilePickerOptions{
		Root: msg.Root, RootLabel: msg.RootLabel, Start: msg.Start,
		Selectable: extPredicate(msg.Exts), PickDirKey: msg.PickDirKey,
	})
	m.filePickTarget = msg.Target
	m.debug.logf("picker open target=%s", msg.Target)

	return m, nil
}

// closeFilePicker drops the overlay and its target.
func (m *RootModel) closeFilePicker() {
	m.filePick, m.filePickTarget = nil, ""
}

// cancelFilePicker handles the picker's own cancel: dropping a
// spec-for-file browse abandons the pending tx file with it — nothing
// half-applied — and leaves one visible line saying so; `f` re-arms.
// Dropping a scenario spec browse abandons the gated run with a notice;
// a gated preview needs none, its honest line already says the cause.
func (m *RootModel) cancelFilePicker() {
	target := m.filePickTarget
	pending := m.pendingTxFile
	m.pendingTxFile = ""
	runID := m.pendingScenarioRun
	m.pendingScenarioRun = ""
	m.pendingScenarioPreviewID, m.pendingScenarioPreviewAt = "", 0
	m.closeFilePicker()
	switch {
	case target == settingsSpecForFileTarget && pending != "":
		m.pushToast("transaction file not loaded - pick a specification file first (f again)", widgets.ToastInfo)
	case target == scenarioSpecTarget && runID != "":
		m.pushToast("scenario not run - pick a specification file first", widgets.ToastInfo)
	}
}

// extPredicate builds the picker's extension filter (nil = any file).
func extPredicate(exts []string) func(string) bool {
	if len(exts) == 0 {
		return nil
	}

	return func(name string) bool {
		for _, e := range exts {
			if strings.HasSuffix(strings.ToLower(name), strings.ToLower(e)) {
				return true
			}
		}

		return false
	}
}

// settingsSpecForFileTarget routes the chained browse a specless tx-file
// pick opens: the picked spec and the pending file land in one patch.
const settingsSpecForFileTarget = "settings:spec-for-file"

// scenarioSpecTarget routes the chained browse a gated scenario run or
// step preview opens: the picked spec lands first, then the waiting work.
const scenarioSpecTarget = "scenario:runspec"

// applyFilePicked routes a selection to the target's commit seam; settings
// targets reuse commitSettingKey so validation stays in one place.
func (m *RootModel) applyFilePicked(msg widgets.FilePickedMsg) (tea.Model, tea.Cmd) {
	target := m.filePickTarget
	m.closeFilePicker()
	if target == "" {
		return m, nil
	}
	// Wizard targets feed the wizard's own choose legs, not settings.
	switch target {
	case wizardPickSpecTarget:
		return m.wizardChooseSpec(msg.Path)
	case wizardPickFileTarget:
		return m.wizardChooseFile(msg.Path)
	case stressPickFileTarget, bgPickFileTarget:
		// The worker wizard's [f] pick: load the tx file, refresh the
		// wizard candidates (selections survive by name), stay on step 1.
		return m.pickWorkerTxFile(msg.Path)
	case serverPickSpecTarget:
		return m.pickServerFormField(serverFieldSpecPath, msg.Path)
	case serverPickRoutesTarget:
		return m.pickServerFormField(serverFieldRoutes, msg.Path)
	case settingsSpecForFileTarget:
		return m.applySpecForFilePick(msg.Path)
	case scenarioSpecTarget:
		return m.applyScenarioSpecPick(msg.Path)
	case analyzePickTarget:
		// The §J capture step: the pick commits through the same
		// capture-choose leg Enter uses (validate + advance).
		return m.handleAnalyzeCommitCapture(pages.AnalyzeCommitCaptureMsg{Value: msg.Path})
	case analyzeSpecPickTarget:
		// The §J spec step: the pick commits through the same spec-choose
		// leg Enter uses (stat-validate + advance).
		return m.handleAnalyzeCommitSpec(pages.AnalyzeCommitSpecMsg{Value: msg.Path})
	case analyzeOutputPickTarget:
		// The §J output browse: a file pick names the output file itself;
		// the [s] folder pick names a directory, which
		// applyAnalyzeOutputPick resolves to a usable file path.
		return m.applyAnalyzeOutputPick(msg.Path)
	case app.SettingTxFile:
		return m.gateTxFilePick(msg.Path)
	}

	return m.commitSettingKey(target, msg.Path)
}

// gateTxFilePick applies a picked tx file unless that would silently bind
// specless entries to the engine default: an empty GetSpec is the
// "default in use" signal, and a file with specless entries then waits on
// a chained spec browse rooted at the file's dir. A count failure applies
// as today — no new failure mode; the load error surfaces on its own leg.
func (m *RootModel) gateTxFilePick(path string) (tea.Model, tea.Cmd) {
	explicit := false
	if cfg := m.configOrNil(); cfg != nil {
		explicit = cfg.GetSpec() != ""
	}
	if !explicit {
		if n, err := app.CountTransactionsWithoutSpec(path); err == nil && n > 0 {
			m.pendingTxFile = path
			start := analyzePickStart(path)

			return m.openFilePicker(OpenFilePickerMsg{
				Target: settingsSpecForFileTarget, Root: "/", RootLabel: start + string(filepath.Separator),
				Start: start, Exts: settingsPickExts(app.SettingSpec),
			})
		}
	}

	return m.commitSettingKey(app.SettingTxFile, path)
}

// applySpecForFilePick commits the chained spec pick: BOTH the spec and
// the pending tx file travel in one patch so the file is built against
// the spec chosen now (ApplySettings resolves spec before file).
func (m *RootModel) applySpecForFilePick(specPath string) (tea.Model, tea.Cmd) {
	file := m.pendingTxFile
	m.pendingTxFile = ""
	if file == "" {
		return m, nil // nothing waits beneath this browse
	}
	src := m.settingsSource()
	if src == nil {
		m.settingsNote = errNoAppSession

		return m, nil
	}
	for _, key := range []string{app.SettingSpec, app.SettingTxFile} {
		delete(m.settingsErrs, key)
	}
	m.settingsNote = ""
	m.settingsSavedLine = ""

	return m.commitSettingPatch(src, map[string]string{app.SettingSpec: specPath, app.SettingTxFile: file})
}

// txPickExts is the §B tx-file picker's extension filter (the tx file is JSON).
var txPickExts = settingsPickExts(app.SettingTxFile)

// pickTree resolves the picker's root triple: the hook's virtual tree when
// set, else Root "/" with the absolute start dir as label base, so every up
// leg can leave the start dir.
func (m *RootModel) pickTree(key, value string) (root, label, start string) {
	if m.filePickRootFn != nil {
		root, label := m.filePickRootFn(key, value)

		return root, label, "" // the hook owns the whole virtual tree
	}
	start = "."
	if value != "" {
		start = filepath.Dir(value)
	}
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}

	return "/", start + string(filepath.Separator), start
}

// handleTxPickFile resolves §B `f` into an OpenFilePickerMsg: browse from
// the current tx file's directory (rooted at "/"), committed through the same
// settings commit path §L uses.
func (m *RootModel) handleTxPickFile() (tea.Model, tea.Cmd) {
	if m.filePick != nil {
		return m, nil
	}
	value := ""
	if m.app != nil {
		if cfg := m.app.Config(); cfg != nil {
			value = cfg.GetFile()
		}
	}
	root, label, start := m.pickTree(app.SettingTxFile, value)

	// Mark the pick as from §B so the load result surfaces on the
	// transactions page; drop any prior load error.
	m.txFilePickFromB, m.txFileLoadErr = true, ""

	return m.openFilePicker(OpenFilePickerMsg{
		Target: app.SettingTxFile, Root: root, RootLabel: label, Start: start, Exts: txPickExts,
	})
}

// settingsPathKeys are the §L rows whose values are file paths; `f` opens
// the picker on them.
var settingsPathKeys = map[string]bool{
	app.SettingSpec: true, app.SettingTxFile: true, app.SettingTLSConfig: true, app.SettingDB: true,
}

// settingsPickExts maps a §L path key to the picker's extension filter.
func settingsPickExts(key string) []string {
	switch key {
	case app.SettingSpec, app.SettingTxFile, app.SettingTLSConfig:
		return []string{jsonExt}
	default:
		return nil
	}
}

// handleSettingsPickFile resolves a §L `f` into an OpenFilePickerMsg: it
// starts at the field value's directory (rooted at "/") and a selection
// commits through handleSettingsCommit.
func (m *RootModel) handleSettingsPickFile(msg pages.SettingsPickFileMsg) (tea.Model, tea.Cmd) {
	if m.Current().ID() != pages.SettingsPageID || m.filePick != nil {
		return m, nil
	}
	value := ""
	if m.settingsView != nil {
		for _, r := range m.settingsView.Rows {
			if r.Key == msg.Key {
				value = r.Value

				break
			}
		}
	}
	root, label, start := m.pickTree(msg.Key, value)

	return m.openFilePicker(OpenFilePickerMsg{Target: msg.Key, Root: root, RootLabel: label, Start: start, Exts: settingsPickExts(msg.Key)})
}

// pushToast appends a root-side toast (timestamped with the injectable
// now; the widget never reads the clock).
func (m *RootModel) pushToast(text string, kind widgets.ToastKind) {
	if m.toast == nil {
		m.toast = widgets.NewToast(m.themeOrNil())
	}
	m.toast.Push(text, kind, m.now())
}

// armToastTick keeps a prune tick in flight while toasts are visible;
// expiry is age-based, not generation-based.
func (m *RootModel) armToastTick() tea.Cmd {
	if m.toast == nil || m.toast.Len() == 0 || m.toastTickWait {
		return nil
	}
	m.toastTickWait = true
	f := m.toastTickf
	if f == nil {
		f = defaultToastTick
	}

	return f(toastTickInterval, func() tea.Msg { return toastTickMsg{} })
}

// applyToastTick prunes expired toasts by the injected now + root ttl.
func (m *RootModel) applyToastTick() (tea.Model, tea.Cmd) {
	m.toastTickWait = false
	if m.toast != nil {
		ttl := m.toastTTL
		if ttl == 0 {
			ttl = toastDefaultTTL
		}
		m.toast.Prune(m.now(), ttl)
	}

	return m, nil
}

// overlayToasts layers the right-aligned toast stack over the content's
// bottom-right; the widget's full-width left pad is trimmed so only the
// toast box overwrites the page.
func (m *RootModel) overlayToasts(content string) string {
	if m.toast == nil || content == "" {
		return content
	}
	w, h := frame.ContentSize(m.width, m.height)
	m.toast.SetSize(w)
	block := m.toast.View()
	if block == "" {
		return content
	}
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimLeft(l, " ")
	}

	return overlayBottomRight(content, strings.Join(lines, "\n"), w, h)
}
