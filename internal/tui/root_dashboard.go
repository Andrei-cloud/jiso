package tui

import (
	"strings"
	"time"

	"jiso/internal/app/events"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// syncDashboard pushes a fresh DashboardState snapshot into the canonical
// dashboard instance. It runs from NewRootModel, after every Update, and
// at the end of updateBridgeMsg, so the page always renders the freshest
// app/bridge truth without ever touching internal/app itself. It uses
// only the injectable clock.
func (m *RootModel) syncDashboard() {
	if m.dash == nil {
		return
	}
	m.dash.SetState(m.dashboardState())
}

// dashboardState derives the §A snapshot: the connection card from the
// bridge's latest ConnectionEvent (falling back to the app snapshot), the
// static config fields, and the palette action registry for quick actions.
// Unwired metrics (tx counters, latency) stay nil and render as the dash.
func (m *RootModel) dashboardState() pages.DashboardState {
	st := pages.DashboardState{Actions: m.dashActions}

	if m.conn != nil {
		st.Conn.Status = connCardStatus(*m.conn)
	}
	if st.Conn.Status == pages.ConnUnknown && m.app != nil && m.app.IsConnected() {
		st.Conn.Status = pages.ConnOnline
	}
	if st.Conn.Status == pages.ConnOnline && m.connSince != nil {
		up := m.now().Sub(*m.connSince)
		st.Conn.Uptime = &up
	}
	// The page learns "a connection exists" from this bool (quick-
	// actions visibility); it derives from the card status above.
	st.HasConnection = st.Conn.Status == pages.ConnOnline
	// The grid's card snapshots, all root-derived.
	st.LastSend = m.lastSendCard()
	st.LastStress = m.lastStress
	st.Server = m.dashServerCard()
	if len(m.serverLog) > 0 {
		st.ServerLog = append([]string(nil), m.serverLog...)
	}

	if m.app != nil {
		if cfg := m.app.Config(); cfg != nil {
			if host := cfg.GetHost(); host != "" {
				st.Conn.Target = host + ":" + cfg.GetPort()
			}
			st.Conn.Header = m.effectiveHeader()
			st.Conn.TLS = tlsDisplay(cfg)
			retries := cfg.GetReconnectAttempts()
			st.Conn.Retries = &retries
		}
	}
	st.Session = m.dashSessionCard()

	return st
}

// lastSendCard derives the §A LAST SEND card from the frozen §D state
// (the same state the ":last send" palette action shows): time, tx name,
// the MTI rows read off the field-0 exchange rows, the RC badge and the
// validation truths. The time is the completion stamp, never the clock.
func (m *RootModel) lastSendCard() *pages.LastSendCard {
	st := m.lastSend
	if st == nil {
		return nil
	}
	c := &pages.LastSendCard{
		TxName:      st.TxName,
		RC:          st.RC,
		RCNote:      st.RCLabel,
		Elapsed:     st.Elapsed,
		Validated:   st.Validated,
		Correlation: st.CorrelationOK,
	}
	if !m.lastSendAt.IsZero() {
		c.Time = m.lastSendAt.Format("15:04:05")
	}
	c.ReqMTI = exchangeMTI(st.Request)
	c.RespMTI = exchangeMTI(st.Response)

	return c
}

// exchangeMTI is the MTI cell of one exchange pane: the field-0 row's
// display value (the same source the §D pane titles read).
func exchangeMTI(rows []pages.ExchangeRow) string {
	for _, r := range rows {
		if r.Num == "0" {
			return r.Display
		}
	}

	return ""
}

// dashServerCard derives the §A MOCK SERVER card: the running truth the
// start/stop results stamped, the frozen-while-stopped uptime, and the
// §G stats card (the same serveStatsCard the §G page renders).
func (m *RootModel) dashServerCard() pages.ServerCard {
	sc := pages.ServerCard{
		Running: m.serverRunning(),
		Port:    m.serverPort,
		Header:  m.serverHeader,
	}
	switch {
	case sc.Running:
		sc.Uptime = m.now().Sub(m.serverStartAt)
	case m.serverSnap != nil:
		sc.Uptime = m.serverSnap.Uptime // frozen at the stop-time value
	}
	if m.serverSnap != nil {
		sc.StatsKnown = true
		sc.Stats = serveStatsCard(m.serverSnap)
	}

	return sc
}

// dashSessionCard derives the §A SESSION card: id and db path from the
// config (home-shortened for display), counters from the last
// successful async SessionStats snapshot (Known), dashes until one
// lands. Failures leave the previous snapshot — never a zero lie.
func (m *RootModel) dashSessionCard() *pages.SessionCard {
	sc := &pages.SessionCard{}
	if cfg := m.configOrNil(); cfg != nil {
		sc.ID = m.shortSessionID(cfg.GetSessionID())
		sc.DBPath = displayDbPath(m.homeDir, cfg.GetDbPath())
	}
	if snap := m.sessionStatsSnap; snap != nil {
		sc.TxSent = snap.TotalTransactions
		sc.OK = snap.SuccessfulTransactions
		sc.Fail = snap.FailedTransactions
		sc.AvgResponse = time.Duration(snap.AverageProcessingTimeMs * float64(time.Millisecond))
		sc.Known = true
	}

	return sc
}

// displayDbPath shortens an absolute db path under the user's home to
// the wireframe's "~/..." form; anything else (including a missing home)
// stays verbatim.
func displayDbPath(home, path string) string {
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}

	return path
}

// effectiveHeader is the framing the CONNECTION card and the header
// chip render: the live (or last live) link's effective header, else
// the configured header, else the app fallback. The last-live value
// survives a disconnect: it describes the link the operator last had,
// and the next connect stamps it again from its own options.
func (m *RootModel) effectiveHeader() string {
	if m.connHeader != "" {
		return m.connHeader
	}

	return effectiveLengthType(nil, m.configOrNil())
}

// connCardStatus maps a bus ConnectionEvent onto the card status.
func connCardStatus(ev events.ConnectionEvent) pages.ConnStatus {
	switch ev.State {
	case events.StateConnected:
		return pages.ConnOnline
	case events.StateFailed:
		return pages.ConnFailed
	case events.StateDisconnected:
		return pages.ConnOffline
	default:
		return pages.ConnUnknown
	}
}

// tlsDisplay reports the TLS state from the config: "off" without a TLS
// file (or a disabled one), "mTLS" with a client cert, plain "TLS" with a
// CA/server pin only.
func tlsDisplay(cfg *config.Config) string {
	tc := cfg.GetTLSConfig()
	if cfg.GetTLSConfigPath() == "" || tc == nil || !tc.Enabled {
		return "off"
	}
	if tc.ClientCert != "" && tc.ClientKey != "" {
		return "mTLS"
	}

	return "TLS"
}
