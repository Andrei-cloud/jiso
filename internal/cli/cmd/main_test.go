package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"jiso/internal/utils"
)

// TestMain makes the package hermetic for every test in it.
//
// NewRootCmd's PersistentPreRunE calls userconfig.Load, which resolves
// $JISO_CONFIG and otherwise falls back to os.UserConfigDir (on macOS
// ~/Library/Application Support/jiso/config.yaml). Tests that never opted into
// a user config therefore inherited whatever the developer's machine held: a
// config with `spec: specs/flex.json` failed TestBareInvocationUsageHint and
// TestInitDryRunWritesNothing, because that relative path is resolved against
// the package directory. The same leak let serve state land in the real
// ~/.local/state/jiso.
//
// isolateConfig clears this set per test; hoisting the identical clearance to
// the process is what covers the tests that never call it. A test that needs a
// specific value still sets its own with t.Setenv, which shadows this baseline
// and restores it on cleanup.
func TestMain(m *testing.M) {
	processStateDir = mustTempDir()
	dir := processStateDir

	for _, k := range jisoEnvVars {
		if k == "JISO_CONFIG" {
			continue
		}
		mustSetenv(k, "")
	}
	// A path that does not exist: userconfig.Load reports an empty File
	// instead of reading the developer's config.
	mustSetenv("JISO_CONFIG", filepath.Join(dir, "absent-config.yaml"))
	mustSetenv("JISO_STATE_DIR", filepath.Join(dir, "state"))

	code := m.Run()

	// Stop the STAN persistence worker before removing the directory it writes
	// into. GetCounter starts that worker on first use and JISO_STATE_DIR above
	// points it inside dir; cmd/main.go defers the same call, but nothing here
	// did, so the worker outlived m.Run() and re-created dir/state after the
	// cleanup below - which is how every run of this package left a temp
	// directory behind.
	utils.StopPersistWorker()
	removeProcessStateDir()

	if err := os.RemoveAll(dir); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "test main: cleanup %s: %v\n", dir, err)
	}
	os.Exit(code)
}

// mustSetenv pins one environment variable for the whole test process. A
// failure here would leave the suite reading the developer's real state - the
// exact condition this file exists to prevent - so it stops the run rather than
// reporting a green that proves nothing.
func mustSetenv(key, value string) {
	if err := os.Setenv(key, value); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "test main: setenv %s: %v\n", key, err)
		os.Exit(1)
	}
}

// processStateDir is the temp dir TestMain created for this process, and
// removeProcessStateDir is the exit-path-independent version of the cleanup
// TestMain cannot guarantee: a re-exec probe child ends its test function with
// os.Exit, which skips TestMain's cleanup and every t.Cleanup, leaving the
// directory (and the state/ the STAN worker made inside it) behind on every run.
// The child calls this before os.Exit; TestMain calls it on the normal path, and
// RemoveAll on a removed directory is not an error, so the two do not fight.
var processStateDir string

func removeProcessStateDir() {
	if processStateDir != "" {
		_ = os.RemoveAll(processStateDir)
	}
}

func mustTempDir() string {
	dir, err := os.MkdirTemp("", "jiso-cmd-test")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "test main: temp dir: %v\n", err)
		os.Exit(1)
	}
	return dir
}
