package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"jiso/internal/clearing/base2"
	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/db"
)

func newCTFCmd() *cobra.Command {
	ctfCmd := &cobra.Command{
		Use:         "ctf",
		Aliases:     []string{"clearing"},
		Short:       "Generate Visa Base II CTF clearing interchange files from recorded sessions",
		Annotations: map[string]string{skipSessionDBInitAnnotation: annotationSet},
	}

	ctfCmd.AddCommand(newCTFExportCmd())
	ctfCmd.AddCommand(newCTFListCmd())

	return ctfCmd
}

// ctfExportSummary is the machine-readable result of `ctf export`: emitted
// as pure JSON under --json and rendered as the human banner otherwise;
// --dry-run returns the same summary with dry_run=true and written=false
// without touching the filesystem (M1 review #3/#14).
type ctfExportSummary struct {
	SessionID            string `json:"session_id"`
	OutputPath           string `json:"output_path"`
	DryRun               bool   `json:"dry_run,omitempty"`
	Written              bool   `json:"written"`
	FileSizeBytes        int    `json:"file_size_bytes"`
	Records              int    `json:"records"`
	MonetaryTransactions int    `json:"monetary_transactions"`
	TotalTCRs            int    `json:"total_tcrs"`
	DestinationAmountSum int64  `json:"destination_amount_sum_raw"`
	SourceAmountSum      int64  `json:"source_amount_sum_raw"`
	CIB                  string `json:"cib"`
	ProcessingDate       string `json:"processing_date"`
	SkippedTransactions  int    `json:"skipped_transactions,omitempty"`
	ApprovedTransactions int    `json:"approved_transactions"`
}

func summaryFromCTF(sessionID, outputPath string, result *base2.CTFResult, approved int, dryRun, written bool) *ctfExportSummary {
	return &ctfExportSummary{
		SessionID:            sessionID,
		OutputPath:           outputPath,
		DryRun:               dryRun,
		Written:              written,
		FileSizeBytes:        len(result.RawContent),
		Records:              len(result.Records),
		MonetaryTransactions: result.MonetaryTxCount,
		TotalTCRs:            result.TotalTCRCount,
		DestinationAmountSum: result.DestinationAmountSum,
		SourceAmountSum:      result.SourceAmountSum,
		CIB:                  result.CIB,
		ProcessingDate:       result.ProcessingDate,
		SkippedTransactions:  result.SkippedCount,
		ApprovedTransactions: approved,
	}
}

func printCTFExportBanner(w, notice io.Writer, s *ctfExportSummary) {
	title := "VISA BASE II CTF CLEARING FILE EXPORT SUCCESSFUL\n"
	if s.DryRun {
		title = "VISA BASE II CTF CLEARING FILE EXPORT PLAN (dry-run: nothing written)\n"
	}

	_, _ = fmt.Fprintf(w, "\n=========================================================================\n")
	_, _ = fmt.Fprint(w, title)
	_, _ = fmt.Fprintf(w, "=========================================================================\n")
	_, _ = fmt.Fprintf(w, "Session ID:              %s\n", s.SessionID)
	_, _ = fmt.Fprintf(w, "Messages to Export:      %d approved transactions\n", s.ApprovedTransactions)
	_, _ = fmt.Fprintf(w, "Output File:             %s\n", s.OutputPath)
	if s.DryRun {
		_, _ = fmt.Fprintf(w, "Planned File Size:       %d bytes (%d records)\n", s.FileSizeBytes, s.Records)
	} else {
		_, _ = fmt.Fprintf(w, "File Size:               %d bytes (%d records)\n", s.FileSizeBytes, s.Records)
	}
	_, _ = fmt.Fprintf(w, "Monetary Transactions:   %d\n", s.MonetaryTransactions)
	_, _ = fmt.Fprintf(w, "Total TCRs in Batch:     %d\n", s.TotalTCRs)
	_, _ = fmt.Fprintf(w, "Destination Amount Sum:  %.2f (Raw: %d)\n", float64(s.DestinationAmountSum)/100.0, s.DestinationAmountSum)
	_, _ = fmt.Fprintf(w, "Source Amount Sum:       %.2f (Raw: %d)\n", float64(s.SourceAmountSum)/100.0, s.SourceAmountSum)
	_, _ = fmt.Fprintf(w, "Clearing Interchange BIN (CIB): %s\n", s.CIB)
	_, _ = fmt.Fprintf(w, "Processing Date:                %s\n", s.ProcessingDate)
	if s.SkippedTransactions > 0 {
		_, _ = fmt.Fprintf(w, "Skipped Transactions:           %d (non-approved or BIN filtered)\n", s.SkippedTransactions)
	}
	_, _ = fmt.Fprintf(w, "=========================================================================\n")

	if notice != nil {
		if s.DryRun {
			_, _ = fmt.Fprintf(notice, "dry-run: CTF file %s was NOT written\n", s.OutputPath)
		} else {
			_, _ = fmt.Fprintf(notice, "CTF file written to: %s\n", s.OutputPath)
		}
	}
}

// ctfSessionID resolves the session to export: --session is the headless
// contract spelling (PAR-308) and wins when both it and the legacy
// --session-id are given; an explicitly empty value is the usage class.
func ctfSessionID(cmd *cobra.Command) string {
	for _, name := range []string{"session", "session-id"} {
		f := cmd.Flags().Lookup(name)
		if f == nil || !f.Changed {
			continue
		}

		v, _ := cmd.Flags().GetString(name)

		return strings.TrimSpace(v)
	}

	return ""
}

func newCTFExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export approved Visa transactions from a session into a Base II CTF file",
		Long: "Export approved transactions from a recorded session into a Visa Base II\n" +
			"CTF clearing file.\n\n" +
			"Headless contract (PAR-308): with --session (or the legacy --session-id)\n" +
			"given this command never prompts, so --yes is accepted as a no-op\n" +
			"confirmation — there is no prompt to confirm away. Missing --session is\n" +
			"a usage error (exit 2); an unknown session exits 3; --dry-run prints the\n" +
			"export plan and writes nothing.",
		RunE: runCTFExport,
	}

	cmd.Flags().String("session", "", "Session ID to extract transactions from (required; wins over --session-id)")
	cmd.Flags().String("session-id", "", "Legacy alias of --session")
	cmd.Flags().StringP("output", "o", "", "Output CTF file path")
	cmd.Flags().StringP("cib", "c", "400129", "Clearing Interchange BIN (CIB)")
	cmd.Flags().StringP("bin", "b", "", "Filter approved transactions by card BIN")
	cmd.Flags().IntP("batch", "B", 1, "Batch number")
	cmd.Flags().Bool("yes", false, "No-op confirmation: this command is non-interactive and never prompts")

	// A missing --session/--session-id is a usage error (exit 2) reported by
	// RunE, not MarkFlagRequired, so either spelling satisfies the
	// requirement (M1 review #4; PAR-308).

	return cmd
}

// runCTFExport is the ctf export RunE: it validates the session argument, loads the
// session and its approved transactions, generates the CTF and writes (or dry-run
// plans) the output.
func runCTFExport(cmd *cobra.Command, _ []string) error {
	out := output.New(cmd)

	sessionID := ctfSessionID(cmd)
	outputPath, _ := cmd.Flags().GetString("output")
	cib, _ := cmd.Flags().GetString("cib")
	binFilter, _ := cmd.Flags().GetString("bin")
	batchNum, _ := cmd.Flags().GetInt("batch")

	if sessionID == "" {
		// Absence and an explicitly empty value are the same usage
		// class (exit 2, M1 review #4; PAR-308).
		msg := "--session is required (legacy alias: --session-id)"
		_, _ = fmt.Fprintf(out.Err(), "Error: %s\n", msg)

		return &ExitCodeError{Code: ExitUsage}
	}

	dbPath := cfg.GetConfig().GetDbPath()
	if dbPath == "" {
		return errors.New("database not configured (use --db flag)")
	}

	// Read path (PAR-311): open without creating; a missing file
	// exits 3 naming the path, it never leaves a fresh database
	// behind.
	if err := db.OpenExisting(dbPath); err != nil {
		return &ExitConfigError{Path: dbPath, Err: err}
	}
	defer func() {
		_ = db.Close()
	}()

	session, txs, err := loadCTFSessionAndTxs(sessionID)
	if err != nil {
		return err
	}

	result, cleanPath, err := generateCTFFile(session, txs, outputPath, cib, binFilter, batchNum)
	if err != nil {
		return err
	}

	// --dry-run prints the plan and writes nothing (M1 review #3).
	if out.DryRun() {
		plan := summaryFromCTF(sessionID, cleanPath, result, len(txs), true, false)

		return out.Data(plan, func() { printCTFExportBanner(out.Out(), noticeWriter(out), plan) })
	}

	if err := os.WriteFile(cleanPath, result.RawContent, 0o644); err != nil {
		return fmt.Errorf("failed to write CTF file to %s: %w", cleanPath, err)
	}

	summary := summaryFromCTF(sessionID, cleanPath, result, len(txs), false, true)

	return out.Data(summary, func() { printCTFExportBanner(out.Out(), noticeWriter(out), summary) })
}

// loadCTFSessionAndTxs loads a session and its approved Visa transactions, mapping
// the unknown-session case to a config-class error naming the id.
func loadCTFSessionAndTxs(sessionID string) (*db.SessionRecord, []*db.EnrichedTransactionRecord, error) {
	session, err := db.GetSessionByID(sessionID)
	if err != nil {
		// An unknown session is a config-class failure naming the
		// id (exit 3, PAR-308); other load errors stay exit 1.
		if errors.Is(err, db.ErrSessionNotFound) {
			return nil, nil, &ExitConfigError{Path: sessionID, Err: errors.New("unknown session")}
		}

		return nil, nil, fmt.Errorf("failed to load session %s: %w", sessionID, err)
	}

	txs, err := db.GetApprovedVisaTransactions(sessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query approved transactions: %w", err)
	}

	if len(txs) == 0 {
		return nil, nil, fmt.Errorf("no approved transactions found in session %s", sessionID)
	}

	return session, txs, nil
}

// generateCTFFile builds the generator options, generates the CTF and resolves the
// output path (defaulting to a timestamped name when none was given), returning the
// result and its cleaned path.
func generateCTFFile(session *db.SessionRecord, txs []*db.EnrichedTransactionRecord, outputPath, cib, binFilter string, batchNum int) (*base2.CTFResult, string, error) {
	now := time.Now().UTC()
	if cib == "" {
		cib = "400129"
	}
	if batchNum <= 0 {
		batchNum = 1
	}

	opts := base2.GeneratorOptions{
		BINFilter:      strings.TrimSpace(binFilter),
		CIB:            cib,
		BatchNumber:    batchNum,
		GenerationTime: now,
	}

	result, err := base2.GenerateCTF(session, txs, opts)
	if err != nil {
		return nil, "", fmt.Errorf("CTF generation failed: %w", err)
	}

	if outputPath == "" {
		dateStr := now.Format("20060102")
		timeStr := now.Format("150405")
		outputPath = fmt.Sprintf("R06_CL%s_AE_VISACTFFile_001O_%s_%s.ctf", cib, dateStr, timeStr)
	}

	return result, filepath.Clean(outputPath), nil
}

// noticeWriter returns the stdout writer for notices, or nil when notices are
// suppressed (--quiet/--json).
func noticeWriter(out *output.Renderer) io.Writer {
	if out.Quiet() || out.JSON() {
		return nil
	}

	return out.Out()
}

func newCTFListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   subCmdList,
		Short: "List recorded sessions with Visa transactions eligible for CTF export",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.New(cmd)

			dbPath := cfg.GetConfig().GetDbPath()
			if dbPath == "" {
				return errors.New("database not configured (use --db flag)")
			}

			// Read path (PAR-311): open without creating; a missing file
			// exits 3 naming the path, it never leaves a fresh database
			// behind.
			if err := db.OpenExisting(dbPath); err != nil {
				return &ExitConfigError{Path: dbPath, Err: err}
			}
			defer func() {
				_ = db.Close()
			}()

			sessions, err := db.GetVisaSessions()
			if err != nil {
				return fmt.Errorf("failed to fetch Visa sessions: %w", err)
			}
			if sessions == nil {
				sessions = []*db.SessionRecord{}
			}

			return out.Data(sessions, func() { cmdpkg.PrintVisaSessionsList(out.Out(), sessions) })
		},
	}

	return cmd
}
