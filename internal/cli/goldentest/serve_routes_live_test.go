// serve_routes_live_test.go is the UAT-02 exec-probe: `jiso serve routes`
// must list the routes the RUNNING server actually matches against (the
// route set persisted in the PAR-304 state file at start), not whatever
// spec/tx slots the querying process happens to carry. Before the fix the
// command re-derived routes from its own config and printed "No mock routes
// configured" while the server was actively matching tx-file routes.
package goldentest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"jiso/internal/app"
	"jiso/internal/server"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// probeRoutesTxJSON mirrors the UAT repro's tx file: transactions plus two
// mock routes the server must match (and `serve routes` must list).
const probeRoutesTxJSON = `[
  {"type": "transaction", "name": "Echo", "description": "Network Management: Echo", "fields": {"0": "0800", "7": "auto", "11": "auto", "70": 301}},
  {"type": "mock_route", "name": "probe-echo-route", "match_fields": {"0": "0800"}, "response_mti": "0810"},
  {"type": "mock_route", "name": "probe-purchase-route", "match_fields": {"0": "0200"}, "response_mti": "0210", "latency_ms": 12, "jitter_ms": 3}
]`

// startRoutesProbeServer boots an in-process mock server WITH the probe
// routes and publishes its PAR-304 side-channel into a dedicated state dir.
// It returns the state dir (cases override $JISO_STATE_DIR with it) and the
// bound port.
func startRoutesProbeServer(t *testing.T) (stateDir, port string) {
	t.Helper()

	if buildErr != nil {
		t.Skipf("golden harness skipped: %v", buildErr)
	}
	if fixtureErr != nil {
		t.Skipf("golden harness skipped: %v", fixtureErr)
	}

	spec, err := utils.CreateSpecFromFile(filepath.Join(fixtureDir, "spec.json"))
	if err != nil {
		t.Fatalf("fixture spec: %v", err)
	}

	work := t.TempDir()
	txPath := filepath.Join(work, "tx-routes.json")
	if err := os.WriteFile(txPath, []byte(probeRoutesTxJSON), 0o600); err != nil {
		t.Fatalf("write probe tx file: %v", err)
	}

	tc, err := transactions.NewTransactionCollection(txPath, spec)
	if err != nil {
		t.Fatalf("probe tx collection: %v", err)
	}

	routes := tc.GetMockRoutes()
	if len(routes) != 2 {
		t.Fatalf("probe routes loaded = %d, want 2", len(routes))
	}

	srv := server.NewServer(spec, routes, "ascii4")
	if err := srv.Start("0"); err != nil {
		t.Fatalf("probe server start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	port, err = srv.BoundPort()
	if err != nil {
		t.Fatalf("probe server BoundPort: %v", err)
	}

	stateDir = t.TempDir()
	t.Setenv(app.ServeStateDirEnv, stateDir)

	_, stop, err := app.StartServeSideChannel(srv, "127.0.0.1", "", routes, app.ServeStatsRefreshInterval)
	if err != nil {
		t.Fatalf("probe side channel: %v", err)
	}
	t.Cleanup(func() { _ = stop() })

	return stateDir, port
}

// probeCase builds a golden-harness case pinned to the probe server's state
// dir, so `serve routes` resolves the probe server (not the TestMain one).
func probeCase(stateDir string, args ...string) *goldenCase {
	return &goldenCase{
		Args: args,
		Env:  map[string]string{app.ServeStateDirEnv: stateDir},
	}
}

// TestServeRoutesLiveListsLoadedRoutes pins UAT-02: against a live server
// started WITH tx-file mock routes, `serve routes -p <port>` lists exactly
// the loaded route names, exit 0, and never prints the "No mock routes
// configured" lie.
func TestServeRoutesLiveListsLoadedRoutes(t *testing.T) {
	stateDir, port := startRoutesProbeServer(t)

	work, err := os.MkdirTemp("", "jiso-golden-case-*")
	if err != nil {
		t.Fatalf("case workdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if err := copyFixtures(work); err != nil {
		t.Fatalf("copy fixtures: %v", err)
	}

	stdout, stderr, code := runBinary(t, probeCase(stateDir, "serve", "routes", "-p", port), work)

	if code != 0 {
		t.Errorf("exit code: got %d, want 0 (stderr %q)", code, stderr)
	}
	if strings.Contains(stdout, "No mock routes configured") {
		t.Errorf("stdout carries the UAT-02 lie while routes are live:\n%s", stdout)
	}
	for _, name := range []string{"probe-echo-route", "probe-purchase-route"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("stdout missing live route %q:\n--- stdout ---\n%s", name, stdout)
		}
	}
}

// TestServeRoutesLiveJSONPinsRouteSet pins the --json view: stdout is a pure
// JSON array of the route set the running server loaded (names + match +
// response MTI), with zero ANSI decoration.
func TestServeRoutesLiveJSONPinsRouteSet(t *testing.T) {
	stateDir, port := startRoutesProbeServer(t)

	work, err := os.MkdirTemp("", "jiso-golden-case-*")
	if err != nil {
		t.Fatalf("case workdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if err := copyFixtures(work); err != nil {
		t.Fatalf("copy fixtures: %v", err)
	}

	stdout, stderr, code := runBinary(t, probeCase(stateDir, "serve", "routes", "-p", port, "--json"), work)

	if code != 0 {
		t.Fatalf("exit code: got %d, want 0 (stderr %q)", code, stderr)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Errorf("--json stdout carries ANSI bytes: %q", stdout)
	}

	var routes []struct {
		Name        string `json:"name"`
		ResponseMTI string `json:"response_mti"`
	}
	if err := json.Unmarshal([]byte(stdout), &routes); err != nil {
		t.Fatalf("--json stdout is not a pure JSON array: %v\n%s", err, stdout)
	}

	got := make([]string, 0, len(routes))
	for _, r := range routes {
		got = append(got, r.Name)
	}
	sort.Strings(got)

	want := []string{"probe-echo-route", "probe-purchase-route"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("json route names: got %v, want %v", got, want)
	}
}
