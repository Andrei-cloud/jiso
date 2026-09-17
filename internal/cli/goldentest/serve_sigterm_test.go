// serve_sigterm_test.go pins the graceful-stop contract with a
// dedicated exec test (NOT a golden file): the golden harness deliberately
// never signals its subprocess (see the harness header on SIGINT flakiness),
// so the JSON-purity-on-clean-stop shape is pinned here as well.
//
// Contract pinned: `serve start 0 --json --report <path>` under
// $JISO_STATE_DIR blocks, publishes the side-channel files, and on
// SIGTERM stops cleanly — exit 0, state/snapshot files removed, final
// ServerStats JSON in the report AND as the only stdout document.
package goldentest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/app"
)

// bootDeadline bounds PROCESS BOOT (exec + bind + state write), not the
// post-appearance settle the ticket caps at 200 ms; the settle sleep below
// stays well under that cap.
const bootDeadline = 5 * time.Second

// settleBeforeSignal is the settle between observing the state file and
// SIGTERM; the ticket caps it at 200 ms.
const settleBeforeSignal = 50 * time.Millisecond

func TestServeStartSIGTERMGracefulStop(t *testing.T) {
	t.Parallel()

	if buildErr != nil {
		t.Skipf("golden binary unavailable: %v", buildErr)
	}

	work := t.TempDir()
	stateDir := filepath.Join(work, "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))

	reportPath := filepath.Join(work, "final-stats.json")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath,
		"serve", "start", "0", "--json", "--report", reportPath)
	cmd.Dir = work
	cmd.Env = caseEnv(&goldenCase{Env: map[string]string{"JISO_STATE_DIR": stateDir}}, work)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	statePath := waitForStateFile(t, stateDir, cmd, &stderr)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })

	state := &app.ServeState{}
	raw, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, state))
	require.NotEmpty(t, state.Port)

	statsPath := strings.TrimSuffix(statePath, ".json") + ".stats.json"
	require.FileExists(t, statsPath, "the first snapshot is written inline at start")

	time.Sleep(settleBeforeSignal)
	require.NoError(t, syscall.Kill(cmd.Process.Pid, syscall.SIGTERM))

	// A 30 s ctx would SIGKILL a server that forgot to stop; Wait then
	// errors and the message below names the stall.
	err = cmd.Wait()
	require.NoError(t, err, "clean SIGTERM stop must exit 0 (stderr=%s)", stderr.String())

	assertStateFilesRemoved(t, statePath, statsPath)
	assert.Contains(t, stderr.String(), "stopping mock server", "stop notice belongs on stderr")

	final := readFinalStats(t, reportPath)
	assertFinalStatsView(t, final, state)
	assertStdoutIsPureFinalJSON(t, stdout.String(), final)
}

// waitForStateFile polls the state dir for serve-<port>.json (excluding the
// .stats.json sibling) until bootDeadline, failing with the captured stderr.
func waitForStateFile(t *testing.T, stateDir string, cmd *exec.Cmd, stderr *bytes.Buffer) string {
	t.Helper()

	deadline := time.Now().Add(bootDeadline)
	for {
		entries, _ := os.ReadDir(stateDir)
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, "serve-") && strings.HasSuffix(n, ".json") &&
				!strings.HasSuffix(n, ".stats.json") {
				return filepath.Join(stateDir, n)
			}
		}

		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("serve state file never appeared in %s within %v; stderr=%s",
				stateDir, bootDeadline, stderr.String())
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// assertStateFilesRemoved pins that the clean stop (StopServer -> side-
// channel stop) removed both files, so `serve stats` immediately
// reports no running server.
func assertStateFilesRemoved(t *testing.T, statePath, statsPath string) {
	t.Helper()

	for _, p := range []string{statePath, statsPath} {
		_, statErr := os.Stat(p)
		assert.True(t, errors.Is(statErr, os.ErrNotExist),
			"clean stop must remove %s (stat err: %v)", p, statErr)
	}
}

func readFinalStats(t *testing.T, reportPath string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(reportPath)
	require.NoError(t, err, "--report file must be written on clean stop")

	view := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &view), "report must be valid JSON")

	return view
}

func assertFinalStatsView(t *testing.T, view map[string]any, state *app.ServeState) {
	t.Helper()

	assert.Equal(t, false, view["running"], "the report is captured after the stop")
	assert.Equal(t, state.Port, view["port"], "ephemeral port resolves to the bound port")
	assert.Equal(t, "binary2", view["header_type"])
	assert.Equal(t, float64(state.PID), view["pid"])
	assert.Contains(t, view, "uptime")
	assert.Contains(t, view, "total_served")
}

// assertStdoutIsPureFinalJSON pins --json during serve start: the WHOLE
// stdout is exactly one JSON document (nothing was written while the
// server ran) and it carries the same summary as the report.
func assertStdoutIsPureFinalJSON(t *testing.T, stdout string, report map[string]any) {
	t.Helper()

	require.NotEmpty(t, stdout, "--json must print the final stats on clean stop")
	assert.NotContains(t, stdout, "\x1b", "no ANSI decoration in JSON mode")

	view := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &view),
		"stdout must be exactly one JSON document")

	assert.Equal(t, report["port"], view["port"])
	assert.Equal(t, false, view["running"])
	assert.Equal(t, report["total_served"], view["total_served"])
}
