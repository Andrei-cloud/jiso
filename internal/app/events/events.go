// Package events defines the typed observation channel between the
// application core (internal/app) and frontends (CLI today, TUI later).
// The bus lets frontends watch worker progress and connection state
// without scraping stdout.
//
// Import rules mirror internal/app: stdlib only, no cobra/readline/survey,
// no lipgloss or jiso/internal/cli, and no writes to stdout/stderr.
//
// # Event taxonomy
//
// Event is a sealed interface: only the types in this file implement it
// (the unexported event method), so a frontend's type switch over the
// concrete types is exhaustive. WorkerProgress carries per-unit counters
// (Done/Total) chosen so stress-test progress (APP-204: worker count and
// per-worker message counts) maps onto it without new fields.
package events

// Event is the sealed interface implemented by every bus event.
type Event interface {
	// event marks this type as part of the closed taxonomy; it cannot be
	// implemented outside this package.
	event()
}

// Connection state values carried by ConnectionEvent.State.
const (
	// StateConnected is published when a Connect succeeded.
	StateConnected = "connected"
	// StateDisconnected is published when a Disconnect succeeded.
	StateDisconnected = "disconnected"
	// StateFailed is published when a Connect or Disconnect failed; the
	// failure text is carried in Detail.
	StateFailed = "failed"
)

// WorkerStarted announces a worker (background sender, stress worker,
// listener) has begun running. Kind is a short discriminator such as
// "bgsend" or "stress".
type WorkerStarted struct {
	ID   string
	Kind string
}

// WorkerProgress reports incremental progress of one worker. Done/Total
// are unit counts (messages, iterations); Total <= 0 means unbounded or
// unknown. Note is an optional human-readable suffix (e.g. current TPS).
type WorkerProgress struct {
	ID    string
	Done  int
	Total int
	Note  string
}

// WorkerStopped announces a worker finished. Reason is a short
// discriminator ("done", "stopped", "failed") optionally with detail.
type WorkerStopped struct {
	ID     string
	Reason string
}

// ConnectionEvent reports a connection state transition. State is one of
// StateConnected, StateDisconnected, StateFailed; Detail carries the
// remote address on success or the failure text on StateFailed.
type ConnectionEvent struct {
	State  string
	Detail string
}

// Logf carries a structured log line for frontends that render their own
// log view instead of scraping stdout. Level is free-form ("debug",
// "info", "warn", "error").
type Logf struct {
	Level string
	Msg   string
}

func (WorkerStarted) event()   {}
func (WorkerProgress) event()  {}
func (WorkerStopped) event()   {}
func (ConnectionEvent) event() {}
func (Logf) event()            {}

// Compile-time proof the taxonomy satisfies Event.
var (
	_ Event = WorkerStarted{}
	_ Event = WorkerProgress{}
	_ Event = WorkerStopped{}
	_ Event = ConnectionEvent{}
	_ Event = Logf{}
)
