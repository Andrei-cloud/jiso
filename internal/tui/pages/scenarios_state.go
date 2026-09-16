// scenarios_state.go holds the §F state contract (wireframe
// .opencode/plans/02-tui-wireframes.md §F) and the page→router messages.
// Root owns the live operation: it runs the SAME scenario engine the CLI
// `scenario run` uses (transactions.ScenarioRunner via the app service),
// streams per-step progress back as root-internal messages, derives every
// display string (MTI, RC, validation notes, summary banner) with its
// injectable clock and the shared app.ScenarioReport view, and pushes
// ScenariosState snapshots via SetState — the page never touches
// internal/app and never reads the clock (SCR-501 data-flow contract).
package pages

import (
	"strings"
	"time"
)

// ScenariosPageID is the router id of the §F scenarios page:
// hotkey 3, footer label "scenarios".
const ScenariosPageID = "scenarios"

// ScenarioRow is one scenario in the master list. ID is the stable row
// identity (the scenario name in the tx file) that survives filter
// recomposition; Name is the display text.
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
	// StepRunning is a step that started and has no result yet (renders ⏳ + "running",
	// symbol+word per contract).
	StepRunning
	// StepPass is a step that finished successfully (✓).
	StepPass
	// StepFail is a step that finished with an error or failed assertions (✗).
	StepFail
)

// StepRow is one step row as pre-derived display strings. MTI comes from
// the step's transaction template (field 0), RC from the response's field
// 39 when root could unpack it ("" renders as the dash). Note is the raw
// sub-line text (extract/validate summary on pass, `expect "00" got "96"`
// diff or engine error on fail); the page adds the theme status glyphs,
// so no glyph choices leak into state and ascii goldens stay 7-bit.
// Latency is root-stamped (time.Duration, never a timestamp).
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
// the selected scenario (§F overlay, UAT round 9 F-9e c). The step
// identity is ScenarioID + StepIndex (StepRow.Index, the declaration
// index the pane displays); a pushed Preview whose identity differs from
// the last shown one arms the overlay, a nil Preview clears it (the §I
// TxReviewState re-arm contract). Request/Response reuse the §I
// TxReviewMessage shape (the same `jiso db tx`-style reconstruction:
// packed hex + parsed fields). Loading marks an in-flight detail load
// (root arms it when ScenarioStepDetailMsg arrives, UAT round 9 Task
// 9.8b): the overlay then renders the loading marker instead of the
// empty state. Request/Response stay nil until root loads a message —
// the captured payload for a step of a completed run, the honest raw
// composition of the step's template for a never-run step — the page
// never invents one.
type ScenarioStepPreview struct {
	StepIndex  int // StepRow.Index of the previewed step
	ScenarioID string
	Loading    bool
	// Note is an honest load-failure line root folds when the detail load
	// produced no message at all (unknown scenario, template compose error):
	// the overlay shows it verbatim instead of the generic run hint, and
	// never a fabricated message (task 9.8b).
	Note     string
	Request  *TxReviewMessage
	Response *TxReviewMessage
	// Composed marks the Request as a template composition of a step that
	// has NOT run (root's ComposeRaw preview path) rather than an
	// engine-captured payload: the overlay then labels the REQUEST section
	// "composed from template - not sent yet", so a pending preview is
	// never mistaken for the real message (UAT round 9 F1). A captured
	// run payload keeps it false — the bytes speak for themselves.
	Composed bool
}

// ScenariosState is the immutable snapshot root pushes into the page.
// Scenarios is the full list (the page filters it locally);
// SelectedSteps holds the step rows for the scenario under the page's
// cursor (empty = none). Running marks a live engine run. Summary is the
// final banner (`3/3 passed · 7.1ms total`); ReportPath is the export
// destination root uses for `e` (the CLI --report convention: path as
// given, default scenario-report.json in cwd — the CLI flag itself has
// no default); StatusLine is the toast-less export feedback line
// (`report → path` / `no report yet`, TUI-406b will add real toasts).
// Preview is the step message preview overlay's payload (nil = nothing
// to preview; the page arms its overlay from a new Preview identity).
type ScenariosState struct {
	Scenarios     []ScenarioRow
	SelectedSteps []StepRow
	Running       bool
	Summary       string
	ReportPath    string
	StatusLine    string
	Preview       *ScenarioStepPreview
}

// matchText is the filter haystack: the lowercased display fields, so
// one substring hit keeps the row (transactions pattern).
func (r ScenarioRow) matchText() string {
	return strings.ToLower(r.ID + " " + r.Name)
}

// ScenarioRunMsg asks the router to run the scenario with ID through the
// engine (Enter). Root owns the live op: one run at a time, a run while
// one is in flight is ignored (no queue — the send pattern).
type ScenarioRunMsg struct {
	ID string
}

// ScenarioStepDetailMsg asks the router to load the message one step
// captured (Enter on a step row while the STEPS pane holds focus; UAT
// round 9 F-9e c). StepIndex is the step's declaration index
// (StepRow.Index, the number the pane displays) and ScenarioID is the
// scenario under the list cursor. Root owns the async load: it first
// clears ScenariosState.Preview (which re-arms the overlay for a
// re-request of the same step after Esc — the §I handleSessionsReview
// doctrine), then arms the payload (Loading first, then the
// reconstructed request/response), and the page opens its overlay when
// the pushed Preview identity changes.
type ScenarioStepDetailMsg struct {
	StepIndex  int
	ScenarioID string
}

// ScenarioExportMsg asks the router to write the last completed report
// as JSON to the report path ('e'). With no completed report root marks
// the status line honestly instead of silently succeeding.
type ScenarioExportMsg struct{}

// ScenarioPopMsg asks the router to pop the §F page (Esc). Root owns the
// stack; at depth 1 the pop is a no-op (InspectorPopMsg/SendPopMsg
// pattern — the consistent Esc/back behaviour of the merged pages).
type ScenarioPopMsg struct{}
