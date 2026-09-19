// root_analyze.go owns the §J wizard state machine: root holds the truth and
// runs every engine leg off the UI thread as seq-tokened tea.Cmds — jumps,
// leaves, and aborts bump analyzeSeq so stale results are dropped. Forward
// transitions pass validation gates; w writes behind the §N3 overwrite
// confirm.
package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// analyzePickTarget routes the capture step's [f] pick back into the
// capture commit.
const analyzePickTarget = "analyze:capture"

// analyzeSpecPickTarget routes the spec step's [f] pick into the spec
// commit, keeping the .json browse out of the capture leg.
const analyzeSpecPickTarget = "analyze:spec"

// analyzeOutputPickTarget routes the run step's [o] pick to the output commit.
const analyzeOutputPickTarget = "analyze:output"

// The §J wizard's gate messages: one text per blocked-step explanation,
// shared by every leg that refuses.
const (
	analyzeNeedCapture = "capture file path is required"
	analyzeNoEngine    = "no analyze engine available"
	analyzeInFlight    = "operation in flight - wait for the current leg"
)

// analyzeBrowse opens the shared file picker: .pcap over the capture step
// ([f] or Enter on an empty list), .json over the spec step (a directory is
// not a legal spec, so no PickDirKey). Both root at "/" so the operator can
// climb above the start dir; picks flow through applyFilePicked.
func (m *RootModel) handleAnalyzeBrowse(spec bool) (tea.Model, tea.Cmd) {
	if m.filePick != nil {
		return m, nil
	}
	if spec {
		start := analyzePickStart(m.analyzeSpecPath)

		return m.openFilePicker(OpenFilePickerMsg{
			Target: analyzeSpecPickTarget, Root: "/", RootLabel: start + string(filepath.Separator),
			Start: start, Exts: []string{jsonExt},
		})
	}
	value := m.analyzeCapturePath
	if value == "" && len(m.analyzeRecents) > 0 {
		value = m.analyzeRecents[0]
	}
	start := analyzePickStart(value)

	return m.openFilePicker(OpenFilePickerMsg{
		Target: analyzePickTarget, Root: "/", RootLabel: start + string(filepath.Separator),
		Start: start, Exts: []string{".pcap"},
	})
}

// analyzePickStart resolves a §J browse's start directory: the picked
// path's directory, made absolute (the working directory when nothing
// is picked yet).
func analyzePickStart(value string) string {
	start := "."
	if value != "" {
		start = filepath.Dir(value)
	}
	if abs, err := filepath.Abs(start); err == nil {
		return abs
	}

	return start
}

// outPickDirKey is the output picker's extra "set folder" key: it commits
// the directory being browsed as the output location.
const outPickDirKey = "s"

// handleAnalyzeOutBrowse opens the shared picker for the [o] editor: rooted
// at "/", starting at the draft's directory, with no extension filter (every
// existing file is a legal write target); [s] commits the browsed folder.
func (m *RootModel) handleAnalyzeOutBrowse(draft string) (tea.Model, tea.Cmd) {
	if m.filePick != nil {
		return m, nil
	}
	value := strings.TrimSpace(draft)
	if value == "" {
		value = m.analyzeOutputDisplay()
	}
	start := "."
	if value != "" {
		start = filepath.Dir(value)
	}
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}

	return m.openFilePicker(OpenFilePickerMsg{
		Target: analyzeOutputPickTarget, Root: "/", RootLabel: start + string(filepath.Separator),
		Start: start, PickDirKey: outPickDirKey,
	})
}

// analyzeExistingFile resolves a typed path (abs first, then the raw path);
// ok is false when neither resolves to a readable file.
func analyzeExistingFile(path string) (string, bool) {
	if abs, err := filepath.Abs(path); err == nil {
		if fi, serr := os.Stat(abs); serr == nil && !fi.IsDir() {
			return abs, true
		}
	}
	if fi, serr := os.Stat(path); serr == nil && !fi.IsDir() {
		return path, true
	}

	return "", false
}

// analyzeSource is the §J engine façade leg; tests inject a fake. The
// wizard reads no config defaults from it: spec/header paths start empty
// and travel to the legs as chosen ("" = the engine default applies there).
type analyzeSource interface {
	StatPath(ctx context.Context, path string) error
	EnumerateFlows(ctx context.Context, pcapPath, headerType, specPath string) (*app.AnalyzeEnumeration, error)
	RunAnalyze(ctx context.Context, opts app.AnalyzeRunOptions) (*app.AnalyzeOutput, error)
	// WriteAnalyze takes the write-leg ctx: abort/leave cancel it so a
	// stale leg never starts a second SaveItems on a file an earlier one
	// is still rewriting.
	WriteAnalyze(ctx context.Context, out *app.AnalyzeOutput) error
}

// analyzeStat resolves the injectable overwrite-stat leg for `w` (nil = os.Stat).
func (m *RootModel) analyzeStat() func(string) (os.FileInfo, error) {
	if m.analyzeStatFn != nil {
		return m.analyzeStatFn
	}

	return os.Stat
}

// analyzeSource resolves the injectable leg (nil = the App façade; nil
// App = no leg, the wizard stays in its empty state).
func (m *RootModel) analyzeSource() analyzeSource {
	if m.analyzeSrc != nil {
		return m.analyzeSrc
	}
	if m.app == nil {
		return nil
	}

	return m.app
}

// Result messages from the tea.Cmd goroutines; seq marks the wizard
// generation (a stale seq is ignored).
type (
	analyzeSpecStatMsg struct {
		seq uint64
		err error
	}
	analyzeEnumLoadedMsg struct {
		seq  uint64
		enum *app.AnalyzeEnumeration
		err  error
	}
	analyzeRunLoadedMsg struct {
		seq uint64
		out *app.AnalyzeOutput
		err error
	}
	// analyzeWriteStatMsg is the §N3 overwrite stat verdict for `w`: the
	// confirm decision rides the shared widgets.ConfirmDialog.
	analyzeWriteStatMsg struct {
		seq    uint64
		out    *app.AnalyzeOutput
		path   string
		exists bool
	}
	analyzeWriteLoadedMsg struct {
		seq   uint64
		path  string
		count int
		err   error
	}
)

// handleAnalyzeNext is Enter on a non-text step: advance one step
// through the gates.
func (m *RootModel) handleAnalyzeNext() (tea.Model, tea.Cmd) {
	return m.handleAnalyzeStepDelta(pages.AnalyzeStepDeltaMsg{Delta: 1})
}

// handleAnalyzeStepDelta: backward jumps are free revisits, forward ones pass
// the per-step gates. Exception: a backward jump while a WRITE is in flight
// is refused — one write in flight, no queue; it could never be undone.
func (m *RootModel) handleAnalyzeStepDelta(msg pages.AnalyzeStepDeltaMsg) (tea.Model, tea.Cmd) {
	step := m.analyzeStep
	if msg.Delta < 0 {
		if m.analyzeWriteWait {
			m.analyzeNote = "write in flight - wait for it to finish"

			return m, nil
		}

		return m, m.setAnalyzeStep(max(step+msg.Delta, 0))
	}
	target := min(step+1, pages.StepCount-1)
	if target == step {
		return m, nil
	}

	switch step {
	case pages.StepCapture, pages.StepSpec:
		// Forward always (re)runs the step's own commit — the leg Enter
		// arms; a wait in flight is named, never silently refused.
		if m.analyzeEnumWait || m.analyzeSpecWait || m.analyzeRunWait || m.analyzeWriteWait {
			m.analyzeNote = analyzeInFlight

			return m, nil
		}
		if step == pages.StepCapture {
			return m.handleAnalyzeCommitCapture(pages.AnalyzeCommitCaptureMsg{Value: m.analyzeCapturePath})
		}

		return m.handleAnalyzeCommitSpec(pages.AnalyzeCommitSpecMsg{Value: m.analyzeSpecPath})
	}

	return m, m.setAnalyzeStep(target)
}

// setAnalyzeStep lands on step n: the seq bump cancels every in-flight leg
// and arriving at the run step arms flow enumeration. A FORWARD arrival at
// the capture or spec step lands with the file browser open on the
// previously chosen file — browsing is those steps' default surface.
func (m *RootModel) setAnalyzeStep(n int) tea.Cmd {
	if n == m.analyzeStep {
		return nil
	}
	forward := n > m.analyzeStep
	m.analyzeStep = n
	m.analyzeSeq++
	m.analyzeSpecWait, m.analyzeEnumWait, m.analyzeRunWait, m.analyzeWriteWait = false, false, false, false
	m.analyzeNote = ""
	m.analyzeStatus = pages.AnalyzeStatusIdle
	m.analyzeBrowserArmed = false
	if n == pages.StepRun {
		return m.armAnalyzeEnum()
	}
	if forward {
		m.autoOpenAnalyzeBrowser()
	}

	return nil
}

// autoOpenAnalyzeBrowser is the file steps' default surface: a forward
// arrival whose step already has a chosen file opens the shared picker
// through the [f] browse's own leg (targets and start dirs stay
// single-sourced), then PositionFile seats the list cursor on the
// previous pick itself — cursor only, the operator confirms with Enter.
// Nothing chosen yet (the first pass) leaves the inline candidate scan.
// The armed bool is fresh per setAnalyzeStep, so one arrival opens the
// browser exactly once; a later arrival re-arms and fires again.
func (m *RootModel) autoOpenAnalyzeBrowser() {
	var prev string
	var spec bool
	switch m.analyzeStep {
	case pages.StepCapture:
		prev = m.analyzeCapturePath
	case pages.StepSpec:
		spec, prev = true, m.analyzeSpecPath
	default:
		return
	}
	if m.analyzeBrowserArmed || m.filePick != nil || prev == "" {
		return
	}
	m.analyzeBrowserArmed = true
	m.handleAnalyzeBrowse(spec)
	if m.filePick != nil {
		m.filePick.PositionFile(prev)
	}
}

// handleAnalyzeCommitSpec commits the spec path ("" = the engine default)
// and validates it off the UI thread; a missing path becomes the inline
// field error.
func (m *RootModel) handleAnalyzeCommitSpec(msg pages.AnalyzeCommitSpecMsg) (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(msg.Value)
	m.analyzeSpecPath = value
	m.analyzeSpecError = ""
	m.analyzeRunStale = true

	if value == "" {
		return m, m.setAnalyzeStep(pages.StepHeader)
	}
	src := m.analyzeSource()
	if src == nil {
		m.analyzeSpecError = analyzeNoEngine

		return m, nil
	}
	m.analyzeSpecWait = true
	m.analyzeSeq++
	seq := m.analyzeSeq

	return m, func() tea.Msg {
		return analyzeSpecStatMsg{seq: seq, err: src.StatPath(context.Background(), value)}
	}
}

// applyAnalyzeSpecStat folds the spec validation result: ok advances, a
// typed error lands as the inline field error (never a crash, never a
// created file).
func (m *RootModel) applyAnalyzeSpecStat(msg analyzeSpecStatMsg) (tea.Model, tea.Cmd) {
	m.analyzeSpecWait = false
	if msg.seq != m.analyzeSeq || m.Current().ID() != pages.AnalyzePageID {
		return m, nil
	}
	if msg.err != nil {
		m.analyzeSpecError = analyzeErrorText(msg.err)

		return m, nil
	}

	return m, m.setAnalyzeStep(pages.StepHeader)
}

// handleAnalyzeCommitCapture validates the capture file exists inline (a
// missing path stays on the step), remembers it in recents, and advances; an
// empty commit with no candidates opens the file picker.
func (m *RootModel) handleAnalyzeCommitCapture(msg pages.AnalyzeCommitCaptureMsg) (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(msg.Value)
	m.analyzeCaptureError = ""
	m.analyzeNote = ""

	if value == "" {
		if len(m.analyzeCaptureItems()) == 0 {
			return m.handleAnalyzeBrowse(false)
		}
		m.analyzeCaptureError = analyzeNeedCapture

		return m, nil
	}
	resolved, ok := analyzeExistingFile(value)
	if !ok {
		m.analyzeCaptureError = "no such file: " + value
		m.debug.logf("analyze capture pick missing %s", value)

		return m, nil
	}
	m.analyzeCapturePath = resolved
	m.analyzeRecents = dedupePaths(append([]string{resolved}, m.analyzeRecents...))
	m.debug.logf("analyze capture pick %s", resolved)

	return m, m.setAnalyzeStep(pages.StepSpec)
}

// handleAnalyzeAbort is Esc: aborting over an in-flight leg asks §N3
// confirm first; otherwise the wizard resets and leaves.
func (m *RootModel) handleAnalyzeAbort() (tea.Model, tea.Cmd) {
	if m.analyzeSpecWait || m.analyzeEnumWait || m.analyzeRunWait || m.analyzeWriteWait {
		m.analyzeConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "abort analyze run?")

		return m, nil
	}

	return m.doAnalyzeAbort()
}

// applyAnalyzeConfirmed / applyAnalyzeCancelled drive the §N3 abort
// confirm (default No keeps the run going).
func (m *RootModel) applyAnalyzeConfirmed() (tea.Model, tea.Cmd) {
	m.analyzeConfirm = nil

	return m.doAnalyzeAbort()
}

func (m *RootModel) applyAnalyzeCancelled() (tea.Model, tea.Cmd) {
	m.analyzeConfirm = nil

	return m, nil
}

// doAnalyzeAbort bumps seq (in-flight results drop), cancels the write
// leg's ctx (a confirmed abort actually stops the SaveItems), resets the
// transient run state, and pops the page.
func (m *RootModel) doAnalyzeAbort() (tea.Model, tea.Cmd) {
	m.analyzeSeq++
	m.cancelAnalyzeWrite()
	m.analyzeOverwriteConfirm = nil
	m.analyzeSpecWait, m.analyzeEnumWait, m.analyzeRunWait, m.analyzeWriteWait = false, false, false, false
	m.analyzeStep = pages.StepCapture
	m.analyzeStatus = pages.AnalyzeStatusIdle
	m.analyzeNote, m.analyzeSpecError, m.analyzeCaptureError = "", "", ""
	m.analyzeFlows, m.analyzeSelected = nil, nil
	m.analyzeParsed, m.analyzeUnparsable, m.analyzeFlowFilter = 0, 0, ""
	m.analyzeOutput, m.analyzePreview, m.analyzeElapsed = nil, "", ""
	m.analyzeItemRows, m.analyzeExcluded = nil, nil
	m.analyzeUnparsableRows = nil
	m.analyzeWriteLine, m.analyzeWriteOK = "", false
	m.analyzeRunStale = true
	m.popPage()

	return m, nil
}

// leaveAnalyze bumps seq (leave-side cancel), cancels the write leg's ctx,
// and parks the wizard at step 1 so re-entry lands on the capture step.
func (m *RootModel) leaveAnalyze() {
	if m.Current().ID() == pages.AnalyzePageID {
		m.analyzeSeq++
		m.cancelAnalyzeWrite()
		m.analyzeOverwriteConfirm = nil
		m.analyzeSpecWait, m.analyzeEnumWait, m.analyzeRunWait, m.analyzeWriteWait = false, false, false, false
		m.analyzeStatus = pages.AnalyzeStatusIdle
		m.analyzeStep = pages.StepCapture
	}
}

// analyzeEngineMode maps the goal radio to the engine mode token.
func analyzeEngineMode(goal string) string {
	switch goal {
	case pages.AnalyzeGoalMockRoutes:
		return app.AnalyzeModeRoutes
	case pages.AnalyzeGoalScenario:
		return app.AnalyzeModeScenario
	default:
		return app.AnalyzeModeTx
	}
}

// analyzeErrorText renders a façade error as inline field/Note text.
func analyzeErrorText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
