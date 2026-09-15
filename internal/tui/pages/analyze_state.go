// analyze_state.go holds the §J state contract (the 4-step analyze
// wizard) and the page→router messages. The wizard state machine lives
// root-side (root_analyze*.go): the page renders an AnalyzeState
// snapshot — current step, per-step candidate lists, enumerated flows,
// run status, and the results preview — and yields key-driven messages;
// every async leg (enumeration, run, write, spec stat) runs root-side
// as a tea.Cmd with the seq-token lifecycle. The page never imports
// internal/app and never reads the clock (the SCR-501 data-flow
// contract): Elapsed and every display string are root-derived.
//
// The 4 steps mirror the send wizard's shape: three pick steps
// (capture ▸ spec ▸ header) and one RUN step that folds the small
// choices (goal, security, flow filter) into inline rows instead of
// spending a step on each.
package pages

import (
	"strconv"
	"strings"
)

// AnalyzePageID is the router id of the §J analyze page:
// hotkey 7, footer label "analyze".
const AnalyzePageID = "analyze"

// Wizard step indices: capture ▸ spec ▸ header ▸ run. The old 7-step
// flow (goal/spec/header/capture/flows/security/output as separate
// steps) folded goal, security, and the flow selection into compact
// inline rows on the run step.
const (
	StepCapture = iota // 1 capture file
	StepSpec           // 2 message spec
	StepHeader         // 3 length header
	StepRun            // 4 run (summary + folded options + results)
	StepCount   = 4
)

// Run-status tokens (root-owned; the page renders them, never times
// them).
const (
	AnalyzeStatusIdle    = "idle"
	AnalyzeStatusRunning = "running"
	AnalyzeStatusDone    = "done"
	AnalyzeStatusError   = "error"
)

// Goal-radio tokens (the run step's inline goal row: transactions /
// mock routes / scenario flow).
const (
	AnalyzeGoalTransactions = "transactions"
	AnalyzeGoalMockRoutes   = "mock_routes"
	AnalyzeGoalScenario     = "scenario"
)

// StepNames are the rail labels in step order (the wizard rail reads
// "1 capture ▸ 2 spec ▸ 3 header ▸ 4 run").
var StepNames = []string{"capture", "spec", "header", "run"}

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

// AnalyzeFlowRow is one §J flows-table row (root-derived display data):
// the direction arrow, the server port, the message count, the MTI
// histogram text, and the signon marker. Only dst rows are analysis
// units (Selectable); src rows display the response half. Selected is
// the PENDING inclusion: the row is part of the next run's port set
// (space toggles it, a runs all/none; the run starts with the included
// rows the flow filter shows).
type AnalyzeFlowRow struct {
	Port       int
	PeerPort   int    // the other end of the conversation (UAT round 7: origin clarity)
	Direction  string // "dst" | "src"
	Msgs       int
	MTIs       string // "0200(180) 0800(25)"
	Signon     bool
	Selectable bool
	Selected   bool
}

// FlowMatchText is the lowercase text the run step's flow filter is
// matched against: direction, port, and MTI histogram. The page filters
// rows for display and the root maps the same filter to the run's port
// set through this one matcher, so what the user sees is what runs.
func FlowMatchText(f AnalyzeFlowRow) string {
	return strings.ToLower(f.Direction + " :" + strconv.Itoa(f.Port) + " " + f.MTIs)
}

// FlowMatchesFilter reports whether a flow row passes the run step's
// substring flow filter ("" = all flows).
func FlowMatchesFilter(f AnalyzeFlowRow, filter string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}

	return strings.Contains(FlowMatchText(f), filter)
}

// AnalyzeState is the immutable §J snapshot root pushes into the page.
// CaptureItems/SpecItems are the wizard candidate lists (recents and
// directory files, current tagged); CaptureError / SpecError are the
// inline field errors (PAR-311 class surfaced as text: path-not-found
// etc.), Note the step-level error, Preview the run-step results block,
// and WriteLine the toast-style write result. Elapsed and FlowFilter are
// root-derived (no clock here).
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
	Note         string

	// Items is the generated-item picker's roster (UAT round 6: after
	// a run the operator chooses WHICH generated transaction types land
	// in the file; the picker overlay replaces the old dry-run preview
	// text). ItemsID bumps per run attach: a fresh run re-arms (opens)
	// the overlay, the same roster keeps the operator's local toggles.
	Items   []AnalyzeItemRow
	ItemsID int

	// UnparsableRows is the reviewer roster behind the run step's
	// "N unparsable · [u] review" affordance (UAT round 6): each row is
	// one framed-but-unpackable message — its stream offset, byte
	// length, the unpack reason, and the head as prebuilt hexdump
	// lines. UnparsableID bumps per enumeration so the viewer re-arms a
	// fresh cursor over a new capture; unlike the item picker it does
	// NOT auto-open (the operator opens it with [u]).
	UnparsableRows []AnalyzeUnparsableRow
	UnparsableID   int

	// OutputPath is the effective output file the generated items will
	// land in (UAT round 5: the destination used to be an invisible
	// engine default). The [o] editor state is page-owned.
	OutputPath string
}

// AnalyzeUnparsableRow is one failure sample for the §J reviewer: the
// byte offset and length in the carved stream, the unpack reason, the
// raw head bytes (rendered as a hexdump with the unparsed region marked),
// the message-relative byte where parsing stopped (FailedAt, -1 when
// unknown), and the fields that unpacked before the failure (describe
// form). UAT round 7: the tester sees WHAT parsed and exactly WHERE the
// bytes went unparsed, not just a reason string.
type AnalyzeUnparsableRow struct {
	Offset   string
	Length   string
	Reason   string
	Head     []byte
	FailedAt int
	Fields   []UnparsableField
}

// UnparsableField is one field that parsed before the failure, in the
// describe projection (ID · Name · Value) the send review uses.
type UnparsableField struct {
	ID    string
	Name  string
	Value string
}

// AnalyzeOutCommitMsg carries the output path typed into the [o]
// editor (root validates it and marks the run stale so the preview and
// the write target agree).
type AnalyzeOutCommitMsg struct{ Path string }

// AnalyzeNextMsg is Enter on the header step: advance one step (root
// owns the validation gates).
type AnalyzeNextMsg struct{}

// AnalyzeStepDeltaMsg is PgUp/PgDn (±1) or Tab/shift-Tab (revisit) and
// the wizard's own Esc-back; forward transitions pass root's gates,
// backward ones are free.
type AnalyzeStepDeltaMsg struct{ Delta int }

type (
	// AnalyzeCommitSpecMsg is Enter on the spec-list step with the chosen or
	// typed path. Root validates it: a missing file becomes the inline field
	// error, never a crash and never a file created on the operator's behalf.
	AnalyzeCommitSpecMsg struct{ Value string }
	// AnalyzeCommitCaptureMsg is Enter on the capture-list step with the chosen
	// .pcap path. Committing with nothing to choose opens the root-owned file
	// picker instead, so the step is never a dead end.
	AnalyzeCommitCaptureMsg struct{ Value string }
)

// AnalyzeBrowseMsg is [f] on the capture step: open the shared
// root-owned file picker over .pcap files.
type AnalyzeBrowseMsg struct{}

// AnalyzeOutBrowseMsg is [f] in the freshly opened [o] output editor
// (UAT round 8 finding 6: "selection of the folder should be
// available"): it hands the keyboard to the shared root-owned picker so
// the operator can browse to the output location; Draft carries the
// seeded/half-typed path so the browse starts there. The editor closes
// with the message — a pick commits root-side, an Esc leaves the
// effective path untouched.
type AnalyzeOutBrowseMsg struct{ Draft string }

type (
	// AnalyzeChooseGoalMsg is one of the run step's direct keys (t, r, s)
	// selecting what the analyze run should produce.
	AnalyzeChooseGoalMsg struct{ Goal string }
	// AnalyzeChooseHeaderMsg is the header step's list selection: the length
	// header the framed messages will carry.
	AnalyzeChooseHeaderMsg struct{ Header string }
	// AnalyzeChooseMaskMsg is the [m] toggle on the run step, switching the
	// sensitive-field masking on or off for the messages that go out.
	AnalyzeChooseMaskMsg struct{ Raw bool }
)

// AnalyzeRunMsg is Enter on the run step: start the analysis through
// root's existing run leg with the folded inline options; Filter is the
// run step's flow filter ("" = every enumerated dst flow). The run's
// port set is the filter-visible rows root already marks Selected.
type AnalyzeRunMsg struct{ Filter string }

// AnalyzeFlowToggleMsg is space on the run step's flow cursor: root toggles
// that (port, direction) in the pending run set (scenario folds the port's
// two directions into one unit; UAT round 7).
type AnalyzeFlowToggleMsg struct {
	Port int
	Dir  string
}

// AnalyzeFlowToggleAllMsg is a on the run step: root includes every
// enumerated direction when they are not all included, else clears them all.
type AnalyzeFlowToggleAllMsg struct{}

// AnalyzeWriteMsg is w on the run step: persist the SELECTED generated
// items (the picker's outcome; UAT round 6).
type AnalyzeWriteMsg struct{}

// AnalyzeItemRow is one generated item in the picker: Key is the
// identity root tracks the deselection by (app.ItemKey form), Kind the
// config discriminator, Preview the item as it lands in the file
// (indented JSON), Included its membership in the pending write set.
type AnalyzeItemRow struct {
	Key      string
	Name     string
	Kind     string
	Group    string // shared by a transaction and its dataset, so they toggle together (UAT round 7)
	Included bool
	Preview  string
}

// AnalyzeItemsApplyMsg is Enter in the item picker: root stores the
// deselection set and the next w writes exactly the selected items.
type AnalyzeItemsApplyMsg struct{ Excluded []string }

// AnalyzeAbortMsg is Esc on the first step: root cancels in-flight legs
// (seq token) and leaves; a run in flight asks §N3 confirm first.
type AnalyzeAbortMsg struct{}
