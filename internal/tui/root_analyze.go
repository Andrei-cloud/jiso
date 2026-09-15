// root_analyze.go owns the §J wizard state machine (SCR-510). The page
// is presentation-only: root holds the truth — current step, per-step
// picks, enumerated flows, run status, and the results preview — and
// runs every engine leg (spec stat, flow enumeration, analyze run,
// write) OFF the UI thread as a tea.Cmd reporting back as a seq-tokened
// msg (the serverTickSeq lifecycle): step jumps, page leaves, and
// aborts bump analyzeSeq so in-flight results turn stale and cancel.
//
// The wizard is capture ▸ spec ▸ header ▸ run (the send wizard's
// shape). Transitions are key-driven (Enter/PgUp/PgDn/Tab/Esc-back)
// with validation gates: the capture commit checks the file exists
// inline (the wizardChooseFile idiom — a missing path never leaves the
// step), the spec commit validates through the stat leg, and entering
// the run step arms flow enumeration. Enter on the run step starts the
// analysis with the folded inline options (goal, security, and the
// substring flow filter mapped to the run's port set); an empty filter
// runs every enumerated dst flow and a filter that matches none is a
// note, never a fabricated selection (E1-FIX lesson). w writes via the
// façade behind the §N3 overwrite confirm; Esc on step 1 aborts (§N3
// confirm over an in-flight leg). analyzeSrc overrides the app legs
// for tests (fake façade).
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

// analyzePickTarget routes the capture step's [f] picker selection
// back into the capture commit (the wizardPick*Targets pattern).
const analyzePickTarget = "analyze:capture"

// analyzeSpecPickTarget routes the spec step's [f] picker selection
// back into the spec commit (UAT round 9 F-9d: every file step opens
// the picker; the target keeps the .json browse out of the .pcap
// capture leg, the same split the wizardPick*Targets pair makes).
const analyzeSpecPickTarget = "analyze:spec"

// analyzeOutputPickTarget routes the run step's [o] picker selection
// to the output commit (UAT round 8 finding 6: the output path gets the
// same folder/file browse the capture step always had).
const analyzeOutputPickTarget = "analyze:output"

// The §J wizard's gate messages: what the operator reads when a leg cannot start.
// Each was spelled at three or four sites across the legs, so rewording one meant
// finding every copy -- and a half-updated pair shows the operator two different
// explanations for the same blocked step.
const (
	analyzeNeedCapture = "capture file path is required"
	analyzeNoEngine    = "no analyze engine available"
	analyzeInFlight    = "operation in flight - wait for the current leg"
)

// analyzeBrowse opens the shared root file picker over the capture
// step ([f] or Enter on an empty candidate list): .pcap files only,
// starting at the last used capture's directory (or the working
// directory). The page stays underneath; Esc returns to it and a
// selection flows through applyFilePicked.
//
// spec selects the step (UAT round 9 F-9d): the spec step's browse is
// the same overlay over .json files, starting at the current spec's
// directory. No PickDirKey there — a directory is not a legal spec,
// only the engine default ("") is.
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

// outPickDirKey is the output picker's extra "set folder" key (the
// widget binds it only for this owner): it commits the directory being
// browsed so the operator can choose the output LOCATION without
// having to find a same-named file to overwrite.
const outPickDirKey = "s"

// handleAnalyzeOutBrowse opens the shared root file picker over the run
// step's [o] output editor (UAT round 8 finding 6): mirroring the
// capture browse it roots at "/" with the start directory spelled in
// the virtual label, beginning at the draft path's directory (else the
// effective output's, else the working directory). Unlike the capture
// picker no extension filter applies — every existing file is a legal
// write target (the §N3 overwrite confirm still gates the actual w) —
// and the extra [s] key commits the browsed folder itself, which
// applyAnalyzeOutputPick turns into a usable file path.
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

// analyzeExistingFile resolves a typed path the way the send wizard's
// file commit does (abs first, then the raw path); ok is false when
// neither resolves to a readable file.
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

// analyzeSource is the §J engine façade leg: the App methods match it
// structurally, and tests inject a fake (no real pcap/engine needed
// above). The wizard reads no config defaults from it: the spec/header
// paths start empty and travel to the legs as chosen ("" = the engine
// default applies there).
type analyzeSource interface {
	StatPath(ctx context.Context, path string) error
	EnumerateFlows(ctx context.Context, pcapPath, headerType, specPath string) (*app.AnalyzeEnumeration, error)
	RunAnalyze(ctx context.Context, opts app.AnalyzeRunOptions) (*app.AnalyzeOutput, error)
	// WriteAnalyze takes the write-leg ctx (E5-FIX/B2): abort/leave
	// cancel it so a stale leg never starts a second config.SaveItems
	// on a file the previous leg may still be rewriting.
	WriteAnalyze(ctx context.Context, out *app.AnalyzeOutput) error
}

// analyzeStat resolves the injectable overwrite-stat leg for `w`
// (nil = os.Stat; the §K ctfStatFn idiom).
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
	// analyzeWriteStatMsg is the §N3 overwrite stat verdict for `w`
	// (E5-FIX/B2: the same-named items in the user's real config are
	// never silently replaced; the confirm decision rides the shared
	// widgets.ConfirmDialog).
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

// handleAnalyzeStepDelta is PgUp/PgDn/Tab: backward jumps are free
// revisits; forward transitions pass the per-step gates. The one
// exception (E5-FIX/B2): a backward jump while a WRITE is in flight is
// refused — clearing analyzeWriteWait mid-leg would let PgUp→PgDn→w arm
// a second concurrent config.SaveItems on the same file (read-modify-
// write + truncate), and no later result could undo it.
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
		// Forward always (re)runs the step's own commit — the same leg
		// Enter arms. A silent refusal would make Enter look dead; name
		// the wait (E5-FIX/B2).
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

// setAnalyzeStep lands on step n: the seq bump cancels every in-flight
// leg (step-jump cancel), and arriving at the run step arms flow
// enumeration (the run itself starts on Enter, with the folded inline
// options).
func (m *RootModel) setAnalyzeStep(n int) tea.Cmd {
	if n == m.analyzeStep {
		return nil
	}
	m.analyzeStep = n
	m.analyzeSeq++
	m.analyzeSpecWait, m.analyzeEnumWait, m.analyzeRunWait, m.analyzeWriteWait = false, false, false, false
	m.analyzeNote = ""
	m.analyzeStatus = pages.AnalyzeStatusIdle
	if n == pages.StepRun {
		return m.armAnalyzeEnum()
	}

	return nil
}

// handleAnalyzeCommitSpec is Enter on ②: commit the spec path ("" =
// the engine default spec) and validate it off the UI thread (PAR-311:
// a missing path becomes the inline field error text).
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

// applyAnalyzeSpecStat folds the ② validation result: ok advances, a
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

// handleAnalyzeCommitCapture is Enter on the capture step (or a picker
// selection through applyFilePicked): validate the file exists inline
// (the wizardChooseFile idiom — a missing path stays on the step with
// "no such file: …"), remember it in the session recents, and advance
// to the spec step. An empty commit with no candidates opens the
// shared file picker (the updateWizardKey empty-step escape); an empty
// commit with candidates is a plain inline error.
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

// doAnalyzeAbort bumps the seq (in-flight legs turn stale and their
// results are dropped), cancels the write leg's ctx (E5-FIX/B2: a
// confirmed abort must actually stop the SaveItems, not just ignore
// its result), resets the transient run state, and pops the page when
// it was pushed.
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

// leaveAnalyze bumps the seq when navigation replaces/pushes away from
// the §J page (the leave-side cancel of the SCR-507/509 pattern),
// cancels the write leg's ctx (E5-FIX/B2), and parks the wizard back at
// step 1 so hotkey 7 / the palette jump always land on the capture
// step.
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

// analyzeErrorText renders a façade error as inline field/Note text:
// the typed config-class error already names its path (PAR-311 class
// surfaced as text, never a crash).
func analyzeErrorText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
