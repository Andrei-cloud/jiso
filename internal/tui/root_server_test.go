// root_server_test.go covers the §G root contract (SCR-507): the tick
// lifecycle (armed only while the page is current AND the server runs;
// stale in-flight ticks ignored via the seq token), the frozen stats
// snapshot after stop, and the stop paths (direct at 0 live conns,
// widgets.ConfirmDialog otherwise — y stops, n/Esc keep running, and
// nothing ever auto-restarts).
package tui

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// serveTestRoot wires a real app with all four serve legs faked plus a
// fake clock and a recording tick scheduler.
type serveTestRoot struct {
	m      *RootModel
	tx     string
	routes []config.MockRouteConfig

	mu       sync.Mutex
	stats    *app.ServerStats
	startErr error
	stopErr  error
	starts   int
	stops    int
	last     serveStartCall
	ticks    []func() tea.Msg // serverTickf recordings (mk senders)
}

type serveStartCall struct{ port, header, spec, txPath, routesFile string }

func serveFixtureStats() *app.ServerStats {
	return &app.ServerStats{
		Uptime:            42*time.Minute + 11*time.Second,
		ActiveConnections: 3,
		TotalServed:       1204,
		Matched:           1198,
		Dropped:           2,
		RequestErrors:     2,
		RouteCounts: map[string]int64{
			app.ServeFallbackRoute: 4, "0200/proc": 812, "0800/nmc": 380,
		},
	}
}

func serveFixtureRoutes() []config.MockRouteConfig {
	return []config.MockRouteConfig{
		{
			Name: "0200/proc", Description: "purchase auth", MatchFields: map[string]any{"11": "000000"},
			RequiredFields: []string{"11"}, EchoFields: []int{11}, ResponseMTI: "0210",
			ResponseFields: map[string]any{"0": "0210"}, DelayMs: 100, JitterMs: 25,
		},
		{Name: "0800/nmc", ResponseMTI: "0810", LatencyMs: 0},
		{
			Name: "0200/proc-mc", Description: "drops connection", MatchFields: map[string]any{"11": "000000"},
			ResponseMTI: "0210", DropConnection: true, DelayMs: 50,
		},
	}
}

func newServeTestRoot(t *testing.T) *serveTestRoot {
	t.Helper()

	// Hermetic state dir: starts stamp last-server-start.json and the
	// forms read it; tests must never touch the real XDG state dir.
	t.Setenv("JISO_STATE_DIR", t.TempDir())

	txFile := t.TempDir() + "/pool.json"
	if err := os.WriteFile(txFile, []byte(txFixtureJSON), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}
	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	cfg.SetSpec("../../specs/spec.json")
	cfg.SetFile(txFile)

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	time.Sleep(20 * time.Millisecond)

	r := &serveTestRoot{m: NewRootModel(a), tx: txFile, routes: serveFixtureRoutes()}
	clock := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	r.m.now = func() time.Time { return clock }
	r.m.serveStartFn = func(port, header, spec, txPath, routesFile string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.starts++
		r.last = serveStartCall{port, header, spec, txPath, routesFile}

		return r.startErr
	}
	r.m.serveStopFn = func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.stops++

		return r.stopErr
	}
	r.m.serveStatsFn = func() *app.ServerStats {
		r.mu.Lock()
		defer r.mu.Unlock()
		s := *r.stats

		return &s
	}
	r.m.serveRoutesFn = func() []config.MockRouteConfig { return r.routes }
	r.m.serverTickf = func(d time.Duration, mk func() tea.Msg) tea.Cmd {
		if d != serverTickInterval {
			t.Errorf("tick interval = %v, want %v", d, serverTickInterval)
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.ticks = append(r.ticks, mk)

		return nil
	}
	r.stats = serveFixtureStats()

	return r
}

// run delivers a Cmd's message into Update and keeps following the
// resulting Cmd chain (page msg → handler cmd → result msg → …) until it
// goes quiet; the fake tickf returns nil, so chains terminate.
func (r *serveTestRoot) run(cmd tea.Cmd) {
	for depth := 0; cmd != nil && depth < 8; depth++ {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = r.m.Update(msg)
	}
}

// key sends a printable key and runs the returned Cmd.
func (r *serveTestRoot) key(c rune) {
	_, cmd := r.m.Update(ch(c))
	r.run(cmd)
}

func (r *serveTestRoot) startCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.starts
}

func (r *serveTestRoot) stopCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.stops
}

func (r *serveTestRoot) tickCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.ticks)
}

// tick delivers the nth recorded tick's message.
func (r *serveTestRoot) tick(i int) {
	r.mu.Lock()
	mk := r.ticks[i]
	r.mu.Unlock()
	_, _ = r.m.Update(mk())
}

// goPage enters §G and stamps a successful start (fake leg returns the
// fixture stats), leaving the model exactly as a happy start would.
func (r *serveTestRoot) goPage(t *testing.T) {
	t.Helper()

	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	if id := r.m.Current().ID(); id != pages.ServerPageID {
		t.Fatalf("current page = %q, want %q", id, pages.ServerPageID)
	}
	r.key('c')
	if r.m.serverDlg == nil {
		t.Fatal("c did not open the server start form")
	}
	_, cmd := r.m.Update(special(tea.KeyEnter))
	r.run(cmd)
	if !r.m.serverRunning() {
		t.Fatal("enter did not start the server")
	}
}

func TestRootServerSlotAndJump(t *testing.T) {
	m := NewRootModel(nil)
	if _, ok := m.registry[3].(*pages.Server); !ok {
		t.Fatalf("registry slot 4 = %T, want *pages.Server", m.registry[3])
	}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('4'))
	wantStack(t, m, "server")
}

func TestRootServerTickArmedOnlyActiveAndRunning(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)
	// Arming happened on the start-result Update while the page was
	// current; one tick is in flight.
	if n := r.tickCount(); n != 1 {
		t.Fatalf("tick arming on active+running = %d, want 1", n)
	}
	// Leaving to a page that renders NO server snapshot disarms; a
	// stale in-flight tick is ignored (UAT round 5: the §A dashboard is
	// a snapshot consumer too, so the leave case must use §B here).
	before := r.m.serverTickSeq
	r.key('2')
	if r.m.serverTickSeq != before+1 {
		t.Fatalf("seq after leave = %d, want %d", r.m.serverTickSeq, before+1)
	}
	r.tick(0) // stale: armed under the previous seq
	if r.m.serverTickWait {
		t.Fatal("stale tick re-armed the wait flag")
	}
	if got := r.m.serverSnap; got == nil || got.TotalServed != 1204 {
		t.Fatalf("running snapshot wrong: %+v", got)
	}
}

// TestRootServerTickArmedOnDashboard: UAT round 5 — the §A dashboard's
// MOCK SERVER card renders the live snapshot (conns included), so the
// poll must stay armed while the operator sits on the dashboard: moving
// §G -> §A must not disarm, and the fold must refresh the snapshot the
// card reads (the reported bug: conns live, card frozen at 0).
func TestRootServerTickArmedOnDashboard(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)
	before := r.m.serverTickSeq

	r.key('1') // §G -> §A dashboard: a snapshot consumer, must NOT disarm

	if r.m.serverTickSeq != before {
		t.Fatalf("seq after moving to the dashboard = %d, want %d (a disarm freezes the card's conns)",
			r.m.serverTickSeq, before)
	}
	if !r.m.serverTickWait {
		t.Fatal("the dashboard must keep the stats tick in flight")
	}

	r.mu.Lock()
	r.stats.ActiveConnections = 4
	r.mu.Unlock()
	r.tick(0) // the in-flight tick, armed under the current seq

	if got := r.m.serverSnap; got == nil || got.ActiveConnections != 4 {
		t.Fatalf("dashboard tick fold = %+v, want ActiveConnections 4", got)
	}
}

func TestRootServerTickNotArmedWhenStopped(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4') // page current, server never started
	if n := r.tickCount(); n != 0 {
		t.Fatalf("tick armed while stopped: %d", n)
	}
}

func TestRootServerFreshTickUpdatesSnapshot(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)
	r.mu.Lock()
	r.stats.TotalServed = 1300
	r.mu.Unlock()
	r.tick(0) // current seq: folds in
	if got := r.m.serverSnap.TotalServed; got != 1300 {
		t.Fatalf("snapshot after fresh tick = %d, want 1300", got)
	}
}

func TestRootServerStatsFrozenAfterStop(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)
	r.mu.Lock()
	r.stats.ActiveConnections = 0
	r.mu.Unlock()
	r.tick(0)  // fold the 0-conn truth into the live snapshot
	r.key('s') // direct stop (no live conns)
	if r.stopCount() != 1 {
		t.Fatalf("stops = %d, want 1", r.stopCount())
	}
	r.mu.Lock()
	r.stats.TotalServed = 9999 // post-stop truth must NOT reach the card
	r.mu.Unlock()
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	if got := r.m.serverSnap.TotalServed; got != 1204 {
		t.Fatalf("stats drifted after stop: %d, want frozen 1204", got)
	}
	if n := r.tickCount(); n != 2 { // armed twice pre-stop, never again after
		t.Fatalf("tick count after stop = %d, want 2", n)
	}
}

func TestRootServerNoAutoRestartAfterStop(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)
	r.mu.Lock()
	r.stats.ActiveConnections = 0
	r.mu.Unlock()
	r.tick(0)
	r.key('s')
	atStop := r.tickCount()
	time.Sleep(120 * time.Millisecond) // quiet: no retry, no re-arm
	if r.startCount() != 1 || r.tickCount() != atStop {
		t.Fatalf("auto-restart: starts=%d ticks=%d, want 1/%d", r.startCount(), r.tickCount(), atStop)
	}
}

func TestRootServerStopConfirmYesStops(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)
	r.key('s') // 3 live conns → confirm first
	if r.m.serverConfirm == nil || !r.m.serverConfirm.Pending() {
		t.Fatal("stop with live conns must open the confirm dialog")
	}
	if r.stopCount() != 0 {
		t.Fatal("stop ran before confirmation")
	}
	_, cmd := r.m.Update(ch('y'))
	r.run(cmd) // ConfirmedMsg → performStop → stop result
	if r.stopCount() != 1 || r.m.serverRunning() {
		t.Fatalf("after y: stops=%d running=%v", r.stopCount(), r.m.serverRunning())
	}
}

func TestRootServerStopConfirmNoAndEscKeepRunning(t *testing.T) {
	for _, k := range []rune{'n'} {
		r := newServeTestRoot(t)
		r.goPage(t)
		r.key('s')
		_, cmd := r.m.Update(ch(k))
		r.run(cmd)
		if r.stopCount() != 0 || !r.m.serverRunning() {
			t.Fatalf("%q: stops=%d running=%v", k, r.stopCount(), r.m.serverRunning())
		}
		if r.m.serverConfirm != nil {
			t.Fatal("confirm dialog not cleared on cancel")
		}
	}
	r := newServeTestRoot(t)
	r.goPage(t)
	r.key('s')
	_, cmd := r.m.Update(special(tea.KeyEsc))
	r.run(cmd)
	if r.stopCount() != 0 || !r.m.serverRunning() {
		t.Fatalf("esc: stops=%d running=%v", r.stopCount(), r.m.serverRunning())
	}
}

func TestRootServerStopIgnoredWhileStopped(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('s')
	if r.stopCount() != 0 {
		t.Fatalf("s while stopped %d times", r.stopCount())
	}
}

func TestRootServerStateDerivation(t *testing.T) {
	if got := matchPct(0, 0); got != "" {
		t.Errorf("matchPct(0,0) = %q, want unknown", got)
	}
	if got := matchPct(1198, 1204); got != "99.5%" {
		t.Errorf("matchPct(1198,1204) = %q, want 99.5%%", got)
	}
	routes := serveFixtureRoutes()
	if got := routeMatchCell(routes[1]); got != "0800/nmc any" {
		t.Errorf("catch-all match cell = %q", got)
	}
	if got := routeLatencyCell(routes[0]); !strings.Contains(got, "100") || !strings.Contains(got, "25ms") {
		t.Errorf("jitter latency cell = %q, want 100±25ms", got)
	}
	if got := routeBaseDelayMs(config.MockRouteConfig{LatencyMs: 30}); got != 30 {
		t.Errorf("latency_ms alias fallback = %d, want 30", got)
	}
}

// --- E5-FIX/M4 regression tests --------------------------------------

// TestRootServerStopConfirmReSnapshots: after re-entering §G the tick
// snapshot can be stale (0 conns while conns are live); `s` must
// re-snapshot BEFORE the confirm decision — reading the stale cache
// stopped WITH live connections and no confirm (fail-open).
func TestRootServerStopConfirmReSnapshots(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)

	r.m.serverSnap = &app.ServerStats{TotalServed: 1204} // stale: 0 live conns
	r.mu.Lock()
	r.stats.ActiveConnections = 2 // the engine truth: conns are live
	r.mu.Unlock()

	r.key('s')
	if r.m.serverConfirm == nil || !r.m.serverConfirm.Pending() {
		t.Fatal("stop decision must re-snapshot: live conns must open the confirm even when the cache says 0")
	}
	if r.stopCount() != 0 {
		t.Fatal("stop ran before the confirm decision")
	}
}

// TestRootServerStopErrorKeepsRunning: a FAILED stop must not clear the
// running truth — the engine is still up; only a successful stop flips
// serverStartAt to zero.
func TestRootServerStopErrorKeepsRunning(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)

	r.mu.Lock()
	r.stats.ActiveConnections = 0
	r.stopErr = errors.New("stop refused by engine")
	r.mu.Unlock()
	r.tick(0) // fold the 0-conn truth → direct (no-confirm) stop path

	r.key('s')
	if r.stopCount() != 1 {
		t.Fatalf("stops = %d, want 1", r.stopCount())
	}
	if r.m.serverStartAt.IsZero() {
		t.Fatal("a failed stop must keep the running truth (serverStartAt)")
	}
	if !strings.Contains(r.m.serverError, "stop refused") {
		t.Fatalf("stop error = %q, want it named", r.m.serverError)
	}
}
