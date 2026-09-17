package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/db"
)

// newDbTxCmd builds `jiso db tx <transaction-id>`: the headless
// replacement for the REPL `dbstats tx <id>` review.
func newDbTxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tx <transaction-id>",
		Short: "Show the reconstructed ISO 8583 view of one stored transaction",
		Args:  cobra.ExactArgs(1),
		RunE:  executeDbTxArg,
	}
}

func executeDbTxArg(cmd *cobra.Command, args []string) error {
	out := output.New(cmd)

	txID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || txID <= 0 {
		// A non-numeric or non-positive transaction ID is a usage
		// error, not a runtime failure.
		_, _ = fmt.Fprintf(out.Err(), "Error: invalid transaction ID %q: jiso db tx <id>\n", args[0])

		return &ExitCodeError{Code: ExitUsage}
	}

	dbPath := cfg.GetConfig().GetDbPath()
	if dbPath == "" {
		return dbNotConfiguredError()
	}

	// Read path: open without creating; a missing file exits 3
	// naming the path, it never leaves a fresh database behind.
	if err := db.OpenExisting(dbPath); err != nil {
		return &ExitConfigError{Path: dbPath, Err: err}
	}
	defer func() {
		_ = db.Close()
	}()

	return executeDbTxView(out, txID)
}

// executeDbTxView loads one stored transaction and renders the reconstructed
// retrospective review: hex plus parsed fields (db.Reconstruct runs the same
// utils.Describe path `jiso inspect` prints), timestamps, and result. Shared
// by `db tx <id>` and `db stats tx <id>` (one contract, M1 review #23).
func executeDbTxView(out *output.Renderer, txID int64) error {
	tx, err := db.GetTransactionByID(txID)
	if err != nil {
		// Unknown ID: exit 3 with the ID named, never a fabricated
		// retrospective record.
		return &ExitConfigError{Err: fmt.Errorf("failed to fetch transaction: %w", err)}
	}

	request, _ := db.Reconstruct(tx.RequestJSON, tx.RequestRawHEX, tx.SpecPath)

	var response *db.ReconstructedMessage

	if tx.ResponseJSON != nil || tx.ResponseRawHEX != nil {
		response, _ = db.Reconstruct(derefString(tx.ResponseJSON), derefString(tx.ResponseRawHEX), tx.SpecPath)
	}

	view := app.NewDbStatsViewFromTransaction(tx, request, response)

	return out.Data(view, func() { cmdpkg.PrintTransactionDetail(out.Out(), tx) })
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
