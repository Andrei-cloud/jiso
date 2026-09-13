// root_connect_test.go covers the §E root-side contract: the dialog is an
// overlay that never touches the page stack (Esc returns to the SAME
// page), the Enabled flags are root-computed data (station ID iff visa,
// target/bind per mode), the keyboard belongs to the overlay, and the
// attempt loop honours config reconnect-attempts, stamps the progress
// line, cancels on Esc, and NEVER auto-reconnects after the final failure
// (call-counter pinned).
package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/tui/pages"
)

// connectTestRoot wires a real app with the dial leg faked: the collector
// receives the goroutine's attempt/result msgs exactly like program.Send
// would (the sendTestRoot idiom), and the clock is fake.
type connectTestRoot struct {
	m     *RootModel
	col   chan tea.Msg
	clock time.Time
}

func newConnectTestRoot(t *testing.T) *connectTestRoot {
	t.Helper()

	m := NewRootModel(newTxFileApp(t))
	col := make(chan tea.Msg, 64)
	m.SetConnectSender(func(msg tea.Msg) { col <- msg })
	clock := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }

	return &connectTestRoot{m: m, col: col, clock: clock}
}

// next drains the next connect-loop msg (failing on a quiet channel).
func (r *connectTestRoot) next(t *testing.T) tea.Msg {
	t.Helper()

	select {
	case msg := <-r.col:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("connect goroutine delivered no msg")

		return nil
	}
}

// wantQuiet asserts the loop stays silent (no auto-reconnect, ever).
func (r *connectTestRoot) wantQuiet(t *testing.T) {
	t.Helper()

	select {
	case msg := <-r.col:
		t.Fatalf("unexpected extra msg %v after terminal result", msg)
	case <-time.After(120 * time.Millisecond):
	}
}

func (r *connectTestRoot) pump(t *testing.T, msg tea.Msg) {
	t.Helper()

	_, _ = r.m.Update(msg)
}

// openHotkey presses the global c and fails if the overlay did not open.
func (r *connectTestRoot) openHotkey(t *testing.T) {
	t.Helper()

	_, _ = r.m.Update(ch('c'))
	if r.m.dlg == nil {
		t.Fatal("c did not open the connect dialog")
	}
}

func (r *connectTestRoot) state(t *testing.T) pages.ConnectFormState {
	t.Helper()

	if r.m.dlg == nil {
		t.Fatal("dialog closed")
	}

	return r.m.dlg.State()
}

// drainTo pumps loop msgs until the first ConnectResultMsg, returning it
// plus the attempt msgs seen (in order).
func (r *connectTestRoot) drainToResult(t *testing.T) ([]ConnectAttemptMsg, ConnectResultMsg) {
	t.Helper()

	var attempts []ConnectAttemptMsg
	for {
		switch msg := r.next(t).(type) {
		case ConnectAttemptMsg:
			attempts = append(attempts, msg)
			r.pump(t, msg)
		case ConnectResultMsg:
			r.pump(t, msg)

			return attempts, msg
		default:
			t.Fatalf("unexpected msg %T on connect channel", msg)
		}
	}
}

func TestConnectHotkeyIsOverlayNotPage(t *testing.T) {
	r := newConnectTestRoot(t)
	r.openHotkey(t)
	wantStack(t, r.m, "dashboard")

	view := r.m.View()
	if !strings.Contains(view.Content, "CONNECT") || !strings.Contains(view.Content, "[Enter] connect") {
		t.Fatalf("overlay not composed into the frame:\n%s", view.Content)
	}

	_, _ = r.m.Update(special(tea.KeyEscape))
	if r.m.dlg != nil {
		t.Fatal("esc did not close the dialog")
	}
	wantStack(t, r.m, "dashboard")
}

func TestConnectEscReturnsSamePageFromDeepStack(t *testing.T) {
	r := newConnectTestRoot(t)
	_, _ = r.m.Update(ch('2')) // transactions page
	top := r.m.Current().ID()
	r.openHotkey(t)
	wantStack(t, r.m, "transactions") // overlay must not push
	if depth := r.m.StackDepth(); depth != 1 {
		t.Fatalf("overlay pushed the stack to depth %d", depth)
	}
	_, _ = r.m.Update(special(tea.KeyEscape))
	if got := r.m.Current().ID(); got != top {
		t.Fatalf("after esc current=%q, want %q", got, top)
	}
}

func TestConnectDialogOwnsKeyboard(t *testing.T) {
	r := newConnectTestRoot(t)
	r.openHotkey(t)

	_, cmd := r.m.Update(ch('q'))
	if isQuit(t, cmd) {
		t.Fatal("q quit while the dialog owns the keyboard")
	}
	if r.m.dlg == nil {
		t.Fatal("q closed the dialog")
	}
	_, _ = r.m.Update(ch('2'))
	wantStack(t, r.m, "dashboard") // digit must not jump pages

	_, cmd = r.m.Update(mod('c', tea.ModCtrl))
	if !isQuit(t, cmd) {
		t.Fatal("ctrl+c must stay global even over the overlay")
	}
}

func TestStationIDEnabledIffVisa(t *testing.T) {
	r := newConnectTestRoot(t)

	for _, opt := range append(pages.ConnectHeaderOptions, "VISA", "Visa") {
		st := r.m.buildConnectForm()
		f := st.Field(pages.ConnectFieldHeader)
		f.Selected = connectOptionIndex(f.Options, opt, "binary2")
		f.Value = f.Options[f.Selected]
		applyConnectRules(&st)
		if got := st.Field(pages.ConnectFieldStation).Enabled; got != strings.EqualFold(opt, "visa") {
			t.Fatalf("header %q: station enabled=%v, want %v", opt, got, strings.EqualFold(opt, "visa"))
		}
	}
}

func TestModeSwitchTogglesIPPort(t *testing.T) {
	r := newConnectTestRoot(t)

	st := r.m.buildConnectForm()
	setRadio(&st, pages.ConnectFieldMode, "Caller")
	applyConnectRules(&st)
	if !st.Field(pages.ConnectFieldIP).Enabled || !st.Field(pages.ConnectFieldPort).Enabled {
		t.Fatal("caller mode: IP and Port must both be enabled")
	}
	if st.EnterLabel != "connect" {
		t.Fatalf("caller EnterLabel %q, want connect", st.EnterLabel)
	}

	setRadio(&st, pages.ConnectFieldMode, "Listener")
	applyConnectRules(&st)
	if st.Field(pages.ConnectFieldIP).Enabled || !st.Field(pages.ConnectFieldPort).Enabled {
		t.Fatal("listener mode: Port stays enabled, IP dimmed (bind all)")
	}
	if st.Field(pages.ConnectFieldIP).Note != "(n/a: bind all)" {
		t.Fatalf("listener IP note %q", st.Field(pages.ConnectFieldIP).Note)
	}
	if st.EnterLabel != "listen" {
		t.Fatalf("listener EnterLabel %q, want listen", st.EnterLabel)
	}
}

// setRadio parks a radio field on an option (mirrors the dialog's edit).
func setRadio(st *pages.ConnectFormState, key, option string) {
	f := st.Field(key)
	f.Selected = connectOptionIndex(f.Options, option, "")
	f.Value = f.Options[f.Selected]
}

func TestConnectHappyPathClosesDialogAndSyncsDashboard(t *testing.T) {
	r := newConnectTestRoot(t)
	var calls atomic.Int32
	r.m.dialConnect = func(_ context.Context, _ app.ConnectOptions) error {
		calls.Add(1)

		return nil
	}

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))
	if st := r.state(t); !st.InFlight {
		t.Fatal("enter did not flip the dialog into the in-flight shape")
	}

	attempts, res := r.drainToResult(t)
	if len(attempts) != 1 || attempts[0].Attempt != 1 || attempts[0].Total != 3 {
		t.Fatalf("attempts: %+v, want one 1/3 (config default)", attempts)
	}
	if !res.OK || res.Target != "127.0.0.1:65535" {
		t.Fatalf("result: %+v", res)
	}
	if r.m.dlg != nil {
		t.Fatal("dialog must close on success")
	}
	if calls.Load() != 1 {
		t.Fatalf("dial calls %d, want 1", calls.Load())
	}
	if r.m.conn == nil || r.m.conn.State != events.StateConnected {
		t.Fatalf("connection truth not stamped: %+v", r.m.conn)
	}
	ds := r.m.dashboardState()
	if ds.Conn.Status != pages.ConnOnline || ds.Conn.Target != "127.0.0.1:65535" {
		t.Fatalf("dashboard not synced after connect: %+v", ds.Conn)
	}
	r.wantQuiet(t)
}

// TestConnectResultNilErrNoPanic: a malformed failure result (OK=false
// with no cause) renders the generic failure line instead of
// dereferencing msg.Err (E5-A5-6).
func TestConnectResultNilErrNoPanic(t *testing.T) {
	r := newConnectTestRoot(t)
	r.openHotkey(t)
	r.m.connectRun = &connectRun{cancel: func() {}, total: 1}

	_, _ = r.m.Update(ConnectResultMsg{OK: false})

	if st := r.state(t); st.InFlight || st.Error != "connection failed" {
		t.Errorf("dialog state = InFlight:%v Error:%q, want idle + generic failure line",
			st.InFlight, st.Error)
	}
	if r.m.connectRun != nil {
		t.Error("run not closed by the terminal result")
	}
}

// TestConnectNilSenderTerminal: driving RootModel without
// SetConnectSender must not wedge the dialog in-flight: startConnect
// renders the terminal failure line synchronously, arms no goroutine,
// and a later Enter is not ignored in-flight (E5-A5-7).
func TestConnectNilSenderTerminal(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	_, _ = m.openConnect()
	if m.dlg == nil {
		t.Fatal("openConnect did not open the dialog")
	}

	_, cmd := m.startConnect()
	if cmd != nil {
		t.Error("nil-sender connect armed a cmd")
	}
	if m.connectRun != nil {
		t.Error("nil-sender connect armed an attempt loop")
	}
	st := m.dlg.State()
	if st.InFlight || !strings.Contains(st.Error, "no sender wired") {
		t.Errorf("dialog = InFlight:%v Error:%q, want idle + no-sender failure", st.InFlight, st.Error)
	}

	// Not wedged: Enter starts again (same synchronous close).
	if _, cmd := m.startConnect(); cmd != nil {
		t.Error("second nil-sender connect armed a cmd")
	}
	if st := m.dlg.State(); st.InFlight || !strings.Contains(st.Error, "no sender wired") {
		t.Errorf("second connect = InFlight:%v Error:%q, want the same terminal close", st.InFlight, st.Error)
	}
}
