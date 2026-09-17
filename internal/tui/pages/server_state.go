// server_state.go holds the §G state contract and the page→router
// messages. Root owns the live operation (start/stop through the app serve
// façade, ~1s stats tick while shown) and derives every display string
// with its injectable clock; the page never touches internal/app and never
// reads the clock.
package pages

import "time"

// ServerPageID is the router id of the §G mock-server page:
// hotkey 4, footer label "server".
const ServerPageID = "server"

// StatsCard is the left STATS box; root derives MatchPct ("" renders the
// dash, never a fake 0.0% for unknown). Fallback = catch-all path,
// Dropped = drop_connection hits, LiveConns = open clients (confirm-on-stop).
type StatsCard struct {
	Served    int
	Matched   int
	MatchPct  string
	Fallback  int
	Dropped   int
	ReqErr    int
	LiveConns int
}

// RouteDetail is the Enter-on-route detail view: every line is a
// root-derived display string from the route config, rendered verbatim.
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
// identity across SetState recomposition). Whether Hits are known at all is
// ServerState.StatsKnown (never-started renders the dash, not a fake 0).
type RouteRow struct {
	ID      string
	Match   string
	Resp    string
	Hits    int64
	Latency string
	Detail  RouteDetail
}

// ServerState is the immutable snapshot root pushes into the page. Uptime
// (a Duration, never a timestamp) freezes at the stop-time value; Starting
// marks an in-flight start; Error is the last failed-operation line;
// StatsKnown is false only before the server was ever snapshotted.
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
	// root-bounded ring copy); rendered ONLY on this page.
	Log []string
}

// ServerStopMsg asks the router to stop the mock server (`s`). Root
// owns the decision: with live connections it opens the confirm dialog
// first (widgets.ConfirmDialog), otherwise it stops directly as a Cmd.
type ServerStopMsg struct{}

// ServerPopMsg asks the router to pop the §G page (Esc; no-op at depth 1).
type ServerPopMsg struct{}
