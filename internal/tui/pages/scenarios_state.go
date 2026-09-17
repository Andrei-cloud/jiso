// scenarios_state.go holds the page state contract and the page→router
// messages. Root owns the live operation and derives every display
// string; the page never touches internal/app and never reads the clock.
package pages

import (
	"strings"
	"time"
)

// ScenariosPageID is the router id of the scenarios page.
const ScenariosPageID = "scenarios"

// ScenarioRow is one scenario in the master list. ID is the stable row
// identity surviving filter recomposition; Name is the display text.
type ScenarioRow struct {
	ID   string
	Name string
}

// StepStatus is one step's execution state. Zero value is StepPending
// (declared, not yet run) — never a fake pass.
type StepStatus uint8

const (
	// StepPending is a declared step that has not started yet.
	StepPending StepStatus = iota
	// StepRunning is a step that started and has no result yet.
	StepRunning
	// StepPass is a step that finished successfully.
	StepPass
	// StepFail is a step that finished with an error or failed assertions.
	StepFail
)

// StepRow is one step row as pre-derived display strings. MTI comes from
// the template's field 0, RC from the response's field 39 ("" renders as
// the dash — unknown, never guessed). Note is raw sub-line text; the page
// adds the theme status glyphs. Latency is a root-stamped duration.
type StepRow struct {
	Index   int
	Name    string
	MTI     string
	RC      string
	Note    string
	Latency time.Duration
	Status  StepStatus
}

// ScenarioStepPreview is the message preview root pushes for one step of
// the selected scenario. The identity is ScenarioID + StepIndex: a pushed
// Preview whose identity differs from the last shown one arms the
// overlay, a nil Preview clears it. Request/Response reuse the
// TxReviewMessage reconstruction and stay nil until root loads one — the
// page never invents a message.
type ScenarioStepPreview struct {
	StepIndex  int // StepRow.Index of the previewed step
	ScenarioID string
	Loading    bool
	// Note is an honest load-failure line root folds when the detail load
	// produced no message at all; shown verbatim, never a fabricated message.
	Note     string
	Request  *TxReviewMessage
	Response *TxReviewMessage
	// Composed marks the Request as a template composition of a step that
	// has NOT run rather than an engine capture; the overlay labels it so.
	Composed bool
}

// ScenariosState is the immutable snapshot root pushes into the page:
// the full scenario list (filtered locally), the selected scenario's step
// rows, run flags, the final banner, the export ReportPath and status
// line, and the preview overlay's payload (nil = nothing to preview).
type ScenariosState struct {
	Scenarios     []ScenarioRow
	SelectedSteps []StepRow
	Running       bool
	Summary       string
	ReportPath    string
	StatusLine    string
	Preview       *ScenarioStepPreview
}

// matchText is the filter haystack: the lowercased display fields.
func (r ScenarioRow) matchText() string {
	return strings.ToLower(r.ID + " " + r.Name)
}

// ScenarioRunMsg asks the router to run the scenario with id (Enter).
// A run while one is in flight is ignored (single live op, no queue).
type ScenarioRunMsg struct {
	ID string
}

// ScenarioStepDetailMsg asks root to load one step's captured messages
// (Enter on the STEPS pane). Root clears Preview first so a same-step
// re-request after Esc sees nil→payload, and the page arms its overlay
// when the pushed Preview identity changes.
type ScenarioStepDetailMsg struct {
	StepIndex  int
	ScenarioID string
}

// ScenarioExportMsg asks the router to write the last completed report
// as JSON to the report path ('e'); with none, root marks the status line
// honestly instead of silently succeeding.
type ScenarioExportMsg struct{}

// ScenarioPopMsg asks the router to pop the scenarios page (Esc); at
// depth 1 the pop is a no-op.
type ScenarioPopMsg struct{}
