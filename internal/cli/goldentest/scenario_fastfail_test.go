// scenario_fastfail_test.go is the exec-probe: `jiso scenario run`
// must validate the scenario name and the target BEFORE any connect
// attempt. The UAT repro (`scenario run NoSuchScenario` with no target)
// dialed ":0" through ~7 s of backoff and exited 1; per the exit-code
// taxonomy pinned across E1 it must exit 3 (unknown scenario, named) or 2
// (target not configured) in well under a second, with empty stdout.
package goldentest

import (
	"os"
	"strings"
	"testing"
	"time"
)

// fastFailProbeExit runs args in the isolated work dir and returns the
// captured streams, exit code, and wall time of the exec.
func fastFailProbeExit(t *testing.T, args ...string) (stdout, stderr string, code int, elapsed time.Duration) {
	t.Helper()

	if buildErr != nil {
		t.Skipf("golden harness skipped: %v", buildErr)
	}
	if fixtureErr != nil {
		t.Skipf("golden harness skipped: %v", fixtureErr)
	}

	work, err := os.MkdirTemp("", "jiso-golden-case-*")
	if err != nil {
		t.Fatalf("case workdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if err := copyFixtures(work); err != nil {
		t.Fatalf("copy fixtures: %v", err)
	}

	started := time.Now()
	stdout, stderr, code = runBinary(t, &goldenCase{Args: args}, work)
	elapsed = time.Since(started)

	return stdout, stderr, code, elapsed
}

// TestScenarioRunUnknownNameFastFailsExit3 pins the repro: unknown
// scenario + unset target exits 3 naming the scenario (cf. analyze
// --flow-unknown → 3), in under a second, stdout empty.
func TestScenarioRunUnknownNameFastFailsExit3(t *testing.T) {
	t.Parallel()

	stdout, stderr, code, elapsed := fastFailProbeExit(t,
		"scenario", "run", "NoSuchScenario", "-f", "tx.json", "-s", "spec.json")

	if code != 3 {
		t.Errorf("exit code: got %d, want 3 (unknown scenario is a config-class error)", code)
	}
	if elapsed >= time.Second {
		t.Errorf("elapsed %v, want < 1s: the command must not dial or back off before validating", elapsed)
	}
	if stdout != "" {
		t.Errorf("stdout must be empty, got %q", stdout)
	}
	if !strings.Contains(stderr, "NoSuchScenario") {
		t.Errorf("stderr must name the unknown scenario, got %q", stderr)
	}
	if strings.Contains(stderr, "Retrying connection") {
		t.Errorf("stderr shows a connect/backoff attempt for an unknown scenario: %q", stderr)
	}
}

// TestScenarioRunKnownNameNoTargetFastFailsExit2 pins the target guard: a
// known scenario with no host/port exits 2 naming the missing target
// (missingTargetMessage parity with `send`), in under a second.
func TestScenarioRunKnownNameNoTargetFastFailsExit2(t *testing.T) {
	t.Parallel()

	stdout, stderr, code, elapsed := fastFailProbeExit(t,
		"scenario", "run", "Smoke", "-f", "tx.json", "-s", "spec.json")

	if code != 2 {
		t.Errorf("exit code: got %d, want 2 (unset target is a usage error, cf. send)", code)
	}
	if elapsed >= time.Second {
		t.Errorf("elapsed %v, want < 1s: no target must mean no dial", elapsed)
	}
	if stdout != "" {
		t.Errorf("stdout must be empty, got %q", stdout)
	}
	if !strings.Contains(stderr, "host and port are not configured") {
		t.Errorf("stderr must name the missing target, got %q", stderr)
	}
	if strings.Contains(stderr, "Retrying connection") {
		t.Errorf("stderr shows a connect/backoff attempt without a target: %q", stderr)
	}
}
