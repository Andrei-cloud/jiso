package command

import (
	"fmt"
	"io"
	"time"

	"jiso/internal/app"
)

// PrintDbSummary renders the DB-level summary of `jiso db stats` with no
// session, ported from the REPL dbstats overview sections into a
// headless table in the shared db_views.go style. The JSON surface is
// app.DbDatabaseSummary; this is its human printer.
func PrintDbSummary(w io.Writer, v *app.DbDatabaseSummary) {
	if v == nil {
		return
	}

	_, _ = fmt.Fprintln(w, "\nDatabase Summary")
	_, _ = fmt.Fprintln(w, "==================")
	_, _ = fmt.Fprintf(w, "DB Path:            %s\n", v.DBPath)
	_, _ = fmt.Fprintf(w, "Size on Disk:       %s (%d bytes)\n", formatSizeBytes(v.SizeBytes), v.SizeBytes)
	_, _ = fmt.Fprintf(w, "Sessions:           %d\n", v.SessionCount)
	_, _ = fmt.Fprintf(w, "Total Transactions: %d\n", v.TotalTransactions)
	_, _ = fmt.Fprintf(w, "First Session:      %s\n", formatOptionalTimestamp(v.FirstSessionStart))
	_, _ = fmt.Fprintf(w, "Last Session:       %s\n", formatOptionalTimestamp(v.LastSessionActive))
	_, _ = fmt.Fprintln(w)
}

// formatOptionalTimestamp renders a possibly-absent timestamp; "-" keeps
// the table honest when the database holds no sessions.
func formatOptionalTimestamp(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}

	return t.Format("2006-01-02 15:04:05")
}

// formatSizeBytes renders a byte count as a short binary-unit string.
func formatSizeBytes(bytes int64) string {
	const unit = 1024

	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	value := float64(bytes)
	units := []string{"KiB", "MiB", "GiB", "TiB"}

	for _, unitName := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, unitName)
		}
	}

	return fmt.Sprintf("%.1f PiB", value/unit)
}
