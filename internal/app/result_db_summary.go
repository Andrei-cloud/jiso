package app

import (
	"time"

	"jiso/internal/db"
)

// DbDatabaseSummary is the single JSON-serializable shape behind `db stats`
// with no session (PAR-305): a DB-level summary computed from the stored
// session rows plus the database file on disk. It is derived data over real
// rows only — an empty database yields zero counts and absent timestamps,
// never a fabricated session (E1-FIX #1).
//
// FirstSessionStart is the earliest session start_time; LastSessionActive is
// the latest session last_active_time (start_time as fallback). Both are
// absent (omitted from JSON) when no session is recorded.
type DbDatabaseSummary struct {
	DBPath            string     `json:"db_path"`
	SizeBytes         int64      `json:"size_bytes"`
	SessionCount      int        `json:"session_count"`
	TotalTransactions int        `json:"total_transactions"`
	FirstSessionStart *time.Time `json:"first_session_start,omitempty"`
	LastSessionActive *time.Time `json:"last_session_active,omitempty"`
}

// NewDbDatabaseSummary builds the DB-level summary from the session rows
// returned by db.GetSessionsList plus the stat size of the database file.
// Nil rows are skipped; zero timestamps do not qualify as first/last.
func NewDbDatabaseSummary(sessions []*db.SessionRecord, dbPath string, sizeBytes int64) DbDatabaseSummary {
	summary := DbDatabaseSummary{DBPath: dbPath, SizeBytes: sizeBytes}

	var first, last time.Time

	for _, record := range sessions {
		if record == nil {
			continue
		}

		summary.SessionCount++
		summary.TotalTransactions += record.TransactionCount

		if start := record.StartTime; !start.IsZero() && (first.IsZero() || start.Before(first)) {
			first = start
		}

		end := record.LastActiveTime
		if end.IsZero() {
			end = record.StartTime
		}

		if !end.IsZero() && (last.IsZero() || end.After(last)) {
			last = end
		}
	}

	if !first.IsZero() {
		start := first
		summary.FirstSessionStart = &start
	}

	if !last.IsZero() {
		active := last
		summary.LastSessionActive = &active
	}

	return summary
}
