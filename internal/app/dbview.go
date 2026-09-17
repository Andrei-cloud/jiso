// dbview.go is the §I read-only session-DB façade. The TUI
// never touches internal/db itself: root queries these *App methods off
// the UI thread (tea.Cmd) and derives display strings from the returned
// views with its injectable clock — the returned views carry stored
// timestamps only, never relative times. Every path opens through
// db.OpenExisting: a missing file is the typed ConfigError
// below (errors.Is db.ErrDBNotFound), never a created file and never a
// crash; an unset --db path is ErrDBNotConfigured. Queries are read-only
// (SELECTs plus the schema-ensure OpenExisting already does) and
// serialised by App.dbReadMu because the db package keeps one
// process-wide connection.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"jiso/internal/db"
)

// ErrDBNotConfigured reports an App whose database path was never set
// (--db / $JISO_DB / user config): frontends render it as the §I
// empty-state text, never as a failure.
var ErrDBNotConfigured = errors.New("database not configured: pass --db to enable session logging")

// dbReadErr resolves the §I read path for one query: the configured path
// or a typed error (ErrDBNotConfigured when unset, a *ConfigError
// wrapping db.ErrDBNotFound when the file is missing).
func (a *App) dbReadErr() error {
	if a == nil || a.cfg == nil || a.cfg.GetDbPath() == "" {
		return ErrDBNotConfigured
	}

	return nil
}

// DBPath reports the configured session-database path ("" when unset);
// the §I title shows it, the façade keys every read off it.
func (a *App) DBPath() string {
	if a == nil || a.cfg == nil {
		return ""
	}

	return a.cfg.GetDbPath()
}

// checkCtx reports a cancelled caller context (nil ctx = no cancellation
// contract).
func checkCtx(ctx context.Context) error {
	if ctx == nil {
		return nil
	}

	return ctx.Err()
}

// openRead serialises the query and opens the existing DB (never
// creates). The returned func closeRead must be deferred by the caller.
func (a *App) openRead(ctx context.Context) (func(), error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if err := a.dbReadErr(); err != nil {
		return nil, err
	}
	a.dbReadMu.Lock()
	// When the process already holds the write connection for this
	// very file (the session-DB seam ran at startup, as in the TUI), reuse
	// it instead of OpenExisting/Close churn, which would re-open a second
	// handle per refresh and then closeRead the global out from under the
	// async logger. The semantics are unchanged for the standalone
	// read path: no global conn for this path → OpenExisting as before,
	// missing file → typed ErrDBNotFound, never a created file.
	if db.IsInitialized() && db.CurrentPath() == a.cfg.GetDbPath() {
		return func() { a.dbReadMu.Unlock() }, nil
	}

	if err := db.OpenExisting(a.cfg.GetDbPath()); err != nil {
		a.dbReadMu.Unlock()
		if errors.Is(err, db.ErrDBNotFound) {
			return nil, &ConfigError{Path: a.cfg.GetDbPath(), Err: err}
		}

		return nil, err
	}

	return func() { _ = db.Close(); a.dbReadMu.Unlock() }, nil
}

// ListSessions returns the recorded sessions newest-activity-first (the
// db's start_time DESC order, preserved), at most limit rows
// (limit <= 0 = all). No clock, no relative times: When-labels are
// derived root-side from StartTime/LastActiveTime.
func (a *App) ListSessions(ctx context.Context, limit int) ([]DbSessionView, error) {
	closeRead, err := a.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()

	records, err := db.GetSessionsList()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sessions: %w", err)
	}

	views := make([]DbSessionView, 0, len(records))
	for _, rec := range records {
		if rec == nil {
			continue
		}
		if limit > 0 && len(views) >= limit {
			break
		}
		views = append(views, NewDbSessionViewFromRecord(rec))
	}

	return views, nil
}

// SessionStats returns the selected session's counters (total/ok/fail/
// avg + the RC distribution map) via db.GetTransactionStats. A missing
// DB surfaces the same typed errors as ListSessions; a session absent
// from the database returns db.ErrSessionNotFound (errors.Is) instead of
// the zero-counts map the stats query would otherwise fabricate.
func (a *App) SessionStats(ctx context.Context, sessionID string) (*DbSessionStats, error) {
	closeRead, err := a.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()

	if _, err := db.GetSessionByID(sessionID); err != nil {
		return nil, fmt.Errorf("failed to get session stats: %w", err)
	}

	stats, err := db.GetTransactionStats(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session stats: %w", err)
	}

	view := NewDbSessionStatsFromStatsMap(stats)

	return &view, nil
}

// TxHistory returns the session's transactions, most recent first (the
// db returns id ASC; this reverses and keeps the newest limit rows,
// limit <= 0 = all). MTI is parsed out of the stored request JSON;
// everything else is the shared DbTransactionView shape.
func (a *App) TxHistory(ctx context.Context, sessionID string, limit int) ([]DbTransactionView, error) {
	closeRead, err := a.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()

	records, err := db.GetSessionTransactions(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transactions: %w", err)
	}

	views := make([]DbTransactionView, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec == nil {
			continue
		}
		if limit > 0 && len(views) >= limit {
			break
		}
		view := NewDbTransactionViewFromRecord(rec)
		view.MTI = deriveMTI(rec.RequestJSON)
		views = append(views, view)
	}

	return views, nil
}

// ReviewTx reconstructs one stored transaction for the §I review
// (the same capability as `jiso db tx <id>`: db.Reconstruct + describe
// over the stored JSON with the raw-HEX fallback, spec from the stored
// spec path). The returned retrospective carries HEX/DescribeText
// strings; a row id absent from the DB is an error.
func (a *App) ReviewTx(ctx context.Context, txRowID int64) (*DbTransactionRetrospective, error) {
	closeRead, err := a.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()

	record, err := db.GetTransactionByID(txRowID)
	if err != nil {
		if errors.Is(err, db.ErrTransactionNotFound) {
			return nil, fmt.Errorf("failed to get transaction %d: %w", txRowID, ErrTxNotFound)
		}

		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	request, err := db.Reconstruct(record.RequestJSON, record.RequestRawHEX, record.SpecPath)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct request: %w", err)
	}

	var response *db.ReconstructedMessage
	if record.ResponseJSON != nil || record.ResponseRawHEX != nil {
		var respJSON string
		if record.ResponseJSON != nil {
			respJSON = *record.ResponseJSON
		}
		var respHEX string
		if record.ResponseRawHEX != nil {
			respHEX = *record.ResponseRawHEX
		}
		response, err = db.Reconstruct(respJSON, respHEX, record.SpecPath)
		if err != nil {
			return nil, fmt.Errorf("failed to reconstruct response: %w", err)
		}
	}

	view := NewDbStatsViewFromTransaction(record, request, response)

	return view.Transaction, nil
}

// mtiProbe reads just the mti member of a stored message JSON.
type mtiProbe struct {
	MTI string `json:"mti"`
}

// deriveMTI parses the MTI out of a stored request JSON ("" when the
// JSON is absent or carries no mti; never an error — the cell dashes).
func deriveMTI(requestJSON string) string {
	if strings.TrimSpace(requestJSON) == "" {
		return ""
	}
	var probe mtiProbe
	if err := json.Unmarshal([]byte(requestJSON), &probe); err != nil {
		return ""
	}

	return probe.MTI
}

// FormatRelativeTime renders t against now the way the §I list shows it:
// same day "today 12:01", previous day "yest 17:30", older "09-07
// 17:30". Exported for the root bridge (and its tests); the page itself
// only ever sees the finished string.
func FormatRelativeTime(now, t time.Time) string {
	if sameInstantDay(now, t) {
		return "today " + t.Format("15:04")
	}
	if sameInstantDay(now.AddDate(0, 0, -1), t) {
		return "yest " + t.Format("15:04")
	}

	return t.Format("01-02 15:04")
}

// sameInstantDay reports whether now and t fall on the same calendar
// day (UTC-normalised: stored DATETIMEs parse back as UTC).
func sameInstantDay(now, t time.Time) bool {
	n, tt := now.UTC(), t.UTC()

	return n.Year() == tt.Year() && n.YearDay() == tt.YearDay()
}
