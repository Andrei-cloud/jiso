package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CLI-106 stdout/stderr audit: stdout carries machine output only. On the
// success paths that run without a live server, stderr must be empty with
// debug off, and must contain nothing but `debug: ...` lines when
// JISO_DEBUG=1.

func TestStderrEmptyOnSuccessPaths(t *testing.T) {
	isolateConfig(t)
	f := writeCLI102Fixtures(t)
	dbPath := seedCLI102DB(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "bare usage hint",
			args: nil,
			want: "run 'jiso tui' for interactive mode",
		},
		{
			name: "db stats list",
			args: []string{"db", "stats", "list", "--db", dbPath},
			want: cli102SessionID,
		},
		{
			name: "db stats overview",
			args: []string{"db", "stats", cli102SessionID, "--db", dbPath},
			want: "Database Statistics for Session: " + cli102SessionID,
		},
		{
			name: "db stats tx",
			args: []string{"db", "stats", "tx", "1", "--db", dbPath},
			want: "TRANSACTION RETROSPECTIVE REVIEW [ID: 1]",
		},
		{
			name: "inspect json",
			args: []string{"inspect", "Purchase", "--json", "--spec", f.specPath, "--file", f.txPath},
			want: `"name": "Purchase"`,
		},
		{
			name: "inspect human",
			args: []string{"inspect", "Purchase", "--spec", f.specPath, "--file", f.txPath},
			want: "Name: Purchase",
		},
		{
			name: "scenario list",
			args: []string{"scenario", "list", "--spec", f.specPath, "--file", f.txPath},
			want: "Available Scenarios:",
		},
		{
			name: "stress dry-run",
			args: []string{"stress", "--tx", "Purchase", "--dry-run", "--json", "--spec", f.specPath, "--file", f.txPath},
			want: `"dry_run": true`,
		},
		{
			name: "ctf list",
			args: []string{"ctf", "list", "--db", dbPath},
			want: "No recorded sessions with Visa transactions found.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runCLI102(t, tt.args...)
			require.NoError(t, err)

			assert.Contains(t, stdout, tt.want)
			assert.Empty(t, stderr, "success path must keep stderr empty (debug off)")
		})
	}
}

func TestStubCommandsKeepStdoutEmpty(t *testing.T) {
	isolateConfig(t)
	f := writeCLI102Fixtures(t)

	// The shape this test pins (usage failure keeps stdout empty, the
	// message goes to stderr) now applies to the implemented stress
	// command's missing --tx path; the tui suite's own no-TTY guard
	// is pinned by TestTUIRequiresTerminalExit2 and the golden
	// harness.
	tests := [][]string{
		{"stress", "--spec", f.specPath, "--file", f.txPath},
		{"stress", "--spec", f.specPath, "--file", f.txPath, "--json"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, err := runCLI102(t, args...)
			require.Error(t, err)
			assert.Equal(t, ExitUsage, ExitCodeForError(err))

			assert.Empty(t, stdout, "usage failure must keep stdout empty; the message goes to stderr")
			assert.Contains(t, stderr, "--tx is required")
		})
	}
}

func TestDebugLinesOnlyOnStderrWhenJISODebug(t *testing.T) {
	isolateConfig(t)
	dbPath := seedCLI102DB(t)

	t.Run("debug off stderr empty", func(t *testing.T) {
		_, stderr, err := runCLI102(t, "db", "stats", "list", "--db", dbPath)
		require.NoError(t, err)
		assert.Empty(t, stderr)
	})

	t.Run("debug on stderr only debug lines", func(t *testing.T) {
		t.Setenv("JISO_DEBUG", "1")

		_, stderr, err := runCLI102(t, "db", "stats", "list", "--db", dbPath)
		require.NoError(t, err)
		require.NotEmpty(t, stderr, "JISO_DEBUG=1 must emit debug lines on stderr")

		for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
			assert.True(t, strings.HasPrefix(line, "debug: "),
				"stderr must contain debug lines only, got %q", line)
		}
	})
}
