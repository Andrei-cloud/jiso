package pages

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/palette"
)

// Golden harness (same os.WriteFile pattern as widgets/frame/theme):
// goldens pin raw bytes including escape sequences, so token or glyph
// changes show up as diffs.
var update = flag.Bool("update", false, "update golden files")

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui/pages -update)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s mismatch\nwant: %q\ngot:  %q", name, string(want), got)
	}
}

// goldens pin the dashboard body (no frame chrome) for the canonical
// states and both glyph/colour modes: the 120x32 wireframe baseline
// (empty / online) and the proposal-05 §3 grid at the three responsive
// bands (150x44 wide two-column, 110x36 medium, 80x32 narrow stack).
func TestDashboardGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state DashboardState
		w, h  int
	}{
		{"dashboard_empty", DashboardState{}, 120, 32},
		{"dashboard_online", onlineState(), 120, 32},
		{"dashboard_log150", logGoldState(), 150, 44},
		{"dashboard_log110", logGoldState(), 110, 36},
		{"dashboard_log80", logGoldState(), 80, 32},
	}
	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	for _, c := range cases {
		for _, p := range profiles {
			t.Run(c.name+"_"+p.name, func(t *testing.T) {
				d := NewDashboard(testTheme(t, p.prof))
				d.SetState(c.state)
				_, _ = d.Update(windowSize(c.w, c.h))
				checkGolden(t, c.name+"_"+p.name, d.View().Content)
			})
		}
	}
}

// logGoldState is the fully-live §A snapshot for the proposal-05 grid
// goldens: connected link, running mock server with a stats snapshot,
// completed send and stress run, known session stats, the raw root-
// stamped server-log ring (the same fixture shapes the §G log goldens
// use) and the real quick-action registry.
func logGoldState() DashboardState {
	retries := 3
	st := onlineState()
	st.HasConnection = true
	st.Conn.Retries = &retries
	st.Server = ServerCard{
		Running: true, Port: "9999", Header: "binary2", Uptime: 152 * time.Second,
		StatsKnown: true,
		Stats: StatsCard{
			Served: 415, Matched: 415, MatchPct: "100.0%",
			Fallback: 0, ReqErr: 0, LiveConns: 1,
		},
	}
	st.ServerLog = []string{
		"09:17:03 [SERVER] 🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)",
		"09:17:04 [SERVER] 🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)",
		"09:18:11 [SERVER] ⚠️ Fallback (No Route Match) for MTI 0200 -> Responding 0210 (RC: 12)",
		"09:18:12 [SERVER] 🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)",
	}
	st.Actions = palette.DashboardActions()

	return st
}
