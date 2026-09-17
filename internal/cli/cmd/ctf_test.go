package cmd

import (
	"bytes"
	"encoding/json"
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

// seedVisaSession creates a Visa session with one approved transaction and
// returns the session ID; the database is seeded at dbPath.
func seedVisaSession(t *testing.T, dbPath string) string {
	t.Helper()

	cfg.GetConfig().SetDbPath(dbPath)
	require.NoError(t, db.InitDB(dbPath))
	t.Cleanup(func() {
		_ = db.Close()
		cfg.GetConfig().SetDbPath("")
	})

	sessionID := "sess-cli-visa-dry"
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

	return sessionID
}

// TestCTFCliCommand_ExportDryRunWritesNothing asserts --dry-run prints the
// export plan and writes no CTF file (M1 review #3/#19).
func TestCTFCliCommand_ExportDryRunWritesNothing(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli_ctf_dry.db")
	sessionID := seedVisaSession(t, dbPath)

	tests := []struct {
		name  string
		flags []string
	}{
		{"--dry-run", []string{"--dry-run"}},
		{"-n shorthand", []string{"-n"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planPath := filepath.Join(tmpDir, "plan_"+strings.Join(tt.flags, "")+".ctf")

			cmd := NewRootCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(new(bytes.Buffer))
			cmd.SetArgs(append([]string{"ctf", "export", "--session-id", sessionID, "--output", planPath, "--db", dbPath}, tt.flags...))

			require.NoError(t, cmd.Execute())

			assert.Contains(t, out.String(), "EXPORT PLAN")
			_, statErr := os.Stat(planPath)
			assert.ErrorIs(t, statErr, os.ErrNotExist, "dry-run must not write the CTF file")
		})
	}
}

// TestCTFCliCommand_ExportJSONIsPure asserts the export banner is suppressed
// under --json in favour of a pure-JSON summary.
func TestCTFCliCommand_ExportJSONIsPure(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli_ctf_json.db")
	outPath := filepath.Join(tmpDir, "export_json.ctf")
	sessionID := seedVisaSession(t, dbPath)

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"ctf", "export", "--session-id", sessionID, "--output", outPath, "--db", dbPath, "--json"})

	require.NoError(t, cmd.Execute())

	var summary map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &summary), "stdout must be pure JSON: %q", out.String())
	assert.NotContains(t, out.String(), "\x1b")
	assert.Equal(t, sessionID, summary["session_id"])
	assert.Equal(t, true, summary["written"])
	assert.True(t, fileExists(outPath), "non-dry-run export must write the file")

	// Dry-run under --json: pure JSON plan, no file.
	planPath := filepath.Join(tmpDir, "plan_json.ctf")
	cmd2 := NewRootCmd()
	var out2 bytes.Buffer
	cmd2.SetOut(&out2)
	cmd2.SetErr(new(bytes.Buffer))
	cmd2.SetArgs([]string{"ctf", "export", "--session-id", sessionID, "--output", planPath, "--db", dbPath, "--json", "--dry-run"})

	require.NoError(t, cmd2.Execute())

	var plan map[string]any
	require.NoError(t, json.Unmarshal(out2.Bytes(), &plan))
	assert.Equal(t, true, plan["dry_run"])
	assert.Equal(t, false, plan["written"])
	assert.False(t, fileExists(planPath))
}

// TestCTFCliCommand_ExportMissingSessionIDExitsUsage asserts a
// missing --session/--session-id is a usage error (exit 2)
// naming the flag.
func TestCTFCliCommand_ExportMissingSessionIDExitsUsage(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli_ctf_usage.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("placeholder"), 0o600))

	cmd := NewRootCmd()
	var errBuf bytes.Buffer
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"ctf", "export", "--db", dbPath})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, ExitUsage, ExitCodeForError(err), "exit code for %v", err)
	assert.Contains(t, errBuf.String(), "--session is required")
}

// TestCTFCliCommand_ExportSessionAliasAndYes asserts the headless spelling
// `ctf export --session <id> --yes` works: --yes is a no-op, --session
// resolves, and --dry-run with them still writes nothing.
func TestCTFCliCommand_ExportSessionAliasAndYes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cli_ctf_alias.db")
	sessionID := seedVisaSession(t, dbPath)

	outPath := filepath.Join(tmpDir, "alias.ctf")
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"ctf", "export", "--session", sessionID, "--yes", "-o", outPath, "--db", dbPath})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "EXPORT SUCCESSFUL")
	assert.True(t, fileExists(outPath), "--session must resolve like --session-id")

	planPath := filepath.Join(tmpDir, "alias-dry.ctf")
	cmd2 := NewRootCmd()
	var out2 bytes.Buffer
	cmd2.SetOut(&out2)
	cmd2.SetErr(new(bytes.Buffer))
	cmd2.SetArgs([]string{"ctf", "export", "--session", sessionID, "--yes", "-o", planPath, "--db", dbPath, "--dry-run"})
	require.NoError(t, cmd2.Execute())
	assert.Contains(t, out2.String(), "Session ID:")
	assert.Contains(t, out2.String(), sessionID)
	assert.Contains(t, out2.String(), "Messages to Export:")
	assert.Contains(t, out2.String(), planPath)
	assert.NotContains(t, out2.String(), "EXPORT SUCCESSFUL")
	assert.FileExists(t, dbPath)
	_, statErr := os.Stat(planPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "dry-run with --session/--yes must not write")
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

	// Missing --session is usage (exit 2).
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"ctf", "export", "--db", dbPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, ExitUsage, ExitCodeForError(err), "exit code for %v", err)

	// Non-existent session is config-class (exit 3) naming the id.
	cmd2 := NewRootCmd()
	cmd2.SetArgs([]string{"ctf", "export", "--session", "non-existent-sess", "--db", dbPath})
	err2 := cmd2.Execute()
	require.Error(t, err2)
	assert.Equal(t, ExitConfig, ExitCodeForError(err2), "exit code for %v", err2)
	assert.Contains(t, err2.Error(), "non-existent-sess")
}
