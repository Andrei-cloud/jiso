// root_disconnect_test.go proves the root contract (closes
// REGRESSION-1): the palette action and the §A "D" quick key both land on
// handleDisconnect; with a live connection and no workers/serve the leg
// runs App.Disconnect exactly ONCE with no confirm and the card flips via
// the SAME bus-event path the bridge uses (App.Disconnect publishes the
// Disconnected event — the fold itself only toasts); with workers active
// or the serve engine running the §N3 confirm opens first (n cancels, y
// disconnects); without a connection the action is a sane no-op with an
// info toast and no panic; a leg orphaned by a page leave (seq bumped) is
// dropped with the wait flag cleared BEFORE the stale return (the wedge
// class); and the palette lists the command.
package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/palette"
)

type disconnectTestRoot struct {
	m     *RootModel
	clock time.Time
	mu    sync.Mutex
	calls int
	t     *testing.T
}

func newDisconnectTestRoot(t *testing.T) *disconnectTestRoot {
	t.Helper()

	r := &disconnectTestRoot{
		m:     NewRootModel(nil),
		clock: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		t:     t,
	}
	r.m.now = func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.clock }
	r.m.disconnectFn = func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls++

		return nil
	}
	// The §A dashboard now arms the mock-server stats tick
	// while the server runs (the dashboard renders the snapshot). Record
	// the arming instead of scheduling a real tea.Tick — the serveTestRoot
	// convention; this chain follower feeds one message at a time and
	// would have to expand tea.Batch's BatchMsg exactly like the real
	// event loop to follow page cmds past a live tick.
	r.m.serverTickf = func(time.Duration, func() tea.Msg) tea.Cmd { return nil }
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	return r
}

// upd feeds one message through the root and keeps the model pointer.
func (r *disconnectTestRoot) upd(msg tea.Msg) tea.Cmd {
	t := r.t
	t.Helper()

	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		t.Fatal("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

// key feeds a message and runs the Cmd chain to completion (the page
// emits palette.DisconnectMsg, the leg Cmd calls the seam, its result msg
// closes the loop — the program pump's job, done by hand here).
func (r *disconnectTestRoot) key(msg tea.Msg) {
	r.t.Helper()

	r.updChain(msg)
}

func (r *disconnectTestRoot) updChain(msg tea.Msg) tea.Cmd {
	r.t.Helper()

	var last tea.Cmd
	for i := 0; i < 5; i++ {
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

func (r *disconnectTestRoot) bus(ev events.Event) { r.upd(bridge.Msg{Event: ev}) }

func (r *disconnectTestRoot) connect() {
	r.bus(events.ConnectionEvent{State: events.StateConnected, Detail: "10.0.0.5:8080"})
}

func (r *disconnectTestRoot) body() string { return r.m.View().Content }

func (r *disconnectTestRoot) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls
}

// toastText joins the rendered toast lines (the widget's tests-facing
// accessor; theme styling wraps the text, the text itself is verbatim).
func (r *disconnectTestRoot) toastText() string {
	if r.m.toast == nil {
		return ""
	}

	return strings.Join(r.m.toast.Lines(), "\n")
}

func (r *disconnectTestRoot) wantToastContains(t *testing.T, want string) {
	t.Helper()

	if txt := r.toastText(); !strings.Contains(txt, want) {
		t.Fatalf("toast = %q, want it to contain %q", txt, want)
	}
}

// (a) live connection, no workers/serve: no confirm, the seam (fake of
// App.Disconnect) is called exactly once, and the connection card flips
// through the SAME ConnectionEvent path the bridge uses — the fold itself
// never writes the card.
func TestDisconnectLiveConnectionNoConfirmCallsOnce(t *testing.T) {
	r := newDisconnectTestRoot(t)

	if want := "Disconnect"; strings.Contains(r.body(), want) {
		t.Fatalf("quick-actions list must hide %q without a connection:\n%s", want, r.body())
	}

	r.connect()

	if !r.m.dashboardState().HasConnection {
		t.Fatal("DashboardState.HasConnection must be true while connected")
	}
	if want := "Disconnect"; !strings.Contains(r.body(), want) {
		t.Fatalf("quick-actions list must show %q with a connection:\n%s", want, r.body())
	}

	r.key(ch('D'))

	if got := r.callCount(); got != 1 {
		t.Fatalf("App.Disconnect seam calls = %d, want 1", got)
	}
	if r.m.disconnectConfirm != nil {
		t.Fatal("no §N3 confirm may open without workers or the serve engine")
	}
	if r.m.disconnectWait {
		t.Fatal("wait flag must be cleared when the result folds")
	}
	r.wantToastContains(t, "disconnected")

	// The card flip is the bus event's job (App.Disconnect publishes
	// StateDisconnected; updateBridgeMsg owns conn). Feed exactly that
	// event — the same message a live App would send — and the card
	// follows; nothing double-rendered it before it arrived.
	if strings.Contains(r.body(), "OFFLINE") {
		t.Fatal("card must not flip before the Disconnected event arrives")
	}
	r.bus(events.ConnectionEvent{State: events.StateDisconnected, Detail: "10.0.0.5:8080"})
	if !strings.Contains(r.body(), "OFFLINE") {
		t.Fatalf("card must show OFFLINE after the Disconnected event:\n%s", r.body())
	}
	if r.m.dashboardState().HasConnection {
		t.Fatal("HasConnection must be false after the disconnect event")
	}
}

// (b) workers active: the §N3 confirm opens (default No) — n disconnects
// nothing, y runs the leg.
func TestDisconnectConfirmsWhileWorkersActive(t *testing.T) {
	r := newDisconnectTestRoot(t)
	r.connect()
	r.bus(events.WorkerStarted{ID: "w1", Kind: "bgsend"})

	r.key(ch('D'))

	if r.m.disconnectConfirm == nil || !r.m.disconnectConfirm.Pending() {
		t.Fatal("§N3 confirm must open while workers are active")
	}
	if q := r.m.disconnectConfirm.Question(); !strings.Contains(q, "1 active worker(s)") {
		t.Fatalf("confirm question = %q, want it to name the active worker", q)
	}
	if got := r.callCount(); got != 0 {
		t.Fatalf("seam must not be called while the confirm is pending, got %d", got)
	}

	r.key(ch('n'))

	if r.m.disconnectConfirm != nil {
		t.Fatal("n must close the confirm")
	}
	if got := r.callCount(); got != 0 {
		t.Fatalf("n must NOT disconnect: seam calls = %d, want 0", got)
	}

	r.key(ch('D'))
	if r.m.disconnectConfirm == nil || !r.m.disconnectConfirm.Pending() {
		t.Fatal("the second D must reopen the confirm")
	}
	r.key(ch('y'))

	if r.m.disconnectConfirm != nil {
		t.Fatal("y must close the confirm")
	}
	if got := r.callCount(); got != 1 {
		t.Fatalf("y must run the leg: seam calls = %d, want 1", got)
	}
	if r.m.disconnectWait {
		t.Fatal("wait flag must be cleared when the confirmed result folds")
	}
}

// (b, serve variant) the serve engine running trips the same §N3 guard —
// the same liveness check root_server.go uses (serverRunning).
func TestDisconnectConfirmsWhileServeEngineRunning(t *testing.T) {
	t.Parallel()

	r := newDisconnectTestRoot(t)
	r.connect()
	r.m.serverStartAt = r.clock // the §G running truth serverRunning reads
	r.m.serverPort = "8080"

	r.key(ch('D'))

	if r.m.disconnectConfirm == nil || !r.m.disconnectConfirm.Pending() {
		t.Fatal("§N3 confirm must open while the serve engine runs")
	}
	if q := r.m.disconnectConfirm.Question(); !strings.Contains(q, "mock server :8080 running") {
		t.Fatalf("confirm question = %q, want it to name the running server", q)
	}
	r.key(ch('n'))

	if got := r.callCount(); got != 0 {
		t.Fatalf("n must NOT disconnect: seam calls = %d, want 0", got)
	}
}

// (c) already disconnected: the action is a no-op with an info toast — no
// panic, no seam call, nothing left pending.
func TestDisconnectWithoutConnectionIsSaneNoop(t *testing.T) {
	r := newDisconnectTestRoot(t)

	r.key(ch('D'))

	if got := r.callCount(); got != 0 {
		t.Fatalf("seam calls = %d, want 0 (no connection)", got)
	}
	if r.m.disconnectWait {
		t.Fatal("the no-op must not arm the wait flag")
	}
	if r.m.disconnectConfirm != nil {
		t.Fatal("the no-op must not open a confirm")
	}
	r.wantToastContains(t, "no active connection")

	// Still sane after a connected→disconnected round-trip (the event,
	// not the app, is the freshest truth).
	r.connect()
	r.key(ch('D'))
	r.bus(events.ConnectionEvent{State: events.StateDisconnected, Detail: "10.0.0.5:8080"})
	r.key(ch('D')) // second D after the card went offline: sane info no-op
	r.wantToastContains(t, "no active connection — nothing to disconnect")

	if got := r.callCount(); got != 1 {
		t.Fatalf("seam calls after disconnecting = %d, want 1 (second D is a no-op)", got)
	}
}

// (d) the wedge class: a leg orphaned by a page leave (leaveDisconnect
// bumps the seq) must be dropped with the wait flag cleared BEFORE the
// stale return — a stale result can never leave the action in flight.
func TestDisconnectStaleLegAfterPageLeaveIsDropped(t *testing.T) {
	t.Parallel()

	r := newDisconnectTestRoot(t)
	r.connect()

	// Arm the leg but keep it in flight: process the page's
	// DisconnectMsg (arms wait + the leg Cmd), do NOT run that Cmd.
	pageCmd := r.upd(ch('D'))
	if pageCmd == nil {
		t.Fatal("the D key must dispatch the page's DisconnectMsg")
	}
	legCmd := r.upd(pageCmd())
	if legCmd == nil {
		t.Fatal("DisconnectMsg must return the leg Cmd")
	}
	if !r.m.disconnectWait {
		t.Fatal("the armed leg must hold the wait flag")
	}
	staleSeq := r.m.disconnectSeq

	// Page-leave: hotkey 2 replaces the stack; leaveDisconnect bumps the
	// seq and clears the wait flag.
	r.upd(ch('2'))

	if r.m.disconnectSeq != staleSeq+1 {
		t.Fatalf("page leave must bump the disconnect seq: got %d, want %d", r.m.disconnectSeq, staleSeq+1)
	}
	if r.m.disconnectWait {
		t.Fatal("page leave must clear the wait flag")
	}

	// The orphaned leg completes and its (stale) result arrives.
	stale := legCmd()
	if msg, ok := stale.(disconnectResultMsg); !ok || msg.seq != staleSeq {
		t.Fatalf("leg Cmd must yield the seq-tokened result, got %#v", stale)
	}
	r.upd(stale)

	if r.m.disconnectWait {
		t.Fatal("stale result must fold with the wait flag cleared (wedge class)")
	}
	if txt := r.toastText(); strings.Contains(txt, "disconnected") {
		t.Fatalf("stale result must fold silently, toast = %q", txt)
	}
	if r.m.dashboardState().HasConnection != true {
		t.Fatal("a dropped stale leg must not fake the connection card")
	}
}

// (e) the palette lists the Disconnect command and its Run carries the
// router Msg; the §A quick action dispatches the very same Msg.
func TestPaletteListsDisconnectCommand(t *testing.T) {
	t.Parallel()

	var act palette.Action
	for _, a := range palette.Seed().Actions() {
		if a.ID == "disconnect" {
			act = a

			break
		}
	}
	if act.ID != "disconnect" {
		t.Fatal("palette.Seed() must register the disconnect command")
	}
	if act.Title != "Disconnect" {
		t.Fatalf("action title = %q, want %q", act.Title, "Disconnect")
	}
	if _, ok := act.Run(nil).(palette.DisconnectMsg); !ok {
		t.Fatalf("disconnect Run msg = %T, want palette.DisconnectMsg", act.Run(nil))
	}

	// The matcher resolves it, and Enter on the seeded §A row emits the
	// same Msg the palette action does.
	if got := palette.SeedMatcher().Search("disconnect", 0); len(got) == 0 || got[0].ID != "disconnect" {
		t.Fatalf("search \"disconnect\": %v, want disconnect first", got)
	}

	// While a connection is live the §A quick-actions list toggles: the
	// Connect row is replaced by Disconnect on top; Enter
	// on it dispatches the very same Msg the palette action carries.
	r := newDisconnectTestRoot(t)
	r.connect()

	cmd := r.upd(special(tea.KeyEnter)) // cursor starts on the top row
	if cmd == nil {
		t.Fatal("Enter on the disconnect row must dispatch its Msg")
	}
	if _, ok := cmd().(palette.DisconnectMsg); !ok {
		t.Fatalf("quick-action dispatch = %T, want palette.DisconnectMsg", cmd())
	}
}
