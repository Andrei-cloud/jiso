package command

import (
	"encoding/json"
	"fmt"

	"jiso/internal/config"
	"jiso/internal/db"
)

type DbStatsCommand struct{}

func (c *DbStatsCommand) Name() string {
	return "dbstats"
}

func (c *DbStatsCommand) Synopsis() string {
	return "Show database statistics for the current session"
}

func (c *DbStatsCommand) Execute() error {
	dbPath := config.GetConfig().GetDbPath()
	if dbPath == "" {
		return fmt.Errorf("database not configured (use --db-path flag)")
	}

	sessionID := config.GetConfig().GetSessionId()
	if sessionID == "" {
		return fmt.Errorf("session ID not available")
	}

	stats, err := db.GetTransactionStats(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get database stats: %w", err)
	}

	fmt.Printf("Database Statistics for Session: %s\n", sessionID)
	fmt.Printf("=====================================\n")
	fmt.Printf("Total Transactions: %v\n", stats["total_transactions"])
	fmt.Printf("Successful Transactions: %v\n", stats["successful_transactions"])
	fmt.Printf("Failed Transactions: %v\n", stats["failed_transactions"])
	fmt.Printf("Average Processing Time: %.2f ms\n", stats["average_processing_time_ms"])

	if responseCodes, ok := stats["response_code_distribution"].(map[string]int); ok &&
		len(responseCodes) > 0 {
		fmt.Printf("\nResponse Code Distribution:\n")
		for code, count := range responseCodes {
			fmt.Printf("  %s: %d\n", code, count)
		}
	}

	// Pretty print the full stats as JSON
	fmt.Printf("\nFull Statistics (JSON):\n")
	statsJSON, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats to JSON: %w", err)
	}
	fmt.Println(string(statsJSON))

	return nil
}
