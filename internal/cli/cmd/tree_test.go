package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "jiso/internal/config"
)

func TestBareInvocationUsageHint(t *testing.T) {
	cfg.GetConfig().Reset()
	rootCmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs([]string{})

	err := rootCmd.Execute()
	require.NoError(t, err)

	hint := out.String()
	assert.Contains(t, hint, "run 'jiso tui' for interactive mode")
	assert.Contains(t, hint, "run 'jiso --help' for details")
	assert.Contains(t, hint, "Commands:")
	for _, name := range []string{"inspect", "db", "send", "stress", "tui", "connect"} {
		assert.Contains(t, hint, name)
	}
}

func TestTreeResolves(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"inspect", []string{"inspect"}, "inspect"},
		{"db parent", []string{"db"}, "db"},
		{"db stats", []string{"db", "stats"}, "stats"},
		{"connect parent", []string{"connect"}, "connect"},
		{"connect check", []string{"connect", "check"}, "check"},
		{"send", []string{"send"}, "send"},
		{"stress", []string{"stress"}, "stress"},
		{"tui", []string{"tui"}, "tui"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rootCmd := NewRootCmd()
			found, _, err := rootCmd.Find(tc.args)
			require.NoError(t, err)
			require.NotNil(t, found)
			assert.Equal(t, tc.want, found.Name())
		})
	}
}

func TestStubCommandsExit2(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		ticket string
	}{
		// The tui stub's contract lives in
		// TestTUIRequiresTerminalExit2 below; the stress missing --tx
		// usage error is pinned in exit_test.go and the golden
		// harness.
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg.GetConfig().Reset()
			rootCmd := NewRootCmd()
			var out, errBuf bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errBuf)
			rootCmd.SetArgs(tc.args)

			err := rootCmd.Execute()
			require.Error(t, err)

			var exitErr *ExitCodeError
			require.True(t, errors.As(err, &exitErr), "expected *ExitCodeError, got %T: %v", err)
			assert.Equal(t, ExitUsage, exitErr.Code)
			assert.Equal(t, 2, exitErr.Code)
			assert.Contains(t, errBuf.String(), "not implemented yet (planned: "+tc.ticket+")")
			assert.Empty(t, out.String(), "stub output must go to stderr, not stdout")
		})
	}
}

// TestTUIRequiresTerminalExit2 pins the contract: `jiso tui` without
// a TTY fails as a usage error (exit 2) naming the requirement, before the
// Bubble Tea program could ever launch. In-process the go tool always
// captures the test binary's stdout through a pipe, so the guard is
// deterministic here; the exec'd-binary contract is pinned by the
// tui-requires-tty-exit-2 golden.
func TestTUIRequiresTerminalExit2(t *testing.T) {
	cfg.GetConfig().Reset()
	rootCmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs([]string{"tui"})

	err := rootCmd.Execute()
	require.Error(t, err)

	var exitErr *ExitCodeError
	require.True(t, errors.As(err, &exitErr), "expected *ExitCodeError, got %T: %v", err)
	assert.Equal(t, ExitUsage, exitErr.Code)
	assert.Equal(t, 2, exitErr.Code)
	assert.Contains(t, errBuf.String(), "requires an interactive terminal")
	assert.Empty(t, out.String(), "guard output must go to stderr, not stdout")
}

func TestSShortShorthandRemoved(t *testing.T) {
	rootCmd := NewRootCmd()

	analyzeCmd, _, err := rootCmd.Find([]string{"analyze"})
	require.NoError(t, err)
	scenario := analyzeCmd.Flags().Lookup("scenario")
	require.NotNil(t, scenario)
	assert.Empty(t, scenario.Shorthand, "analyze --scenario must be long-only")

	ctfExport, _, err := rootCmd.Find([]string{"ctf", "export"})
	require.NoError(t, err)
	sessionID := ctfExport.Flags().Lookup("session-id")
	require.NotNil(t, sessionID)
	assert.Empty(t, sessionID.Shorthand, "ctf export --session-id must be long-only")
}
