package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"jiso/internal/db"
)

const cli102Spec = `{
	"name": "CLI-102 spec",
	"fields": {
		"0":  {"type": "String",  "length": 4, "description": "MTI",            "enc": "ASCII",  "prefix": "ASCII.Fixed"},
		"1":  {"type": "Bitmap",  "length": 8, "description": "Bitmap",         "enc": "Binary", "prefix": "Hex.Fixed"},
		"3":  {"type": "String",  "length": 6, "description": "Processing Code","enc": "ASCII",  "prefix": "ASCII.Fixed"},
		"11": {"type": "String",  "length": 6, "description": "STAN",           "enc": "ASCII",  "prefix": "ASCII.Fixed"},
		"39": {"type": "String",  "length": 2, "description": "Response Code",  "enc": "ASCII",  "prefix": "ASCII.Fixed"}
	}
}`

const cli102Tx = `[
	{
		"type": "transaction",
		"name": "Purchase",
		"description": "Purchase tx",
		"fields": {"0": "0200", "3": "000000", "11": "123456"}
	},
	{
		"type": "scenario",
		"name": "Sign On",
		"description": "signon flow",
		"steps": [{"name": "Sign On Step", "use_transaction_id": "Purchase"}]
	}
]`

const cli102SessionID = "cli102-sess"

// cli102Notice is the non-essential stdout notice emitted by the inspect
// human printer; used to assert --quiet suppression.
const cli102Notice = "Composing a sample message"

type cli102Fixtures struct {
	specPath string
	txPath   string
}

func writeCLI102Fixtures(t *testing.T) cli102Fixtures {
	t.Helper()

	dir := t.TempDir()
	f := cli102Fixtures{
		specPath: filepath.Join(dir, "spec.json"),
		txPath:   filepath.Join(dir, "tx.json"),
	}
	require.NoError(t, os.WriteFile(f.specPath, []byte(cli102Spec), 0o644))
	require.NoError(t, os.WriteFile(f.txPath, []byte(cli102Tx), 0o644))

	return f
}

// seedCLI102DB creates a session DB with one session and one transaction,
// closing the seeding connection so commands open it themselves.
func seedCLI102DB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "cli102.db")
	require.NoError(t, db.InitDB(dbPath))
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.UpsertSession(cli102SessionID, "spec.json", "spec.json", "tx.json", "tx.json",
		"127.0.0.1", "9999", "CLIENT", "ascii4", "active", false))

	resp := `{"mti":"0210","fields":{"39":"00"}}`
	require.NoError(t, db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
		SessionID:        cli102SessionID,
		TxName:           "Purchase",
		RequestJSON:      `{"mti":"0200","fields":{"3":"000000"}}`,
		ResponseJSON:     &resp,
		ProcessingTimeMs: 42,
		Success:          true,
		ResponseCode:     "00",
	}))

	require.NoError(t, db.Close())

	return dbPath
}

func runCLI102(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	resetConfig(t)

	rootCmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(args)

	err := rootCmd.Execute()

	return out.String(), errBuf.String(), err
}

func TestOutputFlagsRegistered(t *testing.T) {
	rootCmd := NewRootCmd()

	tests := []struct {
		name      string
		flag      string
		wantShort string
	}{
		{"json", "json", ""},
		{"quiet", "quiet", "q"},
		{"dry-run", "dry-run", "n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := rootCmd.PersistentFlags().Lookup(tt.flag)
			require.NotNil(t, f, "--%s must be a root persistent flag", tt.flag)
			assert.Equal(t, tt.wantShort, f.Shorthand)
		})
	}
}

func TestJSONFlagOutputIsPureParseable(t *testing.T) {
	f := writeCLI102Fixtures(t)
	dbPath := seedCLI102DB(t)

	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, v any)
	}{
		{
			name: "inspect --json",
			args: []string{"inspect", "Purchase", "--json", "--spec", f.specPath, "--file", f.txPath},
			check: func(t *testing.T, v any) {
				obj, ok := v.(map[string]any)
				require.True(t, ok, "inspect view must be a JSON object")
				assert.Equal(t, "Purchase", obj["name"])
				assert.Equal(t, "0200", obj["mti"])
				assert.Equal(t, "000000", obj["processing_code"])
				assert.NotEmpty(t, obj["packed_hex"])
				assert.NotEmpty(t, obj["parsed_message"])
			},
		},
		{
			name: "db stats list --json",
			args: []string{"db", "stats", "list", "--json", "--db", dbPath},
			check: func(t *testing.T, v any) {
				arr, ok := v.([]any)
				require.True(t, ok, "session list must be a JSON array")
				require.Len(t, arr, 1)
				session, ok := arr[0].(map[string]any)
				require.Truef(t, ok, "session list element = %T, want JSON object", arr[0])
				assert.Equal(t, cli102SessionID, session["session_id"])
			},
		},
		{
			name: "db stats overview --json",
			args: []string{"db", "stats", cli102SessionID, "--json", "--db", dbPath},
			check: func(t *testing.T, v any) {
				obj, ok := v.(map[string]any)
				require.True(t, ok, "overview must be a JSON object")
				require.NotNil(t, obj["session"])
				require.NotNil(t, obj["stats"])
				session, ok := obj["session"].(map[string]any)
				require.Truef(t, ok, "overview session = %T, want JSON object", obj["session"])
				assert.Equal(t, cli102SessionID, session["session_id"])
				txs, ok := obj["transactions"].([]any)
				require.True(t, ok, "overview must include transactions")
				first, ok := txs[0].(map[string]any)
				require.Truef(t, ok, "overview transaction = %T, want JSON object", txs[0])
				assert.Equal(t, "Purchase", first["transaction_name"])
			},
		},
		{
			name: "scenario list --json",
			args: []string{"scenario", "list", "--json", "--spec", f.specPath, "--file", f.txPath},
			check: func(t *testing.T, v any) {
				arr, ok := v.([]any)
				require.True(t, ok, "scenario list must be a JSON array")
				require.Len(t, arr, 1)
				first, ok := arr[0].(map[string]any)
				require.Truef(t, ok, "scenario list element = %T, want JSON object", arr[0])
				assert.Equal(t, "Sign On", first["name"])
				assert.Equal(t, float64(1), first["step_count"])
			},
		},
		{
			name: "ctf list --json",
			args: []string{"ctf", "list", "--json", "--db", dbPath},
			check: func(t *testing.T, v any) {
				arr, ok := v.([]any)
				require.True(t, ok, "ctf list must be a JSON array")
				assert.Empty(t, arr, "non-Visa session must not be listed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runCLI102(t, tt.args...)
			require.NoError(t, err)

			var v any
			require.NoError(t, json.Unmarshal([]byte(stdout), &v), "stdout must be pure parseable JSON: %q", stdout)
			assert.NotContains(t, stdout, "\x1b", "JSON stdout must carry no colors")
			assert.Empty(t, stderr, "success path must keep stderr empty")

			tt.check(t, v)
		})
	}
}

// TestDbStatsJSONNoSessionPrintsSummary asserts `db stats --json` without a
// session prints the DB-level summary over the stored rows with exit 0
// It supersedes the former usage error: that guard existed
// because the only candidate was EnsureSessionID's fresh UUID; a summary is
// real data, and it still never carries a fabricated session record.
func TestDbStatsJSONNoSessionPrintsSummary(t *testing.T) {
	dbPath := seedCLI102DB(t)

	stdout, stderr, err := runCLI102(t, "db", "stats", "--json", "--db", dbPath)
	require.NoError(t, err)
	assert.Empty(t, stderr, "success path must keep stderr empty")

	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &obj), "stdout must be pure parseable JSON: %q", stdout)

	assert.Equal(t, dbPath, obj["db_path"])
	assert.Equal(t, float64(1), obj["session_count"])
	assert.Equal(t, float64(1), obj["total_transactions"])
	assert.Greater(t, obj["size_bytes"], float64(0))
	assert.NotEmpty(t, obj["first_session_start"])
	assert.Nil(t, obj["session"], "summary must not fabricate a session record")
	assert.Nil(t, obj["transactions"], "summary must not fabricate transaction rows")
}

// TestDBStatsJSONUnknownSession asserts an unknown session ID fails with the
// not-found error instead of a fabricated session + zero stats (M1 #1).
func TestDBStatsJSONUnknownSession(t *testing.T) {
	dbPath := seedCLI102DB(t)

	stdout, _, err := runCLI102(t, "db", "stats", "bogus-session-id", "--json", "--db", dbPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")
	assert.Empty(t, stdout)
}

// TestDBStatsJSONPropagatesQueryErrors asserts a failing auxiliary query
// fails the command under --json instead of silently dropping the
// stress_tests/transactions sections with rc=0.
func TestDBStatsJSONPropagatesQueryErrors(t *testing.T) {
	dbPath := seedCLI102DB(t)

	// Replace stress_tests with a wrong-schema stub through a separate
	// connection (InitDB's CREATE TABLE IF NOT EXISTS then skips it), so the
	// command's own query fails the way a corrupted database would.
	conn, err := sqlite.OpenConn(dbPath)
	require.NoError(t, err)
	require.NoError(t, sqlitex.ExecScript(conn, "DROP TABLE stress_tests; CREATE TABLE stress_tests (id INTEGER PRIMARY KEY, session_id TEXT);"))
	require.NoError(t, conn.Close())

	stdout, _, err := runCLI102(t, "db", "stats", cli102SessionID, "--json", "--db", dbPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stress test summaries")
	assert.Empty(t, stdout, "a query failure must not emit a partial JSON view")
}

func TestQuietSuppressesNotices(t *testing.T) {
	f := writeCLI102Fixtures(t)

	tests := []struct {
		name       string
		flags      []string
		wantNotice bool
	}{
		{"human default prints notice", nil, true},
		{"-q suppresses notice", []string{"-q"}, false},
		{"--quiet suppresses notice", []string{"--quiet"}, false},
		{"--json suppresses notice", []string{"--json"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"inspect", "Purchase"}, tt.flags...)
			args = append(args, "--spec", f.specPath, "--file", f.txPath)

			stdout, _, err := runCLI102(t, args...)
			require.NoError(t, err)

			if tt.wantNotice {
				assert.Contains(t, stdout, cli102Notice)
			} else {
				assert.NotContains(t, stdout, cli102Notice)
			}
		})
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	f := writeCLI102Fixtures(t)
	cleanRoom := t.TempDir()

	tests := []struct {
		name  string
		flags []string
	}{
		{"--dry-run", []string{"--dry-run"}},
		{"-n shorthand", []string{"-n"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reportPath := filepath.Join(cleanRoom, "report.json")

			args := append([]string{"scenario", "run", "Sign On"}, tt.flags...)
			// Unreachable target: success proves dry-run never connected.
			args = append(args,
				"--host", "127.0.0.1", "--port", "1",
				"--report", reportPath,
				"--spec", f.specPath, "--file", f.txPath,
			)

			stdout, _, err := runCLI102(t, args...)
			require.NoError(t, err, "dry-run must exit 0 without connecting")

			assert.Contains(t, stdout, "Would run scenario: Sign On")
			assert.Contains(t, stdout, "Sign On Step")
			assert.False(t, fileExists(reportPath), "dry-run must not write the report")

			entries, err := os.ReadDir(cleanRoom)
			require.NoError(t, err)
			assert.Empty(t, entries, "dry-run must write nothing into the clean room")
		})
	}
}

func TestDryRunJSONIsParseablePreview(t *testing.T) {
	f := writeCLI102Fixtures(t)

	stdout, _, err := runCLI102(t,
		"scenario", "run", "Sign On", "--dry-run", "--json",
		"--spec", f.specPath, "--file", f.txPath,
	)
	require.NoError(t, err)

	var scenarios []any
	require.NoError(t, json.Unmarshal([]byte(stdout), &scenarios))
	require.Len(t, scenarios, 1)

	steps, ok := scenarios[0].(map[string]any)["steps"].([]any)
	require.True(t, ok)
	require.Len(t, steps, 1)
	step, ok := steps[0].(map[string]any)
	require.Truef(t, ok, "step = %T, want JSON object", steps[0])
	assert.Equal(t, "Sign On Step", step["name"])
}

func TestJSONErrorPathsKeepStdoutEmpty(t *testing.T) {
	f := writeCLI102Fixtures(t)

	tests := []struct {
		name       string
		args       []string
		wantErrSub string
	}{
		{
			name:       "inspect unknown tx",
			args:       []string{"inspect", "NopeTx", "--json", "--spec", f.specPath, "--file", f.txPath},
			wantErrSub: "NopeTx",
		},
		{
			name:       "db stats without database",
			args:       []string{"db", "stats", "list", "--json"},
			wantErrSub: "database not configured",
		},
		{
			name:       "missing spec file is a config error",
			args:       []string{"inspect", "Purchase", "--json", "--spec", "nope.json", "--file", f.txPath},
			wantErrSub: "nope.json",
		},
		{
			name:       "scenario list without spec",
			args:       []string{"scenario", "list", "--json"},
			wantErrSub: "spec file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := runCLI102(t, tt.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrSub)
			assert.Empty(t, stdout, "stdout must stay empty on error; errors go to stderr as plain text")
		})
	}
}

func TestOutputEnvFlagFallbacks(t *testing.T) {
	f := writeCLI102Fixtures(t)

	t.Run("JISO_JSON=1 enables machine output", func(t *testing.T) {
		t.Setenv("JISO_JSON", "1")

		stdout, _, err := runCLI102(t, "scenario", "list", "--spec", f.specPath, "--file", f.txPath)
		require.NoError(t, err)

		var v []any
		require.NoError(t, json.Unmarshal([]byte(stdout), &v), "env fallback must produce pure JSON: %q", stdout)
	})

	t.Run("JISO_QUIET=1 suppresses notices", func(t *testing.T) {
		t.Setenv("JISO_QUIET", "1")

		stdout, _, err := runCLI102(t, "inspect", "Purchase", "--spec", f.specPath, "--file", f.txPath)
		require.NoError(t, err)
		assert.NotContains(t, stdout, cli102Notice)
	})

	t.Run("explicit flag beats env", func(t *testing.T) {
		t.Setenv("JISO_JSON", "1")

		stdout, _, err := runCLI102(t, "scenario", "list", "--json=false", "--spec", f.specPath, "--file", f.txPath)
		require.NoError(t, err)
		assert.Contains(t, stdout, "Available Scenarios:", "--json=false must beat JISO_JSON=1")
	})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
