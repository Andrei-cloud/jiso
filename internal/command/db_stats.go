package command

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"

	"jiso/internal/config"
	"jiso/internal/db"
)

type DbStatsCommand struct {
	SessionID  string
	SubCommand string
	TxID       int64
}

func (c *DbStatsCommand) Name() string {
	return "dbstats"
}

func (c *DbStatsCommand) Synopsis() string {
	return "Show database statistics, list sessions, and inspect transaction details (HEX & Parsed ISO)"
}

func (c *DbStatsCommand) SetArgs(args []string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "list", "-l", "--list":
		c.SubCommand = "list"
	case "tx", "-t", "--tx":
		c.SubCommand = "tx"
		if len(args) >= 2 {
			c.TxID, _ = strconv.ParseInt(args[1], 10, 64)
		}
	default:
		// Check if first arg is an integer (tx ID)
		if txID, err := strconv.ParseInt(args[0], 10, 64); err == nil && txID > 0 {
			c.SubCommand = "tx"
			c.TxID = txID
		} else {
			c.SessionID = args[0]
		}
	}
}

func (c *DbStatsCommand) Execute() error {
	dbPath := config.GetConfig().GetDbPath()
	if dbPath == "" {
		return errors.New("database not configured (use --db-path flag)")
	}

	if c.SubCommand == "list" {
		return printSessionsList()
	}

	if c.SubCommand == "tx" {
		if c.TxID <= 0 {
			return errors.New("please specify a valid transaction ID: dbstats tx <id>")
		}
		return printTransactionDetail(c.TxID)
	}

	if c.SessionID != "" {
		return printSessionOverview(c.SessionID)
	}

	// Interactive Mode
	return runInteractiveMenu()
}

func printSessionsList() error {
	sessions, err := db.GetSessionsList()
	if err != nil {
		return fmt.Errorf("failed to fetch sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions recorded in database.")
		return nil
	}

	fmt.Printf("\n%-36s | %-19s | %-16s | %-16s | %-8s | %-6s\n", "Session ID", "Start Time", "Spec Name", "Tx File", "Total Tx", "Status")
	fmt.Println(strings.Repeat("-", 112))

	for _, s := range sessions {
		spec := s.SpecName
		if spec == "" {
			spec = "-"
		}
		txFile := s.TxFileName
		if txFile == "" {
			txFile = "-"
		}
		status := s.Status
		if status == "" {
			status = "active"
		}
		fmt.Printf("%-36s | %-19s | %-16s | %-16s | %-8d | %-6s\n",
			s.SessionID,
			s.StartTime.Format("2006-01-02 15:04:05"),
			truncateString(spec, 16),
			truncateString(txFile, 16),
			s.TransactionCount,
			status,
		)
	}
	fmt.Println()
	return nil
}

func printSessionOverview(sessionID string) error {
	rec, err := db.GetSessionByID(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}

	stats, err := db.GetTransactionStats(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get database stats: %w", err)
	}

	fmt.Printf("\nDatabase Statistics for Session: %s\n", sessionID)
	fmt.Println("=========================================================================")
	if !rec.StartTime.IsZero() {
		fmt.Printf("Start Time:             %s\n", rec.StartTime.Format("2006-01-02 15:04:05"))
	}
	if !rec.LastActiveTime.IsZero() {
		fmt.Printf("Last Active Time:       %s\n", rec.LastActiveTime.Format("2006-01-02 15:04:05"))
	}
	if rec.SpecName != "" {
		fmt.Printf("Specification:          %s (%s)\n", rec.SpecName, rec.SpecPath)
	}
	if rec.TxFileName != "" {
		fmt.Printf("Transaction File:       %s (%s)\n", rec.TxFileName, rec.TxFilePath)
	}
	if rec.Status != "" {
		fmt.Printf("Status:                 %s\n", rec.Status)
	}

	fmt.Printf("\nTotal Transactions:     %v\n", stats["total_transactions"])
	fmt.Printf("Successful Transactions: %v\n", stats["successful_transactions"])
	fmt.Printf("Failed Transactions:     %v\n", stats["failed_transactions"])
	fmt.Printf("Average Processing Time: %.2f ms\n", stats["average_processing_time_ms"])

	if responseCodes, ok := stats["response_code_distribution"].(map[string]int); ok && len(responseCodes) > 0 {
		fmt.Printf("\nResponse Code Distribution:\n")
		for code, count := range responseCodes {
			fmt.Printf("  %s: %d\n", code, count)
		}
	}

	// List transactions in this session
	txs, err := db.GetSessionTransactions(sessionID)
	if err == nil && len(txs) > 0 {
		fmt.Printf("\nTransactions in Session (%d):\n", len(txs))
		fmt.Printf("%-6s | %-19s | %-24s | %-10s | %-6s | %-4s\n", "ID", "Timestamp", "Name", "Proc Time", "Result", "Code")
		fmt.Println(strings.Repeat("-", 82))
		for _, t := range txs {
			resStr := "SUCCESS"
			if !t.Success {
				resStr = "FAIL"
			}
			fmt.Printf("%-6d | %-19s | %-24s | %-7d ms | %-6s | %-4s\n",
				t.ID,
				t.Timestamp.Format("2006-01-02 15:04:05"),
				truncateString(t.TxName, 24),
				t.ProcessingTimeMs,
				resStr,
				t.ResponseCode,
			)
		}
		fmt.Println("\nTo inspect a transaction: dbstats tx <id>")
	}

	return nil
}

func printTransactionDetail(txID int64) error {
	tx, err := db.GetTransactionByID(txID)
	if err != nil {
		return fmt.Errorf("failed to fetch transaction: %w", err)
	}

	fmt.Printf("\n=========================================================================\n")
	fmt.Printf("TRANSACTION RETROSPECTIVE REVIEW [ID: %d]\n", tx.ID)
	fmt.Printf("=========================================================================\n")
	fmt.Printf("Session ID:        %s\n", tx.SessionID)
	fmt.Printf("Transaction Name:  %s\n", tx.TxName)
	fmt.Printf("Timestamp:         %s\n", tx.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("Processing Time:   %d ms\n", tx.ProcessingTimeMs)
	resultStr := "SUCCESS"
	if !tx.Success {
		resultStr = "FAILED"
	}
	fmt.Printf("Result:            %s (Response Code: %s)\n", resultStr, tx.ResponseCode)
	if tx.SpecName != "" {
		fmt.Printf("Specification:     %s (%s)\n", tx.SpecName, tx.SpecPath)
	}
	if tx.TxFileName != "" {
		fmt.Printf("Template File:     %s (%s)\n", tx.TxFileName, tx.TxFilePath)
	}

	// 1. Request Reconstruction
	fmt.Printf("\n--- REQUEST MESSAGE ---\n")
	reqReconstructed, err := db.Reconstruct(tx.RequestJSON, tx.RequestRawHEX, tx.SpecPath)
	if err != nil {
		fmt.Printf("Error reconstructing request: %v\n", err)
	} else {
		if reqReconstructed.HEX != "" {
			fmt.Printf("\nRequest HEX:\n%s\n", reqReconstructed.HEX)
		}
		if reqReconstructed.DescribeText != "" {
			fmt.Printf("Request Parsed ISO8583 Message:\n%s\n", reqReconstructed.DescribeText)
		}
	}

	// 2. Response Reconstruction
	fmt.Printf("\n--- RESPONSE MESSAGE ---\n")
	if tx.ResponseJSON != nil || tx.ResponseRawHEX != nil {
		respJSONStr := ""
		if tx.ResponseJSON != nil {
			respJSONStr = *tx.ResponseJSON
		}
		respHexStr := ""
		if tx.ResponseRawHEX != nil {
			respHexStr = *tx.ResponseRawHEX
		}
		respReconstructed, err := db.Reconstruct(respJSONStr, respHexStr, tx.SpecPath)
		if err != nil {
			fmt.Printf("Error reconstructing response: %v\n", err)
		} else {
			if respReconstructed.HEX != "" {
				fmt.Printf("\nResponse HEX:\n%s\n", respReconstructed.HEX)
			}
			if respReconstructed.DescribeText != "" {
				fmt.Printf("Response Parsed ISO8583 Message:\n%s\n", respReconstructed.DescribeText)
			}
		}
	} else {
		fmt.Println("(No response message recorded for this transaction)")
	}

	return nil
}

func runInteractiveMenu() error {
	currentSessionID := config.GetConfig().GetSessionId()

	options := []string{
		"1. Current Session Overview",
		"2. List Recorded Sessions",
		"3. Select Session to Inspect",
		"4. Review Transaction Detail (by ID)",
		"5. Exit",
	}

	var choice string
	prompt := &survey.Select{
		Message: "Database Statistics & History Review:",
		Options: options,
	}
	if err := survey.AskOne(prompt, &choice); err != nil {
		return printSessionOverview(currentSessionID)
	}

	switch {
	case strings.HasPrefix(choice, "1"):
		return printSessionOverview(currentSessionID)
	case strings.HasPrefix(choice, "2"):
		return printSessionsList()
	case strings.HasPrefix(choice, "3"):
		sessions, err := db.GetSessionsList()
		if err != nil || len(sessions) == 0 {
			fmt.Println("No sessions available.")
			return nil
		}
		sessOptions := make([]string, len(sessions))
		for i, s := range sessions {
			sessOptions[i] = fmt.Sprintf("%s | %s | Tx: %d", s.SessionID, s.StartTime.Format("2006-01-02 15:04:05"), s.TransactionCount)
		}
		var selectedSess string
		if err := survey.AskOne(&survey.Select{Message: "Select session:", Options: sessOptions}, &selectedSess); err != nil {
			return nil
		}
		parts := strings.Split(selectedSess, " | ")
		return printSessionOverview(parts[0])
	case strings.HasPrefix(choice, "4"):
		var txIDStr string
		if err := survey.AskOne(&survey.Input{Message: "Enter Transaction ID to review:"}, &txIDStr); err != nil {
			return nil
		}
		txID, err := strconv.ParseInt(strings.TrimSpace(txIDStr), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid transaction ID: %s", txIDStr)
		}
		return printTransactionDetail(txID)
	default:
		return nil
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
