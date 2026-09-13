// root_sessions.go owns the §I DB truth (SCR-509). The page is a
// presentation-only consumer of SessionsState snapshots while root
// queries the App read façade (ListSessions/SessionStats/TxHistory/
// ReviewTx — the same db.OpenExisting read path the CLI `db stats`/
// `db tx` shims drive, PAR-311) OFF the UI thread: every query runs
// inside a tea.Cmd and reports back as a seq-tokened msg, so Update
// never blocks. Loads arm only while the page is current — on entry,
// after `r`, and after any worker/send completion (bgsend workers and
// §D sends write the DB: WorkerStopped on the bus and a Done send run
// mark the cache dirty; the next Update re-queries). A missing or unset
// DB is a typed façade error folded into the state's Note (empty-state
// text), never a crash and never a created file. sessionsSrc overrides
// the app legs for tests (fake façade; no optimistic writes — the list
// changes only when a query result msg arrives).
package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/db"
	"jiso/internal/tui/pages"
)

// Query caps (display budgets, not truncation lies: the panes show the
// newest rows and the §I browser is not a pager).
const (
	sessionsListLimit    = 200
	sessionsHistoryLimit = 100
)

// sessionsSource is the §I read façade leg: the App methods match it
// structurally, and tests inject a fake (no real DB needed above).
type sessionsSource interface {
	DBPath() string
	ListSessions(ctx context.Context, limit int) ([]app.DbSessionView, error)
	SessionStats(ctx context.Context, sessionID string) (*app.DbSessionStats, error)
	TxHistory(ctx context.Context, sessionID string, limit int) ([]app.DbTransactionView, error)
	ReviewTx(ctx context.Context, txRowID int64) (*app.DbTransactionRetrospective, error)
}

// sessionsSource resolves the injectable leg (nil = the App façade;
// nil App = no leg, the page stays in its empty state).
func (m *RootModel) sessionsSource() sessionsSource {
	if m.sessionsSrc != nil {
		return m.sessionsSrc
	}
	if m.app == nil {
		return nil
	}

	return m.app
}

// sessionsDBPath reports the path shown in the §I title ("" = not
// configured).
func (m *RootModel) sessionsDBPath() string {
	src := m.sessionsSource()
	if src == nil {
		return ""
	}

	return src.DBPath()
}

// sessionsListLoadedMsg / sessionsDetailLoadedMsg /
// sessionsReviewLoadedMsg are the query results reported from the
// tea.Cmd goroutine; seq marks the load generation (a stale seq is
// ignored — the serverTickSeq lifecycle).
type (
	sessionsListLoadedMsg struct {
		seq   uint64
		views []app.DbSessionView
		err   error
	}
	sessionsDetailLoadedMsg struct {
		seq     uint64
		id      string
		stats   *app.DbSessionStats
		history []app.DbTransactionView
		err     error
	}
	sessionsReviewLoadedMsg struct {
		seq  uint64
		txID int64
		rev  *app.DbTransactionRetrospective
		err  error
	}
)

// armSessions arms one pending §I load while the page is current: the
// session list on entry / after `r` / after a dirtying event, then the
// selected session's detail when stale. Returns nil otherwise (the
// queries never run for an unfocused page).
func (m *RootModel) armSessions() tea.Cmd {
	if m.Current() == nil || m.Current().ID() != pages.SessionsPageID {
		return nil
	}
	if m.sessionsSource() == nil {
		return nil
	}

	switch {
	case !m.sessionsListWait && (!m.sessionsLoaded || m.sessionsDirty):
		m.sessionsListWait, m.sessionsDirty = true, false
		m.sessionsSeq++ // new generation: in-flight detail/review msgs turn stale
		seq := m.sessionsSeq
		src := m.sessionsSource()

		return func() tea.Msg {
			views, err := src.ListSessions(context.Background(), sessionsListLimit)

			return sessionsListLoadedMsg{seq: seq, views: views, err: err}
		}
	case m.sessionsSelected != "" && !m.sessionsDetailWait && m.sessionsDetailStale:
		return m.armSessionsDetail(m.sessionsSelected)
	}

	return nil
}

// armSessionsDetail arms the selected session's stats+history query.
func (m *RootModel) armSessionsDetail(id string) tea.Cmd {
	m.sessionsDetailWait, m.sessionsDetailStale = true, false
	seq := m.sessionsSeq
	src := m.sessionsSource()

	return func() tea.Msg {
		stats, err := src.SessionStats(context.Background(), id)
		history, herr := src.TxHistory(context.Background(), id, sessionsHistoryLimit)
		if err == nil {
			err = herr
		}

		return sessionsDetailLoadedMsg{seq: seq, id: id, stats: stats, history: history, err: err}
	}
}

// handleSessionsSelect selects a session (Enter in the list pane) and
// arms its detail query.
func (m *RootModel) handleSessionsSelect(msg pages.SessionsSelectMsg) (tea.Model, tea.Cmd) {
	m.sessionsSelected = msg.ID
	m.sessionsStats, m.sessionsHistory = nil, nil
	m.sessionsDetailStale = true

	return m, m.armSessions()
}

// handleSessionsReview arms one reconstructed tx review query ([t] /
// Enter on a history row); the overlay opens when the result arrives.
func (m *RootModel) handleSessionsReview(msg pages.SessionsReviewMsg) (tea.Model, tea.Cmd) {
	if m.sessionsReviewWait {
		return m, nil
	}
	m.sessionsReviewWait = true
	m.sessionsReview = nil
	seq := m.sessionsSeq
	txID := msg.TxID
	src := m.sessionsSource()

	return m, func() tea.Msg {
		rev, err := src.ReviewTx(context.Background(), txID)

		return sessionsReviewLoadedMsg{seq: seq, txID: txID, rev: rev, err: err}
	}
}

// handleSessionsRefresh marks the cache dirty (`r`); the Update-wrapper
// arm re-queries on the spot (the page is current by construction).
func (m *RootModel) handleSessionsRefresh() (tea.Model, tea.Cmd) {
	m.sessionsDirty = true
	m.sessionsReview = nil

	return m, m.armSessions()
}

// applySessionsList folds a list result: typed errors become the
// empty-state Note (the list clears, nothing is fabricated); a selection
// that left the list falls to the newest session and its detail. The
// wait flag clears BEFORE the stale check — the uniform pattern of
// applySessionsDetail/applySessionsReview (E5-FIX/M3 audit).
func (m *RootModel) applySessionsList(msg sessionsListLoadedMsg) (tea.Model, tea.Cmd) {
	m.sessionsListWait = false
	if msg.seq != m.sessionsSeq {
		return m, nil
	}
	if msg.err != nil {
		// loaded stays satisfied: the note stands until `r` or the next
		// dirtying event — a missing DB must not re-query in a loop.
		m.sessionsLoaded = true
		m.sessionsNote = sessionsErrorText(msg.err)
		m.sessionsList, m.sessionsSelected = nil, ""
		m.sessionsStats, m.sessionsHistory = nil, nil

		return m, nil
	}
	m.sessionsNote = ""
	m.sessionsLoaded = true
	m.sessionsList = msg.views

	if !m.sessionsListCarries(m.sessionsSelected) {
		m.sessionsSelected = ""
		if len(msg.views) > 0 {
			m.sessionsSelected = msg.views[0].SessionID
		}
		m.sessionsStats, m.sessionsHistory = nil, nil
	}
	// A fresh list means the DB moved (worker finished, `r`, new
	// session): the cached stats/history are stale either way, so the
	// detail re-queries too — the state carries new txs on refresh.
	m.sessionsDetailStale = true

	return m, m.armSessions()
}

// sessionsListCarries reports whether the cached list still has the
// selected session id.
func (m *RootModel) sessionsListCarries(id string) bool {
	if id == "" {
		return false
	}
	for _, s := range m.sessionsList {
		if s.SessionID == id {
			return true
		}
	}

	return false
}

// applySessionsDetail folds a detail result for the still-selected
// session (a stale seq or a switched selection is dropped).
func (m *RootModel) applySessionsDetail(msg sessionsDetailLoadedMsg) (tea.Model, tea.Cmd) {
	m.sessionsDetailWait = false
	if msg.seq != m.sessionsSeq || msg.id != m.sessionsSelected {
		return m, nil
	}
	if msg.err != nil {
		m.sessionsNote = sessionsErrorText(msg.err)
		m.sessionsStats, m.sessionsHistory = nil, nil

		return m, nil
	}
	m.sessionsNote = ""
	m.sessionsStats, m.sessionsHistory = msg.stats, msg.history

	return m, nil
}

// applySessionsReview folds a reconstruction: an error becomes the Note
// (no overlay); a result opens the overlay via SetState identity.
func (m *RootModel) applySessionsReview(msg sessionsReviewLoadedMsg) (tea.Model, tea.Cmd) {
	m.sessionsReviewWait = false
	if msg.seq != m.sessionsSeq {
		return m, nil
	}
	if msg.err != nil {
		m.sessionsNote = sessionsErrorText(msg.err)

		return m, nil
	}
	m.sessionsNote = ""
	m.sessionsReview = msg.rev

	return m, nil
}

// sessionsErrorText renders a façade error as §I empty-state text: the
// two typed DB states name their next action (wireframe), anything else
// stays verbatim.
func sessionsErrorText(err error) string {
	switch {
	case errors.Is(err, app.ErrDBNotConfigured):
		return pages.EmptyTextNoSessionDB
	case errors.Is(err, db.ErrDBNotFound):
		var cfgErr *app.ConfigError
		if errors.As(err, &cfgErr) && cfgErr.Path != "" {
			return "no sessions in " + cfgErr.Path + ". Pass --db to enable logging."
		}

		return "database file does not exist - pass --db to enable session logging"
	default:
		return "session database query failed: " + err.Error()
	}
}
