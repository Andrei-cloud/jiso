// analyze_state.go holds the §J state contract and the page→router
// messages. The state machine lives root-side (root_analyze*.go): the page
// renders an AnalyzeState snapshot and yields key-driven messages, and
// never imports internal/app or reads the clock — display strings are
// root-derived.
package pages

import (
	"strconv"
	"strings"
)

// AnalyzePageID is the router id of the §J analyze page (hotkey 7, label "analyze").
const AnalyzePageID = "analyze"

// Wizard step indices: capture ▸ spec ▸ header ▸ run, plus the routes
// goal's matching wizard (StepMatching), which the rail interleaves
// between header and run — see StepsForGoal. The small choices (goal,
// security, flows) fold into inline rows on the run step.
const (
	StepCapture  = iota // 1 capture file
	StepSpec            // 2 message spec
	StepHeader          // 3 length header
	StepRun             // 4 run (summary + folded options + results)
	StepMatching        // routes goal only: the matching-fields wizard
	StepCount    = 5
)

// Run-status tokens (root-owned; the page renders them, never times them).
const (
	AnalyzeStatusIdle    = "idle"
	AnalyzeStatusRunning = "running"
	AnalyzeStatusDone    = "done"
	AnalyzeStatusError   = "error"
)

// Goal-radio tokens (the run step's inline goal row).
const (
	AnalyzeGoalTransactions = "transactions"
	AnalyzeGoalMockRoutes   = "mock_routes"
	AnalyzeGoalScenario     = "scenario"
)

// StepNames are the wizard rail's labels, indexed by step.
var StepNames = []string{"capture", "spec", "header", "run", "matching"}

// StepsForGoal is the wizard's rail order for a goal: the mock-routes goal
// interleaves the matching wizard between header and run; the transactions
// and scenario goals run the classic four steps. Navigation walks
// positions in this list, never raw step indices: the matching step exists
// only inside the routes rail.
func StepsForGoal(goal string) []int {
	if goal == AnalyzeGoalMockRoutes {
		return []int{StepCapture, StepSpec, StepHeader, StepMatching, StepRun}
	}

	return []int{StepCapture, StepSpec, StepHeader, StepRun}
}

// StepNext/StepPrev return the step at position ±1 in the goal's rail,
// clamped to the ends (a clamp, not a stop: the operator never leaves the
// wizard by walking off the rail).
func StepNext(goal string, step int) int {
	return stepAtPosition(goal, StepPosition(goal, step)+1)
}

func StepPrev(goal string, step int) int {
	return stepAtPosition(goal, StepPosition(goal, step)-1)
}

// StepPosition reports a step's position in the goal's rail (rail steps
// only); a step outside the rail resolves to the rail's end (run).
func StepPosition(goal string, step int) int {
	steps := StepsForGoal(goal)
	for i, s := range steps {
		if s == step {
			return i
		}
	}
	// A step outside the goal's rail (the goal radio just changed under
	// the operator) resolves to run: the rail always contains run, and
	// landing there re-runs the enum arm the results screen needs.
	return len(steps) - 1
}

func stepAtPosition(goal string, pos int) int {
	steps := StepsForGoal(goal)
	if pos < 0 {
		pos = 0
	}
	if pos >= len(steps) {
		pos = len(steps) - 1
	}

	return steps[pos]
}

// AnalyzeRadio is one radio option; Key is its direct-select key.
type AnalyzeRadio struct {
	Key      string
	Label    string
	Selected bool
}

// AnalyzeHeaderItem is one length-header list row.
type AnalyzeHeaderItem struct {
	Header   string
	Selected bool
}

// AnalyzeFlowRow is one §J flows-table row (root-derived display data).
// Only dst rows are analysis units (Selectable); src rows display the
// response half. Selected is the PENDING inclusion in the next run's set.
type AnalyzeFlowRow struct {
	Port       int
	PeerPort   int    // the other end of the conversation(origin clarity)
	Direction  string // "dst" | "src"
	Msgs       int
	MTIs       string // "0200(180) 0800(25)"
	Signon     bool
	Selectable bool
	Selected   bool
}

// FlowMatchText is the lowercase text the flow filter matches (direction,
// port, MTI histogram). The display filter and the root-side port mapping
// share it, so what the operator sees is what runs.
func FlowMatchText(f AnalyzeFlowRow) string {
	return strings.ToLower(f.Direction + " :" + strconv.Itoa(f.Port) + " " + f.MTIs)
}

// FlowMatchesFilter reports whether a flow row passes the substring filter ("" = all).
func FlowMatchesFilter(f AnalyzeFlowRow, filter string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}

	return strings.Contains(FlowMatchText(f), filter)
}

// AnalyzeState is the immutable §J snapshot root pushes into the page:
// candidate lists, inline field errors, the step Note, the run Preview and
// WriteLine toast. Elapsed and FlowFilter are root-derived (no clock here).
type AnalyzeState struct {
	Step         int
	Status       string
	Goal         string
	Goals        []AnalyzeRadio
	SpecPath     string
	SpecError    string
	SpecItems    []WizardItem
	Header       string
	Headers      []AnalyzeHeaderItem
	CapturePath  string
	CaptureError string
	CaptureItems []WizardItem
	Flows        []AnalyzeFlowRow
	FlowFilter   string
	Parsed       int
	Unparsable   int
	MaskRaw      bool
	Masking      []AnalyzeRadio
	Elapsed      string
	Preview      string
	WriteLine    string
	WriteOK      bool
	// FileWritten marks the last successful write of the run's items to
	// disk (WriteOK only says [w] is armed). The post-write "use it now"
	// keys read this: they must never promise a file that was not written.
	FileWritten bool
	Note        string

	// Items is the generated-item picker's roster. ItemsID bumps per run
	// attach: a fresh run re-opens the overlay, the same roster keeps the
	// operator's local toggles.
	Items   []AnalyzeItemRow
	ItemsID int

	// UnparsableRows is the reviewer roster behind "[u] review": one row
	// per framed-but-unpackable message. UnparsableID bumps per enumeration;
	// unlike the item picker it does NOT auto-open.
	UnparsableRows []AnalyzeUnparsableRow
	UnparsableID   int

	// OutputPath is the effective output file the generated items will
	// land in; the [o] editor state is page-owned.
	OutputPath string

	// The matching wizard (routes goal): condition rows, the group-by
	// suggestions from the scan, the folded live line ("~42 pairs match ·
	// 2 route(s)" or "scanning..."), the inline warning, and the scan
	// wait flag. Root derives all of it from the scan cache; the page
	// owns only the cursors and the editor draft.
	Conds         []AnalyzeCond
	Variances     []AnalyzeGroupOption
	MatchLine     string
	MatchWarn     string
	MatchScanning bool
}

// AnalyzeCond is one matching-wizard condition row, root-derived for
// display: Side is the readable half ("req"/"resp"), When is the ladder
// token (equals/exists/prefix/oneof/regex/notin), Value carries the scalar
// (equals/exists/prefix/regex) and ValuesText the one-of/not-in list joined
// for display and re-editing. Field is the typed field path, "" while the
// row is still being composed.
type AnalyzeCond struct {
	Side       string
	Field      string
	When       string
	Value      string
	ValuesText string
}

// AnalyzeGroupOption is one field whose captured values varied — the
// group-by pane's row. Vary is the root-derived display count ("2 distinct
// values") and On marks the field chosen into the grouping.
type AnalyzeGroupOption struct {
	Field string
	Side  string // analyzer sides: "req" | "resp"
	Vary  string
	On    bool
}

// The matching-wizard messages: the conds keymap ([a]dd, [d]elete, [space]
// cycle WHEN, [s] flip side, editor commits) and the group-by pane's
// toggle. Root owns the condition truth; the page only routes verbs.
type (
	// AnalyzeCondAddMsg is [a] on the matching step: append a condition row.
	AnalyzeCondAddMsg struct{}
	// AnalyzeCondDeleteMsg is [d]: drop the row at Index.
	AnalyzeCondDeleteMsg struct{ Index int }
	// AnalyzeCondWhenMsg is [space]: cycle the row's condition along the
	// ladder (equals→exists→prefix→one-of→regex→not-in→equals).
	AnalyzeCondWhenMsg struct{ Index int }
	// AnalyzeCondSideMsg is [s]: flip the row's req/resp side.
	AnalyzeCondSideMsg struct{ Index int }
	// AnalyzeCondSetFieldMsg commits a typed FIELD path from the editor
	// (opened on a row whose field is still empty).
	AnalyzeCondSetFieldMsg struct {
		Index int
		Value string
	}
	// AnalyzeCondSetValueMsg commits a typed VALUE from the editor; one-of
	// and not-in arrive as comma-separated text and root parses the list.
	AnalyzeCondSetValueMsg struct {
		Index int
		Value string
	}
	// AnalyzeGroupToggleMsg is [space] in the group-by pane: flip the field
	// into or out of the grouping. Field+Side identify the row (a field id
	// can vary on both sides).
	AnalyzeGroupToggleMsg struct {
		Field string
		Side  string
	}

	// AnalyzeUseTxFileMsg is [l] after a scenario write: load the extract
	// as the session's transactions file through the tx-file gate.
	AnalyzeUseTxFileMsg struct{}

	// AnalyzeUseServerMsg is [g] after a scenario write: open the §G
	// server start form with the extract pre-filled as the routes file.
	AnalyzeUseServerMsg struct{}
)

// AnalyzeUnparsableRow is one failure sample: offset and length in the
// carved stream, the unpack reason, the raw head bytes, the stop byte
// (FailedAt, -1 when unknown), and the fields that unpacked before it.
type AnalyzeUnparsableRow struct {
	Offset   string
	Length   string
	Reason   string
	Head     []byte
	FailedAt int
	Fields   []UnparsableField
}

// UnparsableField is one field that parsed before the failure, in describe form.
type UnparsableField struct {
	ID    string
	Name  string
	Value string
}

// AnalyzeOutCommitMsg carries the [o] editor's output path; root validates
// it and marks the run stale so preview and write target agree.
type AnalyzeOutCommitMsg struct{ Path string }

// AnalyzeNextMsg is Enter on the header step: advance one step (root
// owns the validation gates).
type AnalyzeNextMsg struct{}

// AnalyzeStepDeltaMsg is a ±1 step jump: forward transitions pass root's
// gates, backward ones are free.
type AnalyzeStepDeltaMsg struct{ Delta int }

type (
	// AnalyzeCommitSpecMsg is Enter on the spec step with the chosen or
	// typed path; a missing file becomes the inline field error, never a
	// file created on the operator's behalf.
	AnalyzeCommitSpecMsg struct{ Value string }
	// AnalyzeCommitCaptureMsg is Enter on the capture step with the chosen
	// .pcap path; committing with nothing to choose opens the file picker.
	AnalyzeCommitCaptureMsg struct{ Value string }
)

// AnalyzeBrowseMsg is [f] on the capture or spec step. IsSpec is the step
// discriminator — capture (.pcap) vs spec (.json): without it every browse
// would re-pick the capture.
type AnalyzeBrowseMsg struct{ IsSpec bool }

// AnalyzeOutBrowseMsg is [f] in the freshly opened [o] editor: the editor
// closes and the keyboard goes to the shared picker; Draft carries the
// seeded path so the browse starts there. A pick commits root-side, an Esc
// leaves the effective path untouched.
type AnalyzeOutBrowseMsg struct{ Draft string }

type (
	// AnalyzeChooseGoalMsg selects what the analyze run should produce (t/r/s).
	AnalyzeChooseGoalMsg struct{ Goal string }
	// AnalyzeChooseHeaderMsg is the header step's pick: the length header the
	// framed messages carry.
	AnalyzeChooseHeaderMsg struct{ Header string }
	// AnalyzeChooseMaskMsg is the [m] toggle for sensitive-field masking.
	AnalyzeChooseMaskMsg struct{ Raw bool }
)

// AnalyzeRunMsg is Enter on the run step: start the analysis with the folded
// inline options. Filter is the flow filter ("" = every enumerated dst flow);
// the run's port set is the filter-visible rows root marks Selected.
type AnalyzeRunMsg struct{ Filter string }

// AnalyzeFlowToggleMsg is space on the run step's flow cursor: root toggles
// that (port, direction) in the pending run set.
type AnalyzeFlowToggleMsg struct {
	Port int
	Dir  string
}

// AnalyzeFlowToggleAllMsg is a on the run step: root includes every
// enumerated direction when they are not all included, else clears them all.
type AnalyzeFlowToggleAllMsg struct{}

// AnalyzeWriteMsg is w on the run step: persist the SELECTED generated items.
type AnalyzeWriteMsg struct{}

// AnalyzeItemRow is one generated item in the picker: Key is the identity root
// tracks the deselection by, Preview the item as it lands in the file,
// Included its membership in the pending write set.
type AnalyzeItemRow struct {
	Key      string
	Name     string
	Kind     string
	Group    string   // shared by a transaction and its dataset, so they toggle together
	RC       string   // mock_route rows: the response code this route answers with ("" otherwise)
	Links    []string // keys this row belongs with: toggling it ON selects them too (the complete scenario)
	Included bool
	Preview  string
}

// AnalyzeItemsApplyMsg is Enter in the item picker: root stores the
// deselection set and the next w writes exactly the selected items.
type AnalyzeItemsApplyMsg struct{ Excluded []string }

// AnalyzeAbortMsg is Esc on the first step: root cancels in-flight legs
// (seq token) and leaves; a run in flight asks §N3 confirm first.
type AnalyzeAbortMsg struct{}
