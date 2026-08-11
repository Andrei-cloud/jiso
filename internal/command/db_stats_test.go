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

	_ = db.UpsertSession(sessionID, "specs/spec.json", "spec.json", "transactions/tx.json", "tx.json", "active")
	_ = db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID:        sessionID,
		TxName:           "Purchase",
		RequestJSON:      `{"mti":"0200","fields":{"3":"000000"}}`,
		ResponseJSON:     stringPtrResp(`{"mti":"0210","fields":{"39":"00"}}`),
		ProcessingTimeMs: 100,
		Success:          true,
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

func stringPtrResp(s string) *string {
	return &s
}
