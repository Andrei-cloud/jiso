package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"jiso/internal/clearing/base2"
	cfg "jiso/internal/config"
	"jiso/internal/db"
)

func newCTFCmd() *cobra.Command {
	ctfCmd := &cobra.Command{
		Use:     "ctf",
		Aliases: []string{"clearing"},
		Short:   "Generate Visa Base II CTF clearing interchange files from recorded sessions",
	}

	ctfCmd.AddCommand(newCTFExportCmd())
	ctfCmd.AddCommand(newCTFListCmd())

	return ctfCmd
}

func newCTFExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export approved Visa transactions from a session into a Base II CTF file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessionID, _ := cmd.Flags().GetString("session-id")
			outputPath, _ := cmd.Flags().GetString("output")
			cib, _ := cmd.Flags().GetString("cib")
			binFilter, _ := cmd.Flags().GetString("bin")
			batchNum, _ := cmd.Flags().GetInt("batch")

			if strings.TrimSpace(sessionID) == "" {
				return errors.New("--session-id is required")
			}

			dbPath := cfg.GetConfig().GetDbPath()
			if dbPath == "" {
				return errors.New("database not configured (use --db flag)")
			}

			if err := db.InitDB(dbPath); err != nil {
				return fmt.Errorf("failed to open database at %s: %w", dbPath, err)
			}
			defer func() {
				_ = db.Close()
			}()

			session, err := db.GetSessionByID(sessionID)
			if err != nil {
				return fmt.Errorf("failed to load session %s: %w", sessionID, err)
			}

			txs, err := db.GetApprovedVisaTransactions(sessionID)
			if err != nil {
				return fmt.Errorf("failed to query approved transactions: %w", err)
			}

			if len(txs) == 0 {
				return fmt.Errorf("no approved transactions found in session %s", sessionID)
			}

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
				return fmt.Errorf("CTF generation failed: %w", err)
			}

			if outputPath == "" {
				dateStr := now.Format("20060102")
				timeStr := now.Format("150405")
				outputPath = fmt.Sprintf("R06_CL%s_AE_VISACTFFile_001O_%s_%s.ctf", cib, dateStr, timeStr)
			}

			cleanPath := filepath.Clean(outputPath)
			if err := os.WriteFile(cleanPath, result.RawContent, 0644); err != nil {
				return fmt.Errorf("failed to write CTF file to %s: %w", cleanPath, err)
			}

			cmd.Printf("\n=========================================================================\n")
			cmd.Printf("VISA BASE II CTF CLEARING FILE EXPORT SUCCESSFUL\n")
			cmd.Printf("=========================================================================\n")
			cmd.Printf("Output File:             %s\n", cleanPath)
			cmd.Printf("File Size:               %d bytes (%d records)\n", len(result.RawContent), len(result.Records))
			cmd.Printf("Monetary Transactions:   %d\n", result.MonetaryTxCount)
			cmd.Printf("Total TCRs in Batch:     %d\n", result.TotalTCRCount)
			cmd.Printf("Destination Amount Sum:  %.2f (Raw: %d)\n", float64(result.DestinationAmountSum)/100.0, result.DestinationAmountSum)
			cmd.Printf("Source Amount Sum:       %.2f (Raw: %d)\n", float64(result.SourceAmountSum)/100.0, result.SourceAmountSum)
			cmd.Printf("Center Info Block (CIB): %s\n", result.CIB)
			cmd.Printf("Processing Date:         %s\n", result.ProcessingDate)
			if result.SkippedCount > 0 {
				cmd.Printf("Skipped Transactions:    %d (non-approved or BIN filtered)\n", result.SkippedCount)
			}
			cmd.Printf("=========================================================================\n")
			return nil
		},
	}

	cmd.Flags().StringP("session-id", "S", "", "Session ID to extract transactions from (required)")
	cmd.Flags().StringP("output", "o", "", "Output CTF file path")
	cmd.Flags().StringP("cib", "c", "400129", "Center Information Block (CIB)")
	cmd.Flags().StringP("bin", "b", "", "Filter approved transactions by card BIN")
	cmd.Flags().IntP("batch", "B", 1, "Batch number")

	return cmd
}

func newCTFListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded sessions with Visa transactions eligible for CTF export",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dbPath := cfg.GetConfig().GetDbPath()
			if dbPath == "" {
				return errors.New("database not configured (use --db flag)")
			}

			if err := db.InitDB(dbPath); err != nil {
				return fmt.Errorf("failed to open database at %s: %w", dbPath, err)
			}
			defer func() {
				_ = db.Close()
			}()

			sessions, err := db.GetVisaSessions()
			if err != nil {
				return fmt.Errorf("failed to fetch Visa sessions: %w", err)
			}

			if len(sessions) == 0 {
				cmd.Println("No recorded sessions with Visa transactions found.")
				return nil
			}

			cmd.Printf("\n%-36s | %-19s | %-12s | %-10s | %-10s\n", "Session ID", "Start Time", "Spec", "Total Tx", "Approved Tx")
			cmd.Println(strings.Repeat("-", 97))

			for _, s := range sessions {
				spec := s.SpecName
				if spec == "" {
					spec = "-"
				}
				cmd.Printf("%-36s | %-19s | %-12s | %-10d | %-10d\n",
					s.SessionID,
					s.StartTime.Format("2006-01-02 15:04:05"),
					truncateCLIString(spec, 12),
					s.TransactionCount,
					s.SuccessCount,
				)
			}
			cmd.Println()
			return nil
		},
	}

	return cmd
}

func truncateCLIString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
