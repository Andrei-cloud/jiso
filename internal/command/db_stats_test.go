package command

import (
	"path/filepath"
	"testing"

	"jiso/internal/config"
	"jiso/internal/db"
)

func TestDbStatsCommandExecution(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "dbstats_test.db")

	config.GetConfig().Reset()
	config.GetConfig().SetDbPath(dbPath)

	if err := db.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	sessionID := "test-dbstats-sess"
	config.GetConfig().SetSessionId(sessionID)

	_ = db.UpsertSession(sessionID, "specs/spec.json", "spec.json", "transactions/tx.json", "tx.json", "localhost", "9999", "CLIENT", "2-byte", "active", false)

	_ = db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID:        sessionID,
		TxName:           "Purchase",
		RequestJSON:      `{"mti":"0200","fields":{"3":"000000"}}`,
		ResponseJSON:     stringPtrResp(`{"mti":"0210","fields":{"39":"00"}}`),
		ProcessingTimeMs: 100,
		Success:          true,
	})

	_ = db.InsertStressTestSummary(&db.StressTestSummaryRecord{
		SessionID:              sessionID,
		WorkerID:               "stress-w1",
		TargetTPS:              10,
		Concurrency:            1,
		TotalDurationMs:        5000,
		TotalTransactions:      50,
		SuccessfulTransactions: 50,
		FailedTransactions:     0,
		AverageTPS:             10.0,
		PeakTPS:                11.0,
		MinLatencyMs:           5.0,
		MaxLatencyMs:           15.0,
		MeanLatencyMs:          8.0,
		P50LatencyMs:           7.5,
		P90LatencyMs:           12.0,
		P95LatencyMs:           14.0,
		P99LatencyMs:           15.0,
		TransactionsJSON:       `["Purchase"]`,
		ResponseCodesJSON:      `{"00":50}`,
	})

	cmd := &DbStatsCommand{}

	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("dbstats list failed: %v", err)
	}

	cmdOverview := &DbStatsCommand{SessionID: sessionID}
	if err := cmdOverview.Execute(); err != nil {
		t.Errorf("dbstats session overview failed: %v", err)
	}

	txs, _ := db.GetSessionTransactions(sessionID)
	if len(txs) > 0 {
		cmdTx := &DbStatsCommand{SubCommand: "tx", TxID: txs[0].ID}
		if err := cmdTx.Execute(); err != nil {
			t.Errorf("dbstats tx detail failed: %v", err)
		}
	}
}

func TestDbStatsCommandResetBetweenExecutions(t *testing.T) {
	cmd := &DbStatsCommand{}

	// First execution: dbstats tx 1
	cmd.SetArgs([]string{"tx", "1"})
	if cmd.SubCommand != "tx" || cmd.TxID != 1 {
		t.Errorf("Expected SubCommand='tx' and TxID=1, got SubCommand='%s', TxID=%d", cmd.SubCommand, cmd.TxID)
	}

	// Subsequent execution: dbstats (no args)
	cmd.SetArgs([]string{})
	if cmd.SubCommand != "" || cmd.TxID != 0 || cmd.SessionID != "" {
		t.Errorf("Expected state to be reset, got SubCommand='%s', TxID=%d, SessionID='%s'", cmd.SubCommand, cmd.TxID, cmd.SessionID)
	}
}

func stringPtrResp(s string) *string {
	return &s
}

