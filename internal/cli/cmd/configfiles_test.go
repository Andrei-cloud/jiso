package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tmpl "jiso/internal/command/templates"
	cfg "jiso/internal/config"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func writeBadSpec(t *testing.T) string {
	t.Helper()

	return writeTempFile(t, "bad_spec.json", "{bad json")
}

func writeGoodSpec(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "spec.json")
	require.NoError(t, os.WriteFile(path, tmpl.DefaultSpecJSON, 0o600))

	return path
}

// executeRoot runs the root command with isolated config and captured streams.
func executeRoot(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	resetConfig(t)

	rootCmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(args)

	err = rootCmd.Execute()

	return out.String(), errBuf.String(), err
}

func TestSpecTxLoadValidationExitsConfig(t *testing.T) {
	badSpec := writeBadSpec(t)
	goodSpec := writeGoodSpec(t)
	goodTx := writeTempFile(t, "transactions.json", oneTxJSON)
	badTx := writeTempFile(t, "bad_transactions.json", "{bad json")
	nopeSpec := filepath.Join(t.TempDir(), "nope.json")
	nopeTx := filepath.Join(t.TempDir(), "nope_tx.json")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "missing spec names file",
			args: []string{"--spec", nopeSpec, "scenario", "run", "-f", goodTx},
			want: []string{nopeSpec},
		},
		{
			name: "malformed spec names file and parse error",
			args: []string{"--spec", badSpec, "scenario", "run", "-f", goodTx},
			want: []string{badSpec, "failed to load spec", "unmarshal"},
		},
		{
			name: "bare invocation malformed spec exits config",
			args: []string{"--spec", badSpec},
			want: []string{badSpec, "failed to load spec"},
		},
		{
			name: "missing tx file names file",
			args: []string{"--spec", goodSpec, "scenario", "list", "-f", nopeTx},
			want: []string{nopeTx},
		},
		{
			name: "malformed tx names file and parse error",
			args: []string{"--spec", goodSpec, "scenario", "list", "-f", badTx},
			want: []string{badTx, "unmarshal"},
		},
		{
			name: "inspect malformed spec fails early",
			args: []string{"--spec", badSpec, "inspect"},
			want: []string{badSpec, "failed to load spec"},
		},
		{
			name: "server routes malformed spec fails early",
			args: []string{"--spec", badSpec, "server", "routes"},
			want: []string{badSpec, "failed to load spec"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := executeRoot(t, tt.args...)

			require.Error(t, err)
			assert.Equal(t, ExitConfig, ExitCodeForError(err), "exit code for %v", err)

			var cfgErr *ExitConfigError
			require.True(t, errors.As(err, &cfgErr), "expected *ExitConfigError, got %T: %v", err, err)

			for _, want := range tt.want {
				assert.Contains(t, err.Error(), want)
			}
			assert.Empty(t, stdout, "validation must fail before any command output")
		})
	}
}

func TestBogusSpecLeavesExemptCommandsAlone(t *testing.T) {
	badSpec := writeBadSpec(t)

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{
			name:     "help flag wins over bogus spec",
			args:     []string{"--spec", badSpec, "--help"},
			wantCode: ExitOK,
			wantOut:  "Usage:",
		},
		{
			name:     "version flag wins over bogus spec",
			args:     []string{"--spec", badSpec, "-v"},
			wantCode: ExitOK,
			wantOut:  "jiso version",
		},
		{
			name:     "help subcommand unaffected",
			args:     []string{"--spec", badSpec, "help", "scenario"},
			wantCode: ExitOK,
			wantOut:  "Manage and execute test scenarios",
		},
		{
			name:     "version subcommand unaffected",
			args:     []string{"--spec", badSpec, "version"},
			wantCode: ExitOK,
			wantOut:  "jiso version",
		},
		{
			name:     "completion unaffected",
			args:     []string{"--spec", badSpec, "completion", "bash"},
			wantCode: ExitOK,
			wantOut:  "complete",
		},
		{
			name:     "stress is a data command and fails on bogus spec",
			args:     []string{"--spec", badSpec, "stress", "--tx", "Purchase"},
			wantCode: ExitConfig,
			wantErr:  "failed to load spec",
		},
		{
			name:     "send is a data command and fails on bogus spec",
			args:     []string{"--spec", badSpec, "send", "Echo"},
			wantCode: ExitConfig,
			wantErr:  "failed to load spec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeRoot(t, tt.args...)

			assert.Equal(t, tt.wantCode, ExitCodeForError(err), "exit code for %v", err)
			if tt.wantOut != "" {
				assert.Contains(t, stdout, tt.wantOut)
			}
			if tt.wantErr != "" {
				assert.Contains(t, err.Error()+stderr, tt.wantErr)
			}
		})
	}
}

// TestREPLUnaffectedByBogusSpec pins that the removed repl command still
// short-circuits config validation (REL-604): a bogus --spec must not turn
// the removal notice (exit 2) into a config failure (exit 3).
func TestREPLUnaffectedByBogusSpec(t *testing.T) {
	stdout, stderr, err := executeRoot(t, "--spec", writeBadSpec(t), "repl")
	require.Error(t, err, "repl must fail as removed")
	assert.Equal(t, ExitUsage, ExitCodeForError(err), "repl must exit 2, not a config error")

	assert.Contains(t, stderr, replRemovedNotice)
	assert.Empty(t, stdout)
}

const oneTxJSON = `[{"type":"transaction","name":"auth","description":"auth test","fields":{"0":"0800"}}]`

// TestConfiguredSpecAndTxSurfacesErrors asserts the shared loader used by
// inspect/analyze returns the cached validated parse, names the file on a
// load error instead of swallowing it, and yields nil values for unset
// paths (M1 review #24).
func TestConfiguredSpecAndTxSurfacesErrors(t *testing.T) {
	t.Parallel()

	t.Run("load error surfaces as config error naming the file", func(t *testing.T) {
		resetConfig(t)
		badSpec := writeBadSpec(t)
		cfg.GetConfig().SetSpec(badSpec)

		_, _, err := configuredSpecAndTx()
		require.Error(t, err)

		var cfgErr *ExitConfigError
		require.True(t, errors.As(err, &cfgErr), "expected *ExitConfigError, got %T: %v", err, err)
		assert.Contains(t, err.Error(), badSpec)
	})

	t.Run("unset paths yield nil values without error", func(t *testing.T) {
		resetConfig(t)

		spec, tc, err := configuredSpecAndTx()
		require.NoError(t, err)
		assert.Nil(t, spec)
		assert.Nil(t, tc)
	})

	t.Run("valid paths return the cached collection", func(t *testing.T) {
		resetConfig(t)
		specPath := writeGoodSpec(t)
		txPath := writeTempFile(t, "tx.json", oneTxJSON)
		cfg.GetConfig().SetSpec(specPath)
		cfg.GetConfig().SetFile(txPath)

		spec, tc, err := configuredSpecAndTx()
		require.NoError(t, err)
		require.NotNil(t, spec)
		require.NotNil(t, tc)
	})
}

func TestValidSpecTxStillLoadForConsumers(t *testing.T) {
	stdout, stderr, err := executeRoot(t,
		"--spec", writeGoodSpec(t), "scenario", "list", "-f", writeTempFile(t, "tx.json", oneTxJSON))

	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, "No scenarios defined")
}
