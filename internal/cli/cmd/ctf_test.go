package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "jiso/internal/config"
	"jiso/internal/db"
)

func TestCTFCliCommand_ExportAndList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli_ctf_test.db")
	outputCTF := filepath.Join(tmpDir, "cli_export.ctf")

	cfg.GetConfig().SetDbPath(dbPath)
	require.NoError(t, db.InitDB(dbPath))
	defer func() {
		_ = db.Close()
		cfg.GetConfig().SetDbPath("")
	}()

	sessionID := "sess-cli-visa-01"
	require.NoError(t, db.UpsertSession(sessionID, "specs/visa.json", "visa.json", "tx.json", "tx.json", "127.0.0.1", "9000", "CLIENT", "Visa", "active", false))

	resp00 := `{"mti":"0110","fields":{"38":"123456","39":"00"}}`
	require.NoError(t, db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID:    sessionID,
		TxName:       "Visa Purchase",
		Timestamp:    time.Now().UTC(),
		Success:      true,
		ResponseCode: "00",
		RequestJSON:  `{"mti":"0100","fields":{"2":"4000000000000002","3":"000000","4":"000000050000","49":"840"}}`,
		ResponseJSON: &resp00,
	}))

	// 1. Test ctf list
	rootCmd := NewRootCmd()
	var listBuf bytes.Buffer
	rootCmd.SetOut(&listBuf)
	rootCmd.SetArgs([]string{"ctf", "list", "--db", dbPath})
	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, listBuf.String(), sessionID)

	// 2. Test ctf export
	exportCmd := NewRootCmd()
	var exportBuf bytes.Buffer
	exportCmd.SetOut(&exportBuf)
	exportCmd.SetArgs([]string{"ctf", "export", "--session-id", sessionID, "--output", outputCTF, "--db", dbPath})
	err = exportCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, exportBuf.String(), "VISA BASE II CTF CLEARING FILE EXPORT SUCCESSFUL")

	// Verify file
	content, err := os.ReadFile(outputCTF)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	assert.Equal(t, 5, len(lines))
	for _, l := range lines {
		assert.Equal(t, 168, len(l))
	}
}

func TestCTFCliCommand_Errors(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli_ctf_err.db")

	cfg.GetConfig().SetDbPath(dbPath)
	require.NoError(t, db.InitDB(dbPath))
	defer func() {
		_ = db.Close()
		cfg.GetConfig().SetDbPath("")
	}()

	// Missing --session-id
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"ctf", "export", "--db", dbPath})
	err := cmd.Execute()
	assert.Error(t, err)

	// Non-existent session
	cmd2 := NewRootCmd()
	cmd2.SetArgs([]string{"ctf", "export", "--session-id", "non-existent-sess", "--db", dbPath})
	err2 := cmd2.Execute()
	assert.Error(t, err2)
}
