// workers_state.go holds the §H state contract and the page→router
// messages. Root derives every display string with its injectable clock
// and pushes it via SetState; the page never touches internal/app nor
// reads the clock. Row status flips only when the WorkerStopped event
// arrives — stops never write status optimistically.
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

// WorkerRow is one WORKERS table row; every column is a finished display
// string and Status is one of the canonical tokens above.
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

// ProgressRow is one per-worker progress line. Pct < 0 means an unknown
// total (the page renders the degraded count line, never a spinner);
// ETA is root-derived.
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

// StressSummaryState is a completed stress run's summary, all sections
// root-built. nil anywhere means "no summary"; the page never invents one.
type StressSummaryState struct {
	WorkerID string
	// Status is the canonical status token; Success is the ok/sent ratio
	// ("" when nothing was sent).
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
// Sparkline holds the normalized 0..7 block heights, SparkLabel the TPS
// prefix, Net the derived net-health line, StatusLine the root-stamped
// action line.
type WorkersState struct {
	Workers    []WorkerRow
	Sparkline  []int
	SparkLabel string
	Net        string
	Progress   []ProgressRow
	Summary    *StressSummaryState
	StatusLine string
}

// WorkersOpenFormMsg asks the router to open a start form
// (b = "bgsend", t = "stress").
type WorkersOpenFormMsg struct{ Kind string }

// WorkersStopMsg asks the router to stop the worker with the given id
// (`k`). Terminal rows are root-checked no-ops; the row flips only when
// WorkerStopped arrives.
type WorkersStopMsg struct{ ID string }

// WorkersStopAllMsg asks the router to stop every worker (`K`); root
// confirms first when workers are active.
type WorkersStopAllMsg struct{}

// WorkersPopMsg asks the router to pop the §H page (Esc; no-op at
// depth 1). The summary overlay owns Esc first while open.
type WorkersPopMsg struct{}
