package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"

	"jiso/internal/clearing/base2"
	"jiso/internal/config"
	"jiso/internal/db"
)

// CTFCommand handles Base II CTF clearing file generation in REPL.
type CTFCommand struct {
	SubCommand string
	SessionID  string
	OutputPath string
	BINFilter  string
	CIB        string
}

// Name returns the command name.
func (c *CTFCommand) Name() string {
	return "ctf"
}

// Synopsis returns a short description.
func (c *CTFCommand) Synopsis() string {
	return "Export approved Visa transactions from a recorded session into a Base II CTF clearing file."
}

// Reset clears command arguments.
func (c *CTFCommand) Reset() {
	c.SubCommand = ""
	c.SessionID = ""
	c.OutputPath = ""
	c.BINFilter = ""
	c.CIB = ""
}

// SetArgs parses CLI arguments for the ctf command.
func (c *CTFCommand) SetArgs(args []string) {
	c.Reset()
	if len(args) == 0 {
		return
	}

	if args[0] == "list" {
		c.SubCommand = "list"
		return
	}

	if args[0] == "export" {
		c.SubCommand = "export"
		if len(args) > 1 {
			c.SessionID = args[1]
		}
		if len(args) > 2 {
			c.OutputPath = args[2]
		}
		if len(args) > 3 {
			c.BINFilter = args[3]
		}
		return
	}

	// Shorthand: ctf <session_id> [output_path] [bin]
	c.SessionID = args[0]
	if len(args) > 1 {
		c.OutputPath = args[1]
	}
	if len(args) > 2 {
		c.BINFilter = args[2]
	}
}

// Execute runs the CTF command.
func (c *CTFCommand) Execute() error {
	defer c.Reset()

	dbPath := config.GetConfig().GetDbPath()
	if dbPath == "" {
		return errors.New("database not configured (use --db flag)")
	}

	if c.SubCommand == "list" {
		return printVisaSessionsList()
	}

	if c.SessionID != "" {
		return exportCTFForSession(c.SessionID, c.OutputPath, c.BINFilter, c.CIB)
	}

	// Interactive Mode
	return runInteractiveCTF()
}

func printVisaSessionsList() error {
	sessions, err := db.GetVisaSessions()
	if err != nil {
		return fmt.Errorf("failed to fetch Visa sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No recorded sessions with Visa transactions found.")
		return nil
	}

	fmt.Printf("\n%-36s | %-19s | %-12s | %-10s | %-10s\n", "Session ID", "Start Time", "Spec", "Total Tx", "Approved Tx")
	fmt.Println(strings.Repeat("-", 97))

	for _, s := range sessions {
		spec := s.SpecName
		if spec == "" {
			spec = "-"
		}
		fmt.Printf("%-36s | %-19s | %-12s | %-10d | %-10d\n",
			s.SessionID,
			s.StartTime.Format("2006-01-02 15:04:05"),
			truncateString(spec, 12),
			s.TransactionCount,
			s.SuccessCount,
		)
	}
	fmt.Println()
	return nil
}

func exportCTFForSession(sessionID, outputPath, binFilter, cib string) error {
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

	opts := base2.GeneratorOptions{
		BINFilter:      binFilter,
		CIB:            cib,
		BatchNumber:    1,
		GenerationTime: now,
	}

	result, err := base2.GenerateCTF(session, txs, opts)
	if err != nil {
		return fmt.Errorf("CTF generation failed: %w", err)
	}

	if outputPath == "" {
		// Visa standard format: R06_CL<CIB>_AE_VISACTFFile_001O_<Date>_<Time>.ctf
		dateStr := now.Format("20060102")
		timeStr := now.Format("150405")
		outputPath = fmt.Sprintf("R06_CL%s_AE_VISACTFFile_001O_%s_%s.ctf", cib, dateStr, timeStr)
	}

	cleanPath := filepath.Clean(outputPath)
	if err := os.WriteFile(cleanPath, result.RawContent, 0644); err != nil {
		return fmt.Errorf("failed to write CTF file to %s: %w", cleanPath, err)
	}

	fmt.Printf("\n=========================================================================\n")
	fmt.Printf("VISA BASE II CTF CLEARING FILE EXPORT SUCCESSFUL\n")
	fmt.Printf("=========================================================================\n")
	fmt.Printf("Output File:             %s\n", cleanPath)
	fmt.Printf("File Size:               %d bytes (%d records)\n", len(result.RawContent), len(result.Records))
	fmt.Printf("Monetary Transactions:   %d\n", result.MonetaryTxCount)
	fmt.Printf("Total TCRs in Batch:     %d\n", result.TotalTCRCount)
	fmt.Printf("Destination Amount Sum:  %.2f (Raw: %d)\n", float64(result.DestinationAmountSum)/100.0, result.DestinationAmountSum)
	fmt.Printf("Source Amount Sum:       %.2f (Raw: %d)\n", float64(result.SourceAmountSum)/100.0, result.SourceAmountSum)
	fmt.Printf("Clearing Interchange BIN (CIB): %s\n", result.CIB)
	fmt.Printf("Processing Date:                %s\n", result.ProcessingDate)
	if result.SkippedCount > 0 {
		fmt.Printf("Skipped Transactions:           %d (non-approved or BIN filtered)\n", result.SkippedCount)
	}
	fmt.Println("=========================================================================")
	return nil
}

func runInteractiveCTF() error {
	sessions, err := db.GetVisaSessions()
	if err != nil {
		return fmt.Errorf("failed to query sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions with Visa transactions found.")
		return nil
	}

	options := make([]string, len(sessions))
	for i, s := range sessions {
		options[i] = fmt.Sprintf("%s | %s | Approved: %d/%d",
			s.SessionID,
			s.StartTime.Format("2006-01-02 15:04:05"),
			s.SuccessCount,
			s.TransactionCount,
		)
	}

	var selected string
	prompt := &survey.Select{
		Message: "Select Visa Session for CTF Export:",
		Options: options,
	}
	if err := survey.AskOne(prompt, &selected); err != nil {
		return nil
	}

	sessionID := strings.Split(selected, " | ")[0]

	var binFilter string
	binPrompt := &survey.Input{
		Message: "Filter by Card BIN (leave blank for all approved transactions):",
	}
	if err := survey.AskOne(binPrompt, &binFilter); err != nil {
		return nil
	}

	now := time.Now().UTC()
	defaultFile := fmt.Sprintf("R06_CL400129_AE_VISACTFFile_001O_%s_%s.ctf", now.Format("20060102"), now.Format("150405"))

	var outputPath string
	filePrompt := &survey.Input{
		Message: "Output CTF file path:",
		Default: defaultFile,
	}
	if err := survey.AskOne(filePrompt, &outputPath); err != nil {
		return nil
	}

	return exportCTFForSession(sessionID, outputPath, strings.TrimSpace(binFilter), "400129")
}
