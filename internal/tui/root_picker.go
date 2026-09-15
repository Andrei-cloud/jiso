// root_picker.go is the TUI-406b root plumbing: the shared
// widgets.FilePicker and widgets.Toast wired into the router. The
// picker is a root-owned modal overlay (palette/dialog mechanism):
// OpenFilePickerMsg (or a page message that resolves to one) opens it,
// it owns the keyboard wholesale while open (Esc cancels through the
// widget's own FilePickerCanceledMsg), and a FilePickedMsg commits the
// chosen path through the target's commit seam — §L fields go through
// handleSettingsCommit, so validation/save semantics are unchanged.
// The toast stack is root-side truth: owners call pushToast with text
// + kind, the widget timestamps with the injectable now, and a root
// tick (armToastTick, the serverTickf override pattern) prunes by age
// with the ttl the root passes — the widget never reads the clock.
// View composes the active toasts over the content's bottom-right.
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
	// toastTickInterval is the prune poll; toastDefaultTTL is the age
	// a toast survives (owners may override m.toastTTL) — 3s per the
	// wireframe (E5-FIX/M6; NewRootModel pins m.toastTTL from it).
	toastTickInterval = 500 * time.Millisecond
	toastDefaultTTL   = 3 * time.Second
)

// OpenFilePickerMsg opens the picker overlay; Target names the commit
// seam a selection flows through ("settings:<key>" or a bare §L key).
// PickDirKey (UAT round 8 finding 6) binds the widget's extra
// "commit the browsed folder" key for write-target owners; empty binds
// nothing.
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

// defaultToastTick is the production prune scheduler (the
// defaultServerTick pattern).
func defaultToastTick(d time.Duration, mk func() tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return mk() })
}

// openFilePicker opens the modal browser sized to the content area
// (header + rows + footer stay inside one screen).
// jsonExt is the extension the file pickers offer and the scenario export appends.
// It is spelled in both directions -- filter a directory by it, and add it when a
// typed name forgot it -- so the two ends have to agree or a picked file comes back
// as "spec.json.json".
const jsonExt = ".json"

func (m *RootModel) openFilePicker(msg OpenFilePickerMsg) (tea.Model, tea.Cmd) {
	w, h := frame.ContentSize(m.width, m.height)
	// The View wraps the picker in boxed() (modalBox at modalBoxWidth):
	// lipgloss Width pads but never truncates, so the picker must be
	// sized to the box's INNER width or every spliced line overruns the
	// terminal and the frame corrupts.
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

// applyFilePicked routes a selection to the target's commit seam;
// targets reuse commitSettingKey — the exact §L commit path — so
// validation stays in one place whether the picker was opened from a
// §L field or the §B tx-file key.
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
	case analyzePickTarget:
		// The §J capture step: the pick commits through the same
		// capture-choose leg Enter uses (validate + advance).
		return m.handleAnalyzeCommitCapture(pages.AnalyzeCommitCaptureMsg{Value: msg.Path})
	case analyzeSpecPickTarget:
		// The §J spec step (UAT round 9 F-9d): the pick commits through
		// the same spec-choose leg Enter uses (stat-validate + advance);
		// a failure lands as the inline SpecError, never a crash.
		return m.handleAnalyzeCommitSpec(pages.AnalyzeCommitSpecMsg{Value: msg.Path})
	case analyzeOutputPickTarget:
		// The §J run-step output browse: a file pick names the output
		// file itself (an existing target still passes the §N3 overwrite
		// confirm at w); the [s] folder pick names a directory, which
		// applyAnalyzeOutputPick resolves to a usable file path.
		return m.applyAnalyzeOutputPick(msg.Path)
	}

	return m.commitSettingKey(target, msg.Path)
}

// txPickExts is the §B tx-file picker's extension filter (the tx file
// is JSON; same filter as the §L tx-file field).
var txPickExts = settingsPickExts(app.SettingTxFile)

// pickTree resolves a file picker's root triple for an owner: the
// filePickRootFn hook's virtual root+label (tests browse a t.TempDir
// fixture behind a relative label, no absolute temp path in View), or
// the §J/§G/§H production pattern — Root "/" with the absolute start
// dir as both Start and label base (UAT round 9 F-9a: a Root of "."
// with Start unset pinned p.dir to p.root, so no up leg could ever
// leave the start dir). relLabel bypasses the label when root is "/",
// so the header shows the absolute path — as §J/§G/§H already do.
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

// handleTxPickFile resolves §B `f` into an OpenFilePickerMsg (E5-FIX/
// M6: the message pair existed but had no emitter — the §B empty state
// and the §M registry advertise `f` (UAT round 8 D3; was `t`), and the
// picker only ever opened
// from §L). Browsing starts at the current tx file's directory (or the
// filePickRootFn override — tests browse a t.TempDir fixture), and a
// selection commits the tx-file path through the same settings commit
// path §L uses. Like the §J/§G/§H owners the production picker roots
// at "/" so every up leg can leave the start dir (UAT round 9 F-9a).
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

	// This pick is from §B: mark it so the load result surfaces on the
	// transactions page (UAT round 7), and drop any prior load error.
	m.txFilePickFromB, m.txFileLoadErr = true, ""

	return m.openFilePicker(OpenFilePickerMsg{
		Target: app.SettingTxFile, Root: root, RootLabel: label, Start: start, Exts: txPickExts,
	})
}

// settingsPathKeys are the §L rows whose values are file paths; `f`
// opens the picker on them (SettingOutput stays the text/json enum —
// it is not a path field).
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

// handleSettingsPickFile resolves a §L `f` into an OpenFilePickerMsg:
// the picker starts at the field's current value's directory (or the
// filePickRootFn override — tests browse a t.TempDir fixture), and a
// selection commits through handleSettingsCommit. Like handleTxPickFile
// the production picker roots at "/" via pickTree so it climbs above
// the start dir (UAT round 9 F-9a).
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

// armToastTick keeps a prune tick in flight while toasts are visible
// (the serverTickf/seq pattern without the seq: expiry is age-based,
// not generation-based).
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
// bottom-right corner (wireframe: toasts bottom-right, 3s). The toast
// widget left-pads its lines to full width; the padding is trimmed so
// only the toast box itself overwrites the page.
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
