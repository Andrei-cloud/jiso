package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	"jiso/internal/cli/userconfig"
	cmdpkg "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/db"
)

func newDbCmd() *cobra.Command {
	dbCmd := &cobra.Command{
		Use:         "db",
		Short:       "Session database review",
		Annotations: map[string]string{skipSessionDBInitAnnotation: annotationSet},
	}

	dbCmd.AddCommand(newDbStatsCmd())
	dbCmd.AddCommand(newDbTxCmd())

	return dbCmd
}

// dbStatsView is the JSON+human shape of `jiso db stats --session <id>`:
// the shared SessionOverview renderer lives in internal/command and is used
// by both this surface and the REPL `dbstats` view.
type dbStatsView = cmdpkg.SessionOverview

func newDbStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats [session-id|list|tx <id>] [--session <id>]",
		Short: "Show database or session statistics and retrospective ISO 8583 message logs",
		Args:  cobra.MaximumNArgs(2),
		RunE:  executeDbStats,
	}

	cmd.Flags().String("session", "", "Session ID for the per-session statistics view")

	return cmd
}

// dbStatsArgs is the parsed headless `db stats` argument form: the subcommand
// asked for and the session ID or transaction ID it names.
type dbStatsArgs struct {
	sub       string
	sessionID string
	txID      int64
}

func parseDbStatsArgs(args []string) (dbStatsArgs, error) {
	if len(args) == 0 {
		return dbStatsArgs{}, nil
	}

	switch args[0] {
	case subCmdList:
		return dbStatsArgs{sub: subCmdList}, nil
	case "tx":
		raw := valueAt(args, 1)
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || id <= 0 {
			// A non-numeric or non-positive transaction ID is a usage
			// error, not a runtime failure.
			return dbStatsArgs{sub: "tx"}, fmt.Errorf("invalid transaction ID %q: jiso db stats tx <id>", raw)
		}

		return dbStatsArgs{sub: "tx", txID: id}, nil
	default:
		return dbStatsArgs{sessionID: args[0]}, nil
	}
}

func valueAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}

	return ""
}

// executeDbStats dispatches the headless `db stats` surface:
// list / tx <id> / --session (or positional session ID) overview, and with
// no session a DB-level summary. The interactive review menu stays REPL-only
// (`dbstats`); this cobra path never prompts.
func executeDbStats(cmd *cobra.Command, args []string) error {
	out := output.New(cmd)

	dbPath := cfg.GetConfig().GetDbPath()
	if dbPath == "" {
		return dbNotConfiguredError()
	}

	parsed, argErr := parseDbStatsArgs(args)
	if argErr != nil {
		_, _ = fmt.Fprintf(out.Err(), "Error: %s\n", argErr)

		return &ExitCodeError{Code: ExitUsage}
	}

	if cmd.Flags().Changed("session") {
		flagSession, _ := cmd.Flags().GetString("session")
		flagSession = strings.TrimSpace(flagSession)

		if flagSession == "" {
			_, _ = fmt.Fprintf(out.Err(), "Error: --session requires a session ID: jiso db stats --session <id>\n")

			return &ExitCodeError{Code: ExitUsage}
		}

		if parsed.sessionID != "" || parsed.sub != "" {
			_, _ = fmt.Fprintf(out.Err(), "Error: --session cannot be combined with the %q argument form\n", strings.Join(args, " "))

			return &ExitCodeError{Code: ExitUsage}
		}

		parsed.sessionID = flagSession
	}

	// Read path: open without creating; a missing file exits 3
	// naming the path, it never leaves a fresh database behind.
	if err := db.OpenExisting(dbPath); err != nil {
		return &ExitConfigError{Path: dbPath, Err: err}
	}
	defer func() {
		_ = db.Close()
	}()

	switch {
	case parsed.sub == subCmdList:
		sessions, err := db.GetSessionsList()
		if err != nil {
			return &ExitConfigError{Err: fmt.Errorf("failed to fetch sessions: %w", err)}
		}
		if sessions == nil {
			sessions = []*db.SessionRecord{}
		}

		return out.Data(sessions, func() { cmdpkg.PrintSessionsList(out.Out(), sessions) })

	case parsed.sub == "tx":
		return executeDbTxView(out, parsed.txID)

	case parsed.sessionID != "":
		return dbStatsSessionOverview(out, parsed.sessionID)

	default:
		return dbStatsDatabaseSummary(out, dbPath)
	}
}

// dbNotConfiguredError fails with exit 3 naming every source a database
// path can come from. The user config path is named when known.
func dbNotConfiguredError() error {
	_, ucPath, _ := userconfig.Load()

	source := "--db flag, $JISO_DB, or the user config file"
	if ucPath != "" {
		source = fmt.Sprintf("--db flag, $JISO_DB, or db in %s", ucPath)
	}

	return &ExitConfigError{Err: fmt.Errorf("database not configured: set %s", source)}
}

// dbStatsDatabaseSummary is `db stats` with no session: the
// DB-level summary over real rows only. This replaces the M1-review-#1
// "session ID is required with --json" guard: that guard existed because the
// only candidate was EnsureSessionID's fresh UUID, i.e. a fabricated
// session; a summary over the stored session rows is real data, so
// `db stats --json` now prints it with exit 0.
func dbStatsDatabaseSummary(out *output.Renderer, dbPath string) error {
	sessions, err := db.GetSessionsList()
	if err != nil {
		return &ExitConfigError{Err: fmt.Errorf("failed to fetch sessions: %w", err)}
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		return &ExitConfigError{Path: dbPath, Err: fmt.Errorf("failed to stat database file: %w", err)}
	}

	summary := app.NewDbDatabaseSummary(sessions, dbPath, info.Size())

	return out.Data(&summary, func() { cmdpkg.PrintDbSummary(out.Out(), &summary) })
}

func dbStatsSessionOverview(out *output.Renderer, sessionID string) error {
	rec, err := db.GetSessionByID(sessionID)
	if err != nil {
		// A missing/unknown session is a load failure naming the ID
		// Exit 3, never a fabricated record.
		return &ExitConfigError{Err: fmt.Errorf("failed to get session info: %w", err)}
	}

	stats, err := db.GetTransactionStats(sessionID)
	if err != nil {
		return &ExitConfigError{Err: fmt.Errorf("failed to get database stats: %w", err)}
	}

	// Auxiliary queries must not silently drop sections under --json
	// A failing query fails the command.
	stress, err := db.GetSessionStressTestSummaries(sessionID)
	if err != nil {
		return &ExitConfigError{Err: fmt.Errorf("failed to get stress test summaries: %w", err)}
	}

	txs, err := db.GetSessionTransactions(sessionID)
	if err != nil {
		return &ExitConfigError{Err: fmt.Errorf("failed to get session transactions: %w", err)}
	}

	view := &dbStatsView{Session: rec, Stats: stats, StressTests: stress, Transactions: txs}

	// The "inspect a transaction" hint is a notice: suppressed under
	// --quiet and absent under --json (the human printer never runs there).
	hint := out.Out()
	if out.Quiet() {
		hint = nil
	}

	return out.Data(view, func() { view.Print(out.Out(), hint) })
}
