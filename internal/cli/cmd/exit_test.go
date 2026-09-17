package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "jiso/internal/config"
	"jiso/internal/utils"
)

func resetConfig(t *testing.T) {
	t.Helper()

	cfg.GetConfig().Reset()
	t.Cleanup(func() { cfg.GetConfig().Reset() })
}

func TestExitCodeForError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"explicit code", &ExitCodeError{Code: ExitTestFailure}, ExitTestFailure},
		{"explicit usage code", &ExitCodeError{Code: ExitUsage}, ExitUsage},
		{"config error", &ExitConfigError{Path: "spec.json", Err: errors.New("failed to load spec")}, ExitConfig},
		{"wrapped config error", fmt.Errorf("outer: %w", &ExitConfigError{Err: errors.New("bad timeout")}), ExitConfig},
		{"canceled context", context.Canceled, ExitSIGINT},
		{"wrapped canceled context", fmt.Errorf("run: %w", context.Canceled), ExitSIGINT},
		{"generic failure", errors.New("connection refused"), ExitError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExitCodeForError(tt.err))
		})
	}
}

func TestExitConfigErrorNamesFile(t *testing.T) {
	tests := []struct {
		name     string
		err      *ExitConfigError
		contains string
		dupes    bool
	}{
		{
			name:     "path appended when cause lacks it",
			err:      &ExitConfigError{Path: "nope.json", Err: errors.New("unparseable spec")},
			contains: `unparseable spec: nope.json`,
		},
		{
			name:     "path not duplicated when cause names it",
			err:      &ExitConfigError{Path: "nope.json", Err: fmt.Errorf("opening file nope.json: %w", errors.New("no such file"))},
			contains: "nope.json",
			dupes:    false,
		},
		{
			name:     "empty path keeps cause",
			err:      &ExitConfigError{Err: errors.New("spec file does not exist: nope.json")},
			contains: "nope.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			assert.Contains(t, msg, tt.contains)
			if !tt.dupes {
				assert.Equal(t, 1, bytes.Count([]byte(msg), []byte("nope.json")), "file must be named exactly once")
			}
			assert.ErrorIs(t, tt.err, tt.err.Err)
		})
	}
}

// TestExitConfigErrorNilErrDoesNotPanic asserts a value-only
// ExitConfigError (no wrapped Err) renders without dereferencing nil and
// still maps to the config exit code.
func TestExitConfigErrorNilErrDoesNotPanic(t *testing.T) {
	tests := []struct {
		name string
		err  *ExitConfigError
		want string
	}{
		{"path only", &ExitConfigError{Path: "solo.cfg"}, "config error: solo.cfg"},
		{"no path no err", &ExitConfigError{}, "config error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() { assert.Equal(t, tt.want, tt.err.Error()) })
			assert.Equal(t, ExitConfig, ExitCodeForError(tt.err))
		})
	}
}

func TestHelpPrintsStdoutAndExitsZero(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"root long", []string{"--help"}},
		{"root short", []string{"-h"}},
		{"group command", []string{"scenario", "--help"}},
		{"leaf command", []string{"scenario", "run", "--help"}},
		{"help topic", []string{"help", "scenario"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetConfig(t)

			rootCmd := NewRootCmd()
			var out, errBuf bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errBuf)
			rootCmd.SetArgs(tt.args)

			err := rootCmd.Execute()
			require.NoError(t, err, "--help must exit 0")
			assert.Equal(t, ExitOK, ExitCodeForError(err))

			assert.Contains(t, out.String(), "Usage:")
			assert.Empty(t, errBuf.String(), "help must print to stdout, not stderr")
		})
	}
}

func TestUsageErrorsExit2(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantErrSnippet string
	}{
		{"unknown long flag", []string{"--bogus"}, "unknown flag: --bogus"},
		{"unknown shorthand flag", []string{"-Q"}, "unknown shorthand flag"},
		{"subcommand unknown flag", []string{"scenario", "run", "--bogus"}, "unknown flag: --bogus"},
		{"flag missing value", []string{"scenario", "run", "--report"}, "flag needs an argument"},
		{"invalid flag value", []string{"--hex=nope"}, "invalid argument"},
		{"unknown command", []string{"nope-cmd"}, "unknown command"},
		{"too many args", []string{"scenario", "run", "one", "two"}, "accepts at most"},
		{"stress unknown flag", []string{"stress", "--bogus"}, "unknown flag: --bogus"},
		{"send missing tx arg", []string{"send"}, "accepts 1 arg(s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetConfig(t)

			rootCmd := NewRootCmd()
			var out, errBuf bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errBuf)
			rootCmd.SetArgs(tt.args)

			err := rootCmd.Execute()
			require.Error(t, err)
			assert.Equal(t, ExitUsage, ExitCodeForError(err))
			assert.Contains(t, err.Error()+errBuf.String(), tt.wantErrSnippet)
		})
	}
}

// TestDbStatsTxInvalidIDExitsUsage asserts a non-numeric or missing
// transaction ID under `db stats tx` is a usage error (exit 2), not a
// runtime failure.
func TestDbStatsTxInvalidIDExitsUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"non-numeric", []string{"db", "stats", "tx", "abc"}},
		{"missing id", []string{"db", "stats", "tx"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stdout, stderr, err := runPrecedence(t, append(tt.args, "--db", filepath.Join(t.TempDir(), "x.db"))...)

			require.Error(t, err)
			assert.Equal(t, ExitUsage, ExitCodeForError(err), "exit code for %v", err)
			assert.Contains(t, stderr, "invalid transaction ID")
			assert.Empty(t, stdout)
		})
	}
}

func TestScenarioRunMissingSpecFileExitsConfig(t *testing.T) {
	resetConfig(t)

	rootCmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs([]string{"scenario", "run", "--spec", "nope.json"})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Equal(t, ExitConfig, ExitCodeForError(err))

	var cfgErr *ExitConfigError
	require.True(t, errors.As(err, &cfgErr), "expected *ExitConfigError, got %T: %v", err, err)
	assert.Contains(t, err.Error(), "nope.json", "error must name the file")
	assert.Empty(t, out.String())
}

// TestMissingSpecExit3NamesSpecNotUserConfig asserts: an exit-3
// file-existence failure names the offending file in the ExitConfigError,
// never the unrelated user-config path pointed at by JISO_CONFIG.
func TestMissingSpecExit3NamesSpecNotUserConfig(t *testing.T) {
	resetConfig(t)

	ucPath := filepath.Join(t.TempDir(), "absent-config.yaml")
	t.Setenv("JISO_CONFIG", ucPath)

	rootCmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs([]string{"scenario", "run", "--dry-run", "--spec", "missing-spec.json", "--file", "tx.json"})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Equal(t, ExitConfig, ExitCodeForError(err))

	var cfgErr *ExitConfigError
	require.True(t, errors.As(err, &cfgErr), "expected *ExitConfigError, got %T: %v", err, err)
	assert.Equal(t, "missing-spec.json", cfgErr.Path, "Path must be the offending spec file")

	combined := err.Error() + errBuf.String()
	assert.Contains(t, combined, "spec file does not exist: missing-spec.json")
	assert.NotContains(t, combined, ucPath)
	assert.NotContains(t, combined, "absent-config.yaml")
	assert.Empty(t, out.String())
}

func TestScenarioRunMissingSpecFlagExitsUsage(t *testing.T) {
	resetConfig(t)

	rootCmd := NewRootCmd()
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"scenario", "run"})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Equal(t, ExitUsage, ExitCodeForError(err))
	assert.Contains(t, err.Error(), "spec file is required")
}

func TestSIGINTExits130(t *testing.T) {
	if os.Getenv("JISO_SIGINT_PROBE") == "1" {
		root := NewRootCmd()
		root.AddCommand(&cobra.Command{
			Use: "hang",
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ready")
				<-make(chan struct{})

				return nil
			},
		})
		root.SetArgs([]string{"hang"})
		require.NoError(t, root.Execute())
		os.Exit(ExitOK)
	}

	probe := exec.Command(os.Args[0], "-test.run=^TestSIGINTExits130$", "-test.timeout=20s")
	probe.Env = append(os.Environ(), "JISO_SIGINT_PROBE=1")

	stdout, err := probe.StdoutPipe()
	require.NoError(t, err)

	var stderr bytes.Buffer
	probe.Stderr = &stderr

	require.NoError(t, probe.Start())
	t.Cleanup(func() {
		if probe.Process != nil {
			_ = probe.Process.Kill()
		}
	})

	scanner := bufio.NewScanner(stdout)
	require.True(t, scanner.Scan(), "child must report ready")
	assert.Equal(t, "ready", scanner.Text())

	require.NoError(t, syscall.Kill(probe.Process.Pid, syscall.SIGINT))

	err = probe.Wait()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, ExitSIGINT, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), "received signal interrupt, exiting")
	assert.NotContains(t, stderr.String(), "goroutine", "must exit cleanly without a stack trace")
}

// TestScenarioRunJSONFailureExits4 is an exec-probe for the CLI-103
// test-failure contract (M1 review #2/#14/#19): a scenario whose steps fail
// against a black-hole server must exit 4, with stdout carrying pure JSON
// (no \x1b) under --json. The child re-enters this test with
// JISO_EXIT4_PROBE=1 and os.Exit's with the mapped code, so the asserted
// exit code is real.
func TestScenarioRunJSONFailureExits4(t *testing.T) {
	if os.Getenv("JISO_EXIT4_PROBE") == "1" {
		// The child ends this function with os.Exit, which skips t.Cleanup, so
		// anything created from here on would outlive the test. It is given the
		// parent's fixture paths instead of writing fixtures of its own, and it
		// removes the directory TestMain made for this process before leaving.
		root := NewRootCmd()
		root.SetArgs([]string{
			"scenario", "run", "Sign On", "--json",
			"--spec", os.Getenv("JISO_PROBE_SPEC"),
			"--file", os.Getenv("JISO_PROBE_TX"),
			"--host", "127.0.0.1",
			"--port", os.Getenv("JISO_PROBE_PORT"),
			"--response-timeout", "300ms",
		})
		code := ExitCodeForError(root.Execute())
		// Stop the STAN worker first: it persists on its own schedule, and a
		// write that lands after the removal re-creates state/ inside a
		// directory nothing will clean up again.
		utils.StopPersistWorker()
		removeProcessStateDir()
		os.Exit(code)
	}

	f := writeCLI102Fixtures(t)

	// Black-hole server: accepts connections, reads, never responds — the
	// scenario step fails on the response timeout.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, aErr := ln.Accept()
			if aErr != nil {
				return
			}
			go func() {
				_, _ = io.Copy(io.Discard, conn)
				_ = conn.Close()
			}()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	env := append(os.Environ(),
		"JISO_EXIT4_PROBE=1",
		"JISO_PROBE_SPEC="+f.specPath,
		"JISO_PROBE_TX="+f.txPath,
		"JISO_PROBE_PORT="+port,
	)
	for _, k := range jisoEnvVars {
		env = append(env, k+"=")
	}
	env = append(env, "JISO_CONFIG="+filepath.Join(t.TempDir(), "absent-config.yaml"))

	probe := exec.Command(os.Args[0], "-test.run=^TestScenarioRunJSONFailureExits4$", "-test.timeout=30s")
	probe.Env = env

	stdout, err := probe.StdoutPipe()
	require.NoError(t, err)

	var stderr bytes.Buffer
	probe.Stderr = &stderr

	require.NoError(t, probe.Start())
	t.Cleanup(func() {
		if probe.Process != nil {
			_ = probe.Process.Kill()
		}
	})

	outBytes, err := io.ReadAll(stdout)
	require.NoError(t, err)

	waitErr := probe.Wait()
	var exitErr *exec.ExitError
	require.ErrorAs(t, waitErr, &exitErr)
	assert.Equal(t, ExitTestFailure, exitErr.ExitCode(), "stderr=%s stdout=%s", stderr.String(), string(outBytes))

	assert.NotContains(t, string(outBytes), "\x1b", "stdout under --json must carry no colors")

	var report map[string]any
	require.NoError(t, json.Unmarshal(outBytes, &report), "stdout must be pure parseable JSON: %q", string(outBytes))
	assert.Equal(t, "Sign On", report["scenario_name"])
	assert.Equal(t, false, report["success"], "failed steps must mark the JSON report failed")

	assert.Contains(t, stderr.String(), "scenario failed")
}
