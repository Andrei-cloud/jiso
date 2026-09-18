// root_server.go owns the §G live operation: the page stays a
// presentation-only consumer of ServerState snapshots while root drives
// the embedded mock server through the app's in-process serve façade and
// polls stats back into Update as the root-internal serverStatsTickMsg.
//
// The ~1s tick is armed while a snapshot-rendering page (§A or §G, see
// serverStatsConsumer) is current and the server runs; leaving the page
// or stopping the server bumps the seq, so an in-flight (uncancellable)
// tick turns stale and ignored. Stats freeze at the stop-time snapshot;
// nothing ever auto-restarts the server.
package tui

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// serverTickInterval is the §G stats refresh cadence (~1s).
const serverTickInterval = time.Second

// serverStatsTickMsg is one poll tick carrying its arming-generation seq;
// a seq that no longer matches the current one (page left, server
// stopped) marks the message stale and ignored.
type serverStatsTickMsg struct{ seq uint64 }

// serverStartResultMsg is the terminal verdict of a start attempt: the
// requested port/header/spec/routes ride along so the success path
// stamps the truth (and the last-start memory) without re-reading the
// form.
type serverStartResultMsg struct {
	port   string
	header string
	spec   string
	routes string
	err    error
}

// serverStopResultMsg closes a stop attempt; stats are already frozen
// (snapshotted before the stop ran), the result only flips the truth.
type serverStopResultMsg struct{ err error }

// defaultServerTick is the production tick scheduler; tests inject
// m.serverTickf to observe arming without a clock.
func defaultServerTick(d time.Duration, mk func() tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return mk() })
}

// serverRunning is the root-side running truth (start result stamped it).
func (m *RootModel) serverRunning() bool { return !m.serverStartAt.IsZero() }

// serveStatsSnapshot reads the stats truth: the injected reader, else
// the app's in-process accessor (never a snapshot file).
func (m *RootModel) serveStatsSnapshot() *app.ServerStats {
	if m.serveStatsFn != nil {
		return m.serveStatsFn()
	}
	if m.app == nil {
		return nil
	}

	return m.app.ServeSnapshot()
}

// serveRoutesList reads the configured/active mock routes.
func (m *RootModel) serveRoutesList() []config.MockRouteConfig {
	if m.serveRoutesFn != nil {
		return m.serveRoutesFn()
	}
	if m.app == nil {
		return nil
	}

	return m.app.ServeRoutes()
}

// serverStatsConsumer reports the pages that render the live mock-server
// snapshot and so need the ~1s poll armed while the server runs: the §G
// server page AND the §A dashboard.
func serverStatsConsumer(id string) bool {
	return id == pages.ServerPageID || id == pages.DashboardPageID
}

// armServerTick enforces the tick lifecycle (see file header): arm on
// page-enter/running, disarm (bump the seq, drop the wait flag) on
// page-leave/stop. It runs from the Update wrapper after every message,
// so a jump onto the server page arms the first tick immediately.
func (m *RootModel) armServerTick() tea.Cmd {
	active := m.Current() != nil && serverStatsConsumer(m.Current().ID())
	if !active || !m.serverRunning() {
		if m.serverTickWait {
			m.serverTickSeq++
			m.serverTickWait = false
			m.debug.logf("server tick disarm seq=%d", m.serverTickSeq)
		}

		return nil
	}
	if m.serverTickWait {
		return nil
	}

	seq := m.serverTickSeq
	m.serverTickWait = true

	tickf := m.serverTickf
	if tickf == nil {
		tickf = defaultServerTick
	}

	return tickf(serverTickInterval, func() tea.Msg { return serverStatsTickMsg{seq: seq} })
}

// applyServerStatsTick folds one poll tick into the stats snapshot;
// stale seqs (armed before a leave/stop) are dropped without effect.
func (m *RootModel) applyServerStatsTick(msg serverStatsTickMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.serverTickSeq {
		return m, nil
	}
	m.serverTickWait = false
	if !m.serverRunning() {
		return m, nil
	}
	if snap := m.serveStatsSnapshot(); snap != nil {
		m.serverSnap = snap
	}
	m.debug.logf("server stats tick seq=%d", msg.seq)

	return m, nil
}

// handleServerStop is the `s` path: with live connections it opens the
// confirm dialog first (default No), otherwise it stops directly as a
// Cmd. The connection truth is decided from a FRESH snapshot — the
// cached serverSnap can be stale after re-entering §G, and reading it
// alone would stop with live connections and no confirm.
func (m *RootModel) handleServerStop() (tea.Model, tea.Cmd) {
	if !m.serverRunning() {
		m.debug.logf("server stop ignored (not running)")

		return m, nil
	}
	if snap := m.serveStatsSnapshot(); snap != nil {
		m.serverSnap = snap
	}
	conns := 0
	if m.serverSnap != nil {
		conns = m.serverSnap.ActiveConnections
	}
	if conns > 0 {
		th := m.theme
		if th == nil {
			th = theme.Default()
		}
		m.serverConfirm = widgets.NewConfirmDialog(th,
			fmt.Sprintf("stop mock server :%s with %d live connection(s)?", m.serverPort, conns))
		m.debug.logf("server stop confirm conns=%d", conns)

		return m, nil
	}

	return m.performStop()
}

// performStop freezes the final stats snapshot BEFORE the stop runs (so
// the frozen card shows the last truth, not post-stop zeros) and returns
// the stop Cmd; the engine's Stop itself is quick, no progress line.
func (m *RootModel) performStop() (tea.Model, tea.Cmd) {
	if snap := m.serveStatsSnapshot(); snap != nil {
		m.serverSnap = snap
	}

	stop := m.serveStopFn
	if stop == nil {
		if m.app == nil {
			m.serverError = errNoAppWired

			return m, nil
		}
		stop = m.app.ServeStop
	}
	m.debug.logf("server stop requested port=%s", m.serverPort)

	return m, func() tea.Msg { return serverStopResultMsg{err: stop()} }
}

// applyServerConfirmed is the y path of the stop confirm.
func (m *RootModel) applyServerConfirmed() (tea.Model, tea.Cmd) {
	if m.serverConfirm == nil {
		return m, nil // straggler after a pop
	}
	m.serverConfirm = nil

	return m.performStop()
}

// applyServerCancelled is the n/Esc/Enter (default No) path: the server
// keeps running and no stop is ever armed.
func (m *RootModel) applyServerCancelled() (tea.Model, tea.Cmd) {
	if m.serverConfirm == nil {
		return m, nil
	}
	m.serverConfirm = nil
	m.debug.logf("server stop canceled")

	return m, nil
}

// updateServerConfirmKey routes every key to the pending confirm dialog;
// its decision comes back as a ConfirmedMsg/CancelledMsg.
func (m *RootModel) updateServerConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	next, cmd := m.serverConfirm.Update(msg)
	m.serverConfirm = next

	return m, cmd
}

// applyServerStopResult flips the running truth; the stats snapshot stays
// frozen (uptime renders from it) and nothing re-arms. A FAILED stop keeps
// the running truth — only a successful stop clears it.
func (m *RootModel) applyServerStopResult(msg serverStopResultMsg) (tea.Model, tea.Cmd) {
	m.serverStarting = false
	if msg.err != nil {
		m.serverError = msg.err.Error()
		m.debug.logf("server stop failed port=%s err=%v", m.serverPort, msg.err)

		return m, nil
	}
	m.serverStartAt = time.Time{}
	m.serverError = ""
	m.debug.logf("server stopped port=%s", m.serverPort)

	return m, nil
}

// syncServer pushes a fresh §G snapshot into the canonical page instance
// (Update-wrapper placement mirrors syncDashboard/syncSend/syncScenarios).
func (m *RootModel) syncServer() {
	if m.server == nil {
		return
	}
	m.server.SetState(m.serverState())
}

// serverState derives the §G snapshot from the root-side truth: the
// running flags, the (frozen while stopped) stats snapshot, and the
// route rows from the active/config mock routes. Uptime is stamped with
// the injectable clock; the page never reads it.
func (m *RootModel) serverState() pages.ServerState {
	st := pages.ServerState{
		Running:  m.serverRunning(),
		Starting: m.serverStarting,
		Error:    m.serverError,
		Port:     m.serverPort,
		Header:   m.serverHeader,
	}
	switch {
	case st.Running:
		st.Uptime = m.now().Sub(m.serverStartAt)
	case m.serverSnap != nil:
		st.Uptime = m.serverSnap.Uptime // frozen at the stop-time value
	}
	if snap := m.serverSnap; snap != nil {
		st.StatsKnown = true
		st.Stats = serveStatsCard(snap)
	}
	st.Routes = m.serverRouteRows()
	if len(m.serverLog) > 0 {
		st.Log = append([]string(nil), m.serverLog...)
	}

	return st
}

// serveStatsCard derives the §G stats card (reused by the §A MOCK
// SERVER card): the matched-vs-fallback split the tracker already
// carries, with the match percent root-formatted.
func serveStatsCard(snap *app.ServerStats) pages.StatsCard {
	return pages.StatsCard{
		Served:    int(snap.TotalServed),
		Matched:   int(snap.Matched),
		MatchPct:  matchPct(snap.Matched, snap.TotalServed),
		Fallback:  int(snap.RouteCounts[app.ServeFallbackRoute]),
		Dropped:   int(snap.Dropped),
		ReqErr:    int(snap.RequestErrors),
		LiveConns: snap.ActiveConnections,
	}
}

// matchPct is the one-decimal match ratio ("99.5%"); a zero
// served total is unknown, not 0.0% — the caller renders the dash.
func matchPct(matched, served int64) string {
	if served <= 0 {
		return ""
	}

	return fmt.Sprintf("%.1f%%", float64(matched)/float64(served)*100.0)
}

// serverRouteRows builds the ROUTES table rows from the configured/active
// mock routes; hit counts come from the frozen-or-live snapshot (unknown
// before the first snapshot, dash-rendered by the page).
func (m *RootModel) serverRouteRows() []pages.RouteRow {
	routes := m.serveRoutesList()
	if len(routes) == 0 {
		return nil
	}
	var counts map[string]int64
	if m.serverSnap != nil {
		counts = m.serverSnap.RouteCounts
	}

	rows := make([]pages.RouteRow, 0, len(routes))
	th := m.themeOrNil()
	for _, r := range routes {
		rows = append(rows, pages.RouteRow{
			ID:      r.Name,
			Match:   routeMatchCell(th, r),
			Resp:    r.ResponseMTI,
			Hits:    counts[r.Name],
			Latency: routeLatencyCell(r),
			Detail:  routeDetail(th, r),
		})
	}

	return rows
}

// routeCellValueCells caps one rendered value in a §G route cell: longer
// strings take the theme's ellipsis and the full value belongs in the
// detail, which is where the whole pair lives.
const routeCellValueCells = 24

// routeMatchCell is the compact MATCH cell: the route name plus the MTI it
// matches, with the processing code when the route narrows to one
// ("Purchase Auth 0200/000000"), or the bare name plus "any" when the route
// declares no match fields (the catch-all — `serve routes` says ANY).
// Match pairs never render here: a row summarises, the detail reveals.
func routeMatchCell(th *theme.Theme, r config.MockRouteConfig) string {
	mti, hasMTI := routeScalar(th, r.MatchFields["0"])
	de3, hasDE3 := routeScalar(th, r.MatchFields["3"])

	switch {
	case len(r.MatchFields) == 0:
		return r.Name + " any"
	case hasMTI && hasDE3:
		return r.Name + " " + mti + "/" + de3
	case hasMTI:
		return r.Name + " " + mti
	default:
		// Matches on other fields only: the name is all a row can say about
		// it, and it never claims the catch-all "any".
		return r.Name
	}
}

// routeScalar reads one match criterion for the row summary. Only a scalar
// summarises a route — a nested payload is no summary, so the caller falls
// back to the route name and the pair stays in the detail.
func routeScalar(th *theme.Theme, v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return th.Truncate(val, routeCellValueCells), true
	case float64, int, bool:
		return fmt.Sprintf("%v", val), true
	}

	return "", false
}

// routeLatencyCell is the LATENCY cell: the effective base delay
// (delay_ms, else latency_ms — the engine's own fallback) with the
// jitter suffix the design shows ("100±25ms").
func routeLatencyCell(r config.MockRouteConfig) string {
	base := routeBaseDelayMs(r)
	if r.JitterMs > 0 {
		return strconv.Itoa(base) + "\xc2\xb1" + strconv.Itoa(r.JitterMs) + "ms"
	}

	return strconv.Itoa(base) + "ms"
}

// routeDetail builds the Enter-on-route detail view from the route
// config (internal/config MockRouteConfig — the tx file mock_routes
// schema): match/required/echo/response fields, latency, drop flag.
func routeDetail(th *theme.Theme, r config.MockRouteConfig) pages.RouteDetail {
	d := pages.RouteDetail{
		Name:           r.Name,
		Description:    r.Description,
		MatchLines:     sortedFieldPairs(th, r.MatchFields),
		RequiredLines:  append([]string(nil), r.RequiredFields...),
		RespMTI:        r.ResponseMTI,
		RespLines:      sortedFieldPairs(th, r.ResponseFields),
		Latency:        routeLatencyDetail(r),
		DropConnection: r.DropConnection,
	}
	for _, e := range r.EchoFields {
		d.EchoLines = append(d.EchoLines, strconv.Itoa(e))
	}

	return d
}

// routeLatencyDetail is the detail view's long-form latency line.
func routeLatencyDetail(r config.MockRouteConfig) string {
	base := routeBaseDelayMs(r)
	if r.JitterMs > 0 {
		return strconv.Itoa(base) + "ms \xc2\xb1" + strconv.Itoa(r.JitterMs) + "ms"
	}

	return strconv.Itoa(base) + "ms"
}

// routeBaseDelayMs mirrors MockRouteConfig.GetTotalDelay's base-delay
// fallback (delay_ms wins, latency_ms is the alias) without its jitter
// random draw — the cell shows the configured values, not a sample.
func routeBaseDelayMs(r config.MockRouteConfig) int {
	if r.DelayMs == 0 && r.LatencyMs > 0 {
		return r.LatencyMs
	}

	return r.DelayMs
}

// sortedFieldPairs renders a match/response field map deterministically
// ("11=000000" per entry, keys ascending), capping each value: the full
// value is the detail's job, a %v dump of a nested map is not readable.
func sortedFieldPairs(th *theme.Theme, fields map[string]any) []string {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+fieldValue(th, fields[k]))
	}

	return pairs
}

// fieldValue renders one field-map value for display: a long string is
// clipped with the theme's ellipsis (ASCII profile included), and a map or
// slice is counted rather than dumped.
func fieldValue(th *theme.Theme, v any) string {
	switch val := v.(type) {
	case string:
		return th.Truncate(val, routeCellValueCells)
	case map[string]any:
		return compositeCount(len(val))
	case []any:
		return compositeCount(len(val))
	default:
		return fmt.Sprintf("%v", v)
	}
}

// compositeCount names the size of a nested value that a cell cannot show
// in full ("2 values", "1 value").
func compositeCount(n int) string {
	if n == 1 {
		return "1 value"
	}

	return strconv.Itoa(n) + " values"
}
