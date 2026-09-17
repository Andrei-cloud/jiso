// root_session_stats.go owns the §A SESSION card's async counters
// The card never queries: root reads
// App.SessionStats for the live session id OFF the UI thread — the read
// runs inside a tea.Cmd and reports back as a seq-tokened
// sessionStatsTickMsg, exactly the §G serve-stats lifecycle
// (injectable-tickf seam, seq token drops stale results, nothing ever
// reads the DB in Update/View). The ~2s tick re-arms while the dashboard
// is current OR a DB-writing event dirtied the read: a send completion
// (Update wrapper) and a worker stop (updateBridgeMsg) set the dirty
// flag, so one query still runs while another page is current and the
// card is fresh when the operator returns. Failures leave the previous
// snapshot in place (the card keeps its last truth — or the dashes when
// nothing was ever known).
package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
)

// sessionStatsTickInterval is the §A SESSION card's refresh cadence
// (a 2 s tick, display only).
const sessionStatsTickInterval = 2 * time.Second

// sessionStatsTickMsg is one read's result carrying its arming-generation
// seq and the session id the read was taken for; a seq that no longer
// matches the current one (dashboard left without a dirty re-arm) marks
// the message stale and ignored — the serverStatsTickMsg contract.
type sessionStatsTickMsg struct {
	seq   uint64
	id    string
	stats *app.DbSessionStats
	err   error
}

// defaultSessionStatsTick is the production scheduler; the read itself
// runs inside the returned Cmd (mk is invoked on the timer goroutine, so
// the DB never sees the UI thread). Tests inject m.sessionStatsTickf to
// capture the sender and drive it by hand.
func defaultSessionStatsTick(d time.Duration, mk func() tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return mk() })
}

// sessionStatsLeg resolves the injectable read leg (nil = the App's
// SessionStats — the same façade the §I page and the CLI `db stats`
// shim drive) and the live session id. No leg without a reader; no
// production leg without a configured session id (querying "" would
// spin a failing read every 2 s). A hermetic test leg with no app
// config runs against the empty id (the fold's id check is config-
// gated).
func (m *RootModel) sessionStatsLeg() (func(context.Context, string) (*app.DbSessionStats, error), string) {
	fn := m.sessionStatsFn
	if fn == nil {
		if m.app == nil {
			return nil, ""
		}
		fn = m.app.SessionStats
	}
	if cfg := m.configOrNil(); cfg != nil {
		id := cfg.GetSessionID()
		if id == "" {
			return nil, ""
		}

		return fn, id
	}
	if m.sessionStatsFn == nil {
		return nil, ""
	}

	return fn, ""
}

// armSessionStatsTick enforces the tick lifecycle (the §G contract with
// the dirty exception): arm while the dashboard is current (steady ~2s
// refresh) or while a DB-writing event dirtied the read (one query even
// off-page); leaving the dashboard with nothing pending bumps the seq so
// an in-flight (uncancelable) tea.Tick turns stale and no new tick is
// scheduled.
func (m *RootModel) armSessionStatsTick() tea.Cmd {
	active := m.Current() != nil && m.Current().ID() == pages.DashboardPageID
	if !active && !m.sessionStatsDirty {
		if m.sessionStatsWait {
			m.sessionStatsSeq++
			m.sessionStatsWait = false
			m.debug.logf("session stats tick disarm seq=%d", m.sessionStatsSeq)
		}

		return nil
	}
	if m.sessionStatsWait {
		return nil
	}
	leg, id := m.sessionStatsLeg()
	if leg == nil {
		return nil
	}
	m.sessionStatsWait = true
	// The dirty flag is consumed at ARM time (the armSessions pattern): a
	// completion that lands after this read started re-dirties the card,
	// so the wrapper re-arms immediately once the result folds.
	m.sessionStatsDirty = false

	seq := m.sessionStatsSeq

	tickf := m.sessionStatsTickf
	if tickf == nil {
		tickf = defaultSessionStatsTick
	}

	return tickf(sessionStatsTickInterval, func() tea.Msg {
		stats, err := leg(context.Background(), id)

		return sessionStatsTickMsg{seq: seq, id: id, stats: stats, err: err}
	})
}

// applySessionStatsTick folds one read result: a stale seq (armed before
// a leave-without-dirty) is dropped without effect; a failed read leaves
// the previous snapshot untouched; a result for a session that is no
// longer the live one is dropped (the card never shows another
// session's counters); success stamps the snapshot the next
// syncDashboard renders.
func (m *RootModel) applySessionStatsTick(msg sessionStatsTickMsg) (tea.Model, tea.Cmd) {
	m.sessionStatsWait = false
	if msg.seq != m.sessionStatsSeq {
		m.debug.logf("session stats tick stale seq=%d current=%d", msg.seq, m.sessionStatsSeq)

		return m, nil
	}
	if msg.err != nil || msg.stats == nil {
		m.debug.logf("session stats tick err=%v", msg.err)

		return m, nil
	}
	if cfg := m.configOrNil(); cfg != nil && cfg.GetSessionID() != msg.id {
		m.debug.logf("session stats tick session changed id=%q", msg.id)

		return m, nil
	}
	m.sessionStatsSnap = msg.stats
	m.debug.logf("session stats tick folded total=%d", msg.stats.TotalTransactions)

	return m, nil
}
