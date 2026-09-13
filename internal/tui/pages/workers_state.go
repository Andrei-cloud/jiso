// workers_state.go holds the §H state contract (wireframe
// .opencode/plans/02-tui-wireframes.md §H) and the page→router messages.
// Root owns the worker truth: it starts workers through the App manager
// (WorkerStart/StressStart), stops them (WorkerStop/WorkerStopAll), and
// folds the throttled WorkerStarted/WorkerProgress/WorkerStopped bus
// events into its row cache — the bridge consumer the events package was
// designed for. Every display string (status word, INTERVAL/TPS cell,
// runtime, circuit cell, sparkline heights, progress rows, summary
// sections) is root-derived with the injectable clock and pushed via
// SetState; the page never imports internal/app and never reads the
// clock (the SCR-501 data-flow contract). Row status flips ONLY when the
// WorkerStopped event arrives — stops never write status optimistically.
package pages

// WorkersPageID is the router id of the §H workers & stress page:
// hotkey 5, footer label "workers".
const WorkersPageID = "workers"

// WorkerRow.Status canonical values (the word half of the symbol+word
// cell; the page maps each to a theme kind, never color alone).
const (
	StatusRunning      = "running"
	StatusRamping      = "ramping"
	StatusDone         = "done"
	StatusStopped      = "stopped"
	StatusCircuitBroke = "circuit-broke"
)

// WorkerRow is one WORKERS table row: every column is a finished display
// string. Status is one of the canonical tokens above (the page renders
// symbol+word); ID is the canonical 8-char worker id (the table cursor
// position maps back to it for `k`).
type WorkerRow struct {
	ID          string
	Type        string // "background" | "stress"
	Txn         string // transaction name(s), joined
	Status      string
	Thr         string // sender count
	IntervalTPS string // "5s" (bgsend) or "60→120 tps" (stress)
	Runtime     string // HH:MM:SS (root-stamped from the injectable clock)
	OKFail      string // "50 / 0"
	Circuit     string // "" (nothing to report, dashed) | "4/10" | "TRIPPED (10/10)"
}

// ProgressRow is one per-worker progress line (superfile Processes
// pattern). Pct is the completion percent (−1 = unknown total → the page
// renders the degraded count line, never a spinner). Counts is the
// suffix ("7,940/19,200 sent"); ETA the root-derived suffix line
// ("ETA 01:12" while active, morphed to "elapsed 00:48" on done — the
// morph happens root-side with the fake clock).
type ProgressRow struct {
	ID     string
	Pct    int
	Counts string
	ETA    string
}

// SummaryKV is one label/value line of the stress summary overlay
// (percentiles, RC breakdown); values are display strings.
type SummaryKV struct {
	Label string
	Value string
}

// SummaryBarRow is one latency-histogram bucket: the page draws the bar
// relative to the largest count (█/░ blocks, #/. under ascii).
type SummaryBarRow struct {
	Label string
	Count int
}

// StressSummaryState is the completed stress run's summary (root-built
// from the app's StressSummary — the same sections the CLI stats view
// renders, restyled as boxed sections per UAT round 4: the report must
// be as informative as the legacy table). nil anywhere in the pipeline
// means "no summary": the page never invents one.
type StressSummaryState struct {
	WorkerID string
	// Status is the canonical status token (done/stopped/circuit-broke);
	// the title renders it as symbol+word. Success is the ok/sent ratio
	// ("99.3%", "" when nothing was sent).
	Status   string
	Success  string
	Run      []SummaryKV // transactions · target · plan · runtime · sent
	Headline []string    // legacy two-line headline (fixture compat)
	// Percentiles holds the latency stats; Budget the satisfactory/
	// tolerable/exceeded classification against the response timeout.
	Percentiles []SummaryKV
	Budget      []SummaryKV
	RCs         []SummaryKV // value carries the count + share
	Histogram   []SummaryBarRow
	// TxRows is the per-transaction-type breakdown (selection order).
	TxRows []SummaryTxRow
}

// SummaryTxRow is one per-transaction breakdown line of the stress
// summary; every field is a root-derived display string.
type SummaryTxRow struct {
	Name   string
	OKFail string // "ok 1,802 · err 12"
	Mean   string // "mean 0.22 ms"
	P99    string // "p99 41.0 ms"
	RCs    string // "rc 00(1,802) 96(12)"
}

// WorkersState is the immutable §H snapshot root pushes into the page.
// Sparkline holds the normalized 0..7 block heights (root keeps the
// last ~24 TPS samples; pages.NormalizeSparkline does the math);
// SparkLabel the "w-2 inst 118.4 · avg 96.2" prefix; Net the derived
// net-health line ("" renders nothing). StatusLine is the root-stamped
// action line (no-op notices, stop errors).
type WorkersState struct {
	Workers    []WorkerRow
	Sparkline  []int
	SparkLabel string
	Net        string
	Progress   []ProgressRow
	Summary    *StressSummaryState
	StatusLine string
}

// WorkersOpenFormMsg asks the router to open a start form (b = "bgsend",
// t = "stress"); the modal reuses the §E dialog machinery, as the §G
// server form did.
type WorkersOpenFormMsg struct{ Kind string }

// WorkersStopMsg asks the router to stop the worker with the given id
// (`k` on the selected row). Terminal rows are root-checked no-ops with
// a status line; the row itself flips only when WorkerStopped arrives.
type WorkersStopMsg struct{ ID string }

// WorkersStopAllMsg asks the router to stop every worker (`K`); with
// active workers root opens the widgets.ConfirmDialog first
// (default No — the SCR-507 pattern).
type WorkersStopAllMsg struct{}

// WorkersPopMsg asks the router to pop the §H page (Esc; no-op at
// depth 1, the ServerPopMsg pattern). The summary overlay owns Esc
// first while open.
type WorkersPopMsg struct{}
