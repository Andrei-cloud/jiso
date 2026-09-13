// root_workers_test.go proves the SCR-508 root contract: the table is
// driven ONLY by bridge bus events (start/progress/stopped fold into
// rows with no snapshot poll and no tick), `k` is never optimistic (the
// row flips terminal when WorkerStopped arrives, not when the stop Cmd
// runs), stop-all confirms while anything is active, quit confirms with
// live workers, the runtime tick arms only while the page is current
// with active rows, and a finished stress run opens the summary overlay.
package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
)

type workerTestRoot struct {
	m       *RootModel
	clock   time.Time
	mu      sync.Mutex
	stops   []string
	allStop int
	ticks   int
	lastDur time.Duration
}

func newWorkerTestRoot(t *testing.T) *workerTestRoot {
	t.Helper()

	r := &workerTestRoot{m: NewRootModel(nil), clock: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	r.m.now = func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.clock }
	r.m.workerStopFn = func(id string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.stops = append(r.stops, id)

		return nil
	}
	r.m.workerStopAllFn = func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.allStop++

		return nil
	}
	r.m.workerTickf = func(d time.Duration, send func() tea.Msg) tea.Cmd {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.ticks++
		r.lastDur = d

		return nil
	}
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 132, Height: 32})
	r.gotoPage()

	return r
}

// upd feeds one message through the root and keeps the model pointer.
func (r *workerTestRoot) upd(msg tea.Msg) tea.Cmd {
	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		panic("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

// key feeds a message and then runs the Cmd chain to completion — the
// page emits WorkersStopMsg, root answers with the App-call Cmd, whose
// result msg closes the loop (the program loop would pump these; the
// test has to as well).
func (r *workerTestRoot) key(msg tea.Msg) tea.Cmd {
	var last tea.Cmd
	for i := 0; i < 4; i++ {
		last = r.upd(msg)
		if last == nil {
			return nil
		}
		next := last()
		if next == nil {
			return last
		}
		msg = next
	}

	return last
}

func (r *workerTestRoot) gotoPage() { r.upd(ch('5')) }

func (r *workerTestRoot) bus(ev events.Event) { r.upd(bridge.Msg{Event: ev}) }

func (r *workerTestRoot) body() string { return r.m.View().Content }

func (r *workerTestRoot) rowStatus(t *testing.T, id string) string {
	t.Helper()
	for _, line := range strings.Split(r.body(), "\n") {
		if strings.Contains(line, id) && strings.Contains(line, "worker") == false {
			for _, s := range []string{"running", "ramping", "circuit-broke", "stopped", "done"} {
				if strings.Contains(line, s) {
					return s
				}
			}
		}
	}

	return ""
}

func TestWorkersRowArrivesFromBusAlone(t *testing.T) {
	r := newWorkerTestRoot(t)
	if strings.Contains(r.body(), "w-1") {
		t.Fatal("row existed before any event")
	}
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	if got := r.rowStatus(t, "w-1"); got != "running" {
		t.Fatalf("after WorkerStarted status = %q, want running\n%s", got, r.body())
	}
}

func TestWorkersProgressRendersEventTruth(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	r.advance(2 * time.Second)
	r.bus(events.WorkerProgress{ID: "w-1", Done: 12, Total: 100, Note: "ok=10 err=2"})

	body := r.body()
	if !strings.Contains(body, "10 / 2") {
		t.Fatalf("OK/FAIL must mirror the event note verbatim: %q", body)
	}
	if !strings.Contains(body, "12%") && !strings.Contains(body, "12/100") {
		t.Fatalf("progress row (12/100) missing: %q", body)
	}
	// No tick was needed to show the event.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ticks != 1 {
		t.Fatalf("arming tick(s) = %d, want the single initial arm", r.ticks)
	}
}

func TestWorkersProgressBeforeStartSeedsProvisional(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerProgress{ID: "w-0", Done: 3, Total: 9, Note: "ok=3 err=0"})
	if got := r.rowStatus(t, "w-0"); got != "running" {
		t.Fatalf("out-of-order progress must seed a provisional row, status = %q", got)
	}
}

func (r *workerTestRoot) advance(d time.Duration) {
	r.mu.Lock()
	r.clock = r.clock.Add(d)
	r.mu.Unlock()
}

func TestWorkersStoppedIsTheOnlyTerminal(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})

	// `k` dispatches the stop but must NOT flip the row.
	r.key(tea.KeyPressMsg{Code: 'k', Text: "k"})
	r.mu.Lock()
	n := len(r.stops)
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("stop fn calls = %d, want 1", n)
	}
	if got := r.rowStatus(t, "w-1"); got != "running" {
		t.Fatalf("`k` was optimistic: status = %q before WorkerStopped", got)
	}

	r.bus(events.WorkerStopped{ID: "w-1", Reason: "user stop"})
	if got := r.rowStatus(t, "w-1"); got != "stopped" {
		t.Fatalf("after WorkerStopped status = %q, want stopped", got)
	}
	// Terminal rows ignore further k.
	r.key(tea.KeyPressMsg{Code: 'k', Text: "k"})
	r.mu.Lock()
	n = len(r.stops)
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("terminal row re-stop: %d calls, want 1", n)
	}
}

func TestWorkersCircuitBrokeFromFailedReason(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerStarted{ID: "w-2", Kind: "stress"})
	r.bus(events.WorkerStopped{ID: "w-2", Reason: "failed: breaker tripped"})
	if got := r.rowStatus(t, "w-2"); got != "circuit-broke" {
		t.Fatalf("failed stop status = %q, want circuit-broke", got)
	}
}

func TestWorkersStopAllConfirmsWhileActive(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	r.key(tea.KeyPressMsg{Code: 'K', Text: "K"})
	if !strings.Contains(r.body(), "stop") || !strings.Contains(r.body(), "?") {
		t.Fatalf("K with active workers must open a confirm:\n%s", r.body())
	}
	r.key(tea.KeyPressMsg{Code: 'n', Text: "n"})
	r.mu.Lock()
	n := r.allStop
	r.mu.Unlock()
	if n != 0 || strings.Contains(r.body(), "confirm") {
		t.Fatalf("n must cancel: allStop=%d", n)
	}
	r.key(tea.KeyPressMsg{Code: 'K', Text: "K"})
	r.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	r.mu.Lock()
	n = r.allStop
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("y must stop all: %d calls", n)
	}
}

func TestWorkersStopAllWithoutActiveIsStatusLine(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	r.bus(events.WorkerStopped{ID: "w-1", Reason: "done"})
	r.key(tea.KeyPressMsg{Code: 'K', Text: "K"})
	r.mu.Lock()
	n := r.allStop
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("K with nothing active must not stop anything: %d", n)
	}
	if !strings.Contains(r.body(), "no active") {
		t.Fatalf("K with nothing active must say so:\n%s", r.body())
	}
}

func TestWorkersQuitConfirmsWithActiveWorkers(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	_, cmd := r.m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if isQuit(t, cmd) {
		t.Fatal("q quit straight away with an active worker")
	}
	c1 := r.upd(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if c1 == nil {
		t.Fatal("y on the confirm returned no cmd")
	}
	c2 := r.upd(c1())
	if !isQuit(t, c2) {
		t.Fatal("y on the confirm must quit")
	}

	r2 := newWorkerTestRoot(t)
	r2.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	if _, cmd := r2.m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); isQuit(t, cmd) {
		t.Fatal("q must confirm with an active worker")
	}
	r2.key(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if strings.Contains(r2.body(), "worker(s)?") {
		t.Fatal("n must dismiss the quit confirm")
	}
}

func TestWorkersNoActiveWorkersQuitsDirectly(t *testing.T) {
	t.Parallel()

	t.Skip("superseded by UAT: quit always confirms")
}

// TestQuitAlwaysConfirms: UAT — even with no workers, q opens the
// confirm modal (default No) and only y quits.
func TestQuitAlwaysConfirms(t *testing.T) {
	r := newWorkerTestRoot(t)
	_, cmd := r.m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if isQuit(t, cmd) {
		t.Fatal("q must confirm before quitting (UAT)")
	}
	if r.m.workersConfirm == nil {
		t.Fatal("quit confirmation modal must be armed")
	}
	r.key(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if r.m.workersConfirm != nil {
		t.Fatal("n must dismiss the confirmation")
	}
	_, cmd = r.m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if isQuit(t, cmd) {
		t.Fatal("q re-arms the confirm, not quit")
	}
	ycmd := r.upd(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m := ycmd(); m != nil {
		ycmd = r.upd(m)
	}
	if !isQuit(t, ycmd) {
		t.Fatal("y must confirm the quit")
	}
}

func TestWorkersTickArmedOnlyOnPageWithActive(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.mu.Lock()
	base := r.ticks
	r.mu.Unlock()
	if base != 0 {
		t.Fatalf("tick armed with no rows: %d", base)
	}
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	r.upd(workerRuntimeTickMsg{seq: 1}) // stale seq ignored
	r.mu.Lock()
	if r.ticks != 1 {
		t.Fatalf("arming count = %d, want 1", r.ticks)
	}
	r.mu.Unlock()

	r.upd(ch('1')) // leave the page
	r.mu.Lock()
	after := r.ticks
	r.mu.Unlock()
	r.bus(events.WorkerProgress{ID: "w-1", Done: 1, Total: 2, Note: "ok=1 err=0"})
	r.mu.Lock()
	if r.ticks != after {
		t.Fatalf("tick re-armed while page not current: %d -> %d", after, r.ticks)
	}
	r.mu.Unlock()
}

func TestWorkersStressSummaryOpensOverlay(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.m.workerSummaryFn = func(id string) (*app.StressSummary, error) {
		return &app.StressSummary{
			Sent: 100, Successful: 98, Failed: 2,
			MinLatencyMs: 0.4, MeanLatencyMs: 2.1, P99LatencyMs: 40.5,
			ResponseCodes: map[string]int{"00": 98, "96": 2},
		}, nil
	}
	r.bus(events.WorkerStarted{ID: "w-2", Kind: "stress"})
	r.bus(events.WorkerStopped{ID: "w-2", Reason: "done"})
	body := r.body()
	for _, want := range []string{"LATENCY MS", "RESPONSE CODES", "esc close"} {
		if !strings.Contains(body, want) {
			t.Fatalf("summary overlay lacks %q:\n%s", want, body)
		}
	}
	r.upd(tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(r.body(), "PERCENTILES") {
		t.Fatal("Esc must close the overlay")
	}
	_ = pages.WorkersPageID
}

func TestWorkersSparklineRingCapped(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.bus(events.WorkerStarted{ID: "w-1", Kind: "bgsend"})
	for i := 1; i <= 40; i++ {
		r.advance(time.Second)
		r.bus(events.WorkerProgress{ID: "w-1", Done: i * 5, Total: 1000, Note: "ok=5 err=0"})
	}
	if over := len(r.m.workerRing) - pages.SparkWidth; over > 0 {
		t.Fatalf("ring grew past the sparkline width by %d", over)
	}
	if len(r.m.workerRing) == 0 {
		t.Fatal("ring empty after 40 samples")
	}
}

func TestHumanBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{2202009, "2.1MB"},
		{10485760, "10MB"},
		{1073741824, "1.0GB"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.n); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestCircuitCell pins the §H CIRCUIT column: it reports a state, or it stays
// empty and the page dashes it. Every healthy row used to print "ok (0/10)",
// spending the widest column on the page on a counter that cannot say anything at
// zero -- and the denominator was a copy of a const from internal/app, so the
// column could state a limit that was no longer true.
func TestCircuitCell(t *testing.T) {
	t.Parallel()

	const limit = app.CircuitBreakerFailures

	cases := []struct {
		name string
		r    *workerRowState
		want string
	}{
		{"nothing to report", &workerRowState{consec: 0}, ""},
		{"accumulating", &workerRowState{consec: 4}, "4/10"},
		{"capped at the limit", &workerRowState{consec: 99}, "10/10"},
		{"tripped", &workerRowState{consec: limit, status: pages.StatusCircuitBroke}, "TRIPPED (10/10)"},
		// The trip word wins even if the count did not reach the limit (the
		// manager can break the circuit for a reason the counter does not show).
		{"tripped early", &workerRowState{consec: 2, status: pages.StatusCircuitBroke}, "TRIPPED (2/10)"},
	}

	for _, tc := range cases {
		if got := circuitCell(tc.r, limit); got != tc.want {
			t.Errorf("%s: circuitCell = %q, want %q", tc.name, got, tc.want)
		}
	}
}
