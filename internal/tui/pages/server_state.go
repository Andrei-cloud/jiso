// server_state.go holds the §G state contract (wireframe
// .opencode/plans/02-tui-wireframes.md §G) and the page→router messages.
// Root owns the live operation: it starts/stops the embedded mock server
// through the app's in-process serve façade (internal/app serve.go — the
// same engine the cobra `serve start` shim runs), polls the engine's
// stats tracker via a ~1s cancellable tick while this page is shown and
// the server runs, derives every display string (match percent, route
// hit/latency cells, detail lines) with its injectable clock, and pushes
// ServerState snapshots via SetState — the page never touches
// internal/app and never reads the clock (SCR-501 data-flow contract).
package pages

import "time"

// ServerPageID is the router id of the §G mock-server page:
// hotkey 4, footer label "server".
const ServerPageID = "server"

// StatsCard is the left STATS box: the wireframe's five lines. Root
// derives MatchPct ("99.5%", one decimal) so the page does no math; the
// empty string renders as the dash (never 0.0% for "unknown"). Fallback
// counts the catch-all RC-12 path, Dropped the drop_connection route
// hits, ReqErr the failed request unpacks, LiveConns the open TCP
// clients (the confirm-on-stop trigger).
type StatsCard struct {
	Served    int
	Matched   int
	MatchPct  string
	Fallback  int
	Dropped   int
	ReqErr    int
	LiveConns int
}

// RouteDetail is the Enter-on-route detail view (wireframe: "select
// route → match/echo/latency detail view"). Every line is a root-derived
// display string from the route config (internal/config
// MockRouteConfig, parsed from the tx file's mock_routes section); the
// page renders labels + values verbatim.
type RouteDetail struct {
	Name           string
	Description    string
	MatchLines     []string // "11=000000" per match field
	RequiredLines  []string // "3", "11"
	EchoLines      []string // "11"
	RespMTI        string
	RespLines      []string // "39=00" per response field
	Latency        string   // "100ms ±25ms" / "0ms"
	DropConnection bool
}

// RouteRow is one ROUTES table row. ID is the stable route name (row
// identity across SetState recomposition); Match is the compact
// "name k=v…" match description, Latency the pre-formatted cell
// ("100±25ms"). Hits carries the served count; whether hits are known
// at all is ServerState.StatsKnown (never-started renders the dash,
// not a fake 0).
type RouteRow struct {
	ID      string
	Match   string
	Resp    string
	Hits    int64
	Latency string
	Detail  RouteDetail
}

// ServerState is the immutable snapshot root pushes into the page.
// Uptime is root-stamped from the injectable clock (Duration, never a
// timestamp) and freezes at the stop-time value once stopped. Starting
// marks an in-flight start (Enter pressed, listener not yet bound);
// Error is the last failed-operation line (stop failure — start
// failures stay inside the modal, connect-dialog pattern). StatsKnown
// is false only before the server has ever been snapshotted.
type ServerState struct {
	Running    bool
	Port       string
	Header     string
	Uptime     time.Duration
	Stats      StatsCard
	Routes     []RouteRow
	Starting   bool
	Error      string
	StatsKnown bool

	// Log is the mock server's own output lines (oldest first,
	// root-bounded ring copy); rendered ONLY on this page (UAT round
	// 3: server output leaked into other screens via stderr).
	Log []string
}

// ServerStopMsg asks the router to stop the mock server (`s`). Root
// owns the decision: with live connections it opens the confirm dialog
// first (widgets.ConfirmDialog), otherwise it stops directly as a Cmd.
type ServerStopMsg struct{}

// ServerPopMsg asks the router to pop the §G page (Esc; no-op at
// depth 1, the InspectorPopMsg/SendPopMsg/ScenarioPopMsg pattern).
type ServerPopMsg struct{}
