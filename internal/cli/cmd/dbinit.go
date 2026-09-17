// dbinit.go wires the session database into the cobra tree. The
// golden harness seeded its fixture DB directly, which masked the fact that
// no shipped command ever called db.InitDB: --db recorded nothing and spammed
// "database not initialized" per logged transaction. PersistentPreRunE calls
// ensureSessionDB once per process after the flag > $JISO_* > user-config
// precedence has settled the path, so every writer (send, scenario run,
// stress, the TUI) logs against a live connection.
package cmd

import (
	"github.com/spf13/cobra"

	"jiso/internal/app"
	cfg "jiso/internal/config"
	"jiso/internal/session"
)

// skipSessionDBInitAnnotation marks commands that must never initialize —
// and therefore never create — the session database: the read-only
// review commands (db stats, db tx, ctf list, ctf export) open with
// db.OpenExisting and must keep exiting 3 naming a missing file instead of
// leaving a fresh empty database behind, and the removed/stub commands
// write nothing at all.
const skipSessionDBInitAnnotation = "jiso/skip-session-db-init"

// sessionDBInitSkipped reports whether cmd or an ancestor opted out of the
// session-database seam.
func sessionDBInitSkipped(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[skipSessionDBInitAnnotation] == annotationSet {
			return true
		}
	}

	return false
}

// ensureSessionDB initializes the session DB and records the session row
// for the resolved config. No-ops: no configured path (behavior is exactly
// as before: no file, no stderr noise), a skipped command (see the
// annotation), or --dry-run (which promises to write nothing). An init
// failure is a config-class failure naming the database file (exit 3),
// never a silent no-op.
func ensureSessionDB(cmd *cobra.Command, c *cfg.Config) error {
	if sessionDBInitSkipped(cmd) {
		return nil
	}

	if dryRun, err := cmd.Flags().GetBool("dry-run"); err == nil && dryRun {
		return nil
	}

	dbPath := c.GetDbPath()
	if dbPath == "" {
		return nil
	}

	if err := app.EnsureSessionDB(c); err != nil {
		return &ExitConfigError{Path: dbPath, Err: err}
	}

	// Session row (the §I Sessions screen's anchor row), same upsert the
	// pre-v2 service path wrote after initializing the database.
	_, _ = session.GetManager().StartSession(c.GetSpec(), c.GetFile())

	return nil
}
