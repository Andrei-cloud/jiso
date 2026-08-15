package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/config"
	"jiso/internal/db"
)

func TestCTFCommand_NameAndSynopsis(t *testing.T) {
	cmd := &CTFCommand{}
	assert.Equal(t, "ctf", cmd.Name())
	assert.NotEmpty(t, cmd.Synopsis())
}

func TestCTFCommand_SetArgs(t *testing.T) {
	cmd := &CTFCommand{}

	cmd.SetArgs([]string{"list"})
	assert.Equal(t, "list", cmd.SubCommand)

	cmd.SetArgs([]string{"export", "sess-123", "out.ctf", "400000"})
	assert.Equal(t, "export", cmd.SubCommand)
	assert.Equal(t, "sess-123", cmd.SessionID)
	assert.Equal(t, "out.ctf", cmd.OutputPath)
	assert.Equal(t, "400000", cmd.BINFilter)

	cmd.SetArgs([]string{"sess-456", "custom.ctf"})
	assert.Equal(t, "", cmd.SubCommand)
	assert.Equal(t, "sess-456", cmd.SessionID)
	assert.Equal(t, "custom.ctf", cmd.OutputPath)
}

func TestCTFCommand_ExecuteExport(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ctf_test.db")
	outputCTF := filepath.Join(tmpDir, "test_output.ctf")

	config.GetConfig().SetDbPath(dbPath)
	require.NoError(t, db.InitDB(dbPath))
	defer func() {
		_ = db.Close()
		config.GetConfig().SetDbPath("")
	}()

	sessionID := "sess-ctf-001"
	require.NoError(t, db.UpsertSession(sessionID, "specs/visa.json", "visa.json", "tx.json", "tx.json", "127.0.0.1", "9000", "CLIENT", "Visa", "active", false))

	resp00 := `{"mti":"0110","fields":{"38":"123456","39":"00"}}`
	require.NoError(t, db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID:    sessionID,
		TxName:       "Visa Auth Approved",
		Timestamp:    time.Now().UTC(),
		Success:      true,
		ResponseCode: "00",
		RequestJSON:  `{"mti":"0100","fields":{"2":"4000000000000002","3":"000000","4":"000000010000","49":"840"}}`,
		ResponseJSON: &resp00,
	}))

	cmd := &CTFCommand{}
	cmd.SetArgs([]string{"export", sessionID, outputCTF})
	err := cmd.Execute()
	require.NoError(t, err)

	// Verify file was created on disk
	content, err := os.ReadFile(outputCTF)
	require.NoError(t, err)
	assert.NotEmpty(t, content)

	// Verify 1 transaction produces 5 lines: TCR 0, TCR 1, TCR 5, TC 91, TC 92
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	assert.Equal(t, 5, len(lines))
	for _, l := range lines {
		assert.Equal(t, 168, len(l))
	}
}

func TestCTFCommand_ExecuteList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ctf_list_test.db")

	config.GetConfig().SetDbPath(dbPath)
	require.NoError(t, db.InitDB(dbPath))
	defer func() {
		_ = db.Close()
		config.GetConfig().SetDbPath("")
	}()

	sessionID := "sess-ctf-list-001"
	require.NoError(t, db.UpsertSession(sessionID, "specs/visa.json", "visa.json", "tx.json", "tx.json", "127.0.0.1", "9000", "CLIENT", "Visa", "active", false))

	cmd := &CTFCommand{}
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	assert.NoError(t, err)
}
