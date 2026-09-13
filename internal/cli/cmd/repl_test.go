package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// REL-604: the legacy REPL loop is gone. `jiso repl` prints a one-line
// removal notice to stderr (CLI-102 routing: stdout is machine output and
// stays pure under --json), suppressible with -q/--quiet, and exits 2
// (command unavailable in the E1 exit taxonomy). --help stays exit 0.

func runREPLRemovedCLI(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	isolateConfig(t)
	resetConfig(t)
	for k, v := range env {
		t.Setenv(k, v)
	}

	rootCmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(args)

	err = rootCmd.Execute()

	return out.String(), errBuf.String(), err
}

func TestREPLRemovedNoticeOnStderrExit2(t *testing.T) {
	stdout, stderr, err := runREPLRemovedCLI(t, nil, "repl")
	require.Error(t, err)
	assert.Equal(t, ExitUsage, ExitCodeForError(err), "removed command must exit 2")

	assert.Empty(t, stdout, "notice must not pollute stdout")
	assert.Contains(t, stderr, replRemovedNotice)
}

func TestREPLRemovedNoticeSuppressedByQuiet(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"quiet flag", nil, []string{"repl", "-q"}},
		{"quiet long flag", nil, []string{"repl", "--quiet"}},
		{"quiet env", map[string]string{"JISO_QUIET": "1"}, []string{"repl"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runREPLRemovedCLI(t, tt.env, tt.args...)
			require.Error(t, err, "-q must suppress the notice, not the exit code")
			assert.Equal(t, ExitUsage, ExitCodeForError(err))

			assert.NotContains(t, stderr, replRemovedNotice)
			assert.Empty(t, stdout)
		})
	}
}

func TestREPLRemovedNoticeJSONStdoutStaysClean(t *testing.T) {
	stdout, stderr, err := runREPLRemovedCLI(t, nil, "repl", "--json")
	require.Error(t, err)
	assert.Equal(t, ExitUsage, ExitCodeForError(err))

	assert.Empty(t, stdout, "--json stdout must stay pure; the notice goes to stderr")
	assert.Contains(t, stderr, replRemovedNotice)
}

func TestREPLRemovedNoticeHelpUnaffected(t *testing.T) {
	for _, args := range [][]string{{"repl", "--help"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, err := runREPLRemovedCLI(t, nil, args...)
			require.NoError(t, err, "--help must exit 0")

			assert.Contains(t, stdout, "repl")
			assert.NotContains(t, stderr, replRemovedNotice)
		})
	}
}
