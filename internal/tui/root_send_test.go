package tui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
)

// sendTestRoot wires a real tx-file app (for the tx repository and spec)
// with the live legs faked: the collector receives the goroutine's
// SendStageMsgs exactly like program.Send would, and the clock is fake so
// elapsed stamps are deterministic (the bridge/dashboard test idioms).
type sendTestRoot struct {
	m       *RootModel
	col     chan tea.Msg
	clock   time.Time
	advance func(time.Duration)
}

func newSendTestRoot(t *testing.T) *sendTestRoot {
	t.Helper()

	m := NewRootModel(newTxFileApp(t))
	col := make(chan tea.Msg, 32)
	m.SetSendSender(func(msg tea.Msg) { col <- msg })
	// The direct-send fixture truth: every root here exercises sends on
	// a CONNECTED session (an offline send belongs to the wizard, pinned
	// in root_transactions_test.go); stamp the connection the same way
	// the bridge's live pump does.
	stampConnected(t, m)

	r := &sendTestRoot{m: m, col: col, clock: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	r.advance = func(d time.Duration) { r.clock = r.clock.Add(d) }
	m.now = func() time.Time { return r.clock }

	t.Cleanup(func() {
		select {
		case <-col:
		case <-time.After(150 * time.Millisecond):
		}
	})

	return r
}

func (r *sendTestRoot) spec() *iso8583.MessageSpec { return r.m.app.Service().GetSpec() }

// nextStage drains the next SendStageMsg (failing on quiet channels).
func (r *sendTestRoot) nextStage(t *testing.T) SendStageMsg {
	t.Helper()

	select {
	case msg := <-r.col:
		sm, ok := msg.(SendStageMsg)
		if !ok {
			t.Fatalf("collector got %T, want SendStageMsg", msg)
		}

		return sm
	case <-time.After(2 * time.Second):
		t.Fatal("send goroutine delivered no stage msg")

		return SendStageMsg{}
	}
}

// wantQuiet asserts the collector stays silent (no auto-retry, no
// never-fired stage ever emits).
func (r *sendTestRoot) wantQuiet(t *testing.T) {
	t.Helper()

	select {
	case msg := <-r.col:
		t.Fatalf("unexpected extra msg %v after terminal stage", msg)
	case <-time.After(120 * time.Millisecond):
	}
}

// pump feeds one stage msg through Update.
func (r *sendTestRoot) pump(t *testing.T, msg SendStageMsg) {
	t.Helper()

	_, _ = r.m.Update(msg)
}

// runHappySend drives a full five-stage ok walk for tx id.
func (r *sendTestRoot) runHappySend(t *testing.T, id string) {
	t.Helper()

	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		return cannedExchange(t, r.spec()), nil
	}
	_, _ = r.m.Update(pages.TxSendMsg{ID: id})
	for want := 0; want < pages.SendStageCount; want++ {
		sm := r.nextStage(t)
		if sm.Stage != want || !sm.OK {
			t.Fatalf("stage %d = %d/%v", want, sm.Stage, sm.Err)
		}
		if want == pages.SendStageCount-1 {
			r.advance(3200 * time.Millisecond)
		}
		r.pump(t, sm)
	}
	r.wantQuiet(t)
}

// TestSendHistoryStampingAndOverlay: a completed send
// lands in the bounded ring; ":send history" opens the overlay; Enter
// freezes §D on the picked entry; Esc pops back to the history.
func TestSendHistoryStampingAndOverlay(t *testing.T) {
	r := newSendTestRoot(t)
	r.runHappySend(t, "Purchase")

	if len(r.m.sends) != 1 {
		t.Fatalf("ring entries = %d, want 1", len(r.m.sends))
	}
	// Leave §D (the walk pushed it).
	_, _ = r.m.Update(pages.SendPopMsg{})

	_, _ = r.m.Update(palette.SendHistoryMsg{})
	if got := r.m.Current().ID(); got != pages.SendHistoryPageID {
		t.Fatalf("current page = %q, want the send-history overlay", got)
	}
	body := r.m.View().Content
	if !strings.Contains(body, "Purchase") || !strings.Contains(body, "SEND HISTORY") {
		t.Errorf("history list lacks the completed send:\n%s", body)
	}

	_, _ = r.m.Update(pages.SendHistoryPickMsg{Index: 0})
	if got := r.m.Current().ID(); got != pages.SendPageID {
		t.Fatalf("pick must freeze §D, current = %q", got)
	}
	if st := r.m.send.State(); !st.Done || st.TxID != "Purchase" {
		t.Errorf("frozen §D state = %#v, want the completed Purchase run", st)
	}
	// Esc on §D returns to the history, not past it.
	_, _ = r.m.Update(pages.SendPopMsg{})
	if got := r.m.Current().ID(); got != pages.SendHistoryPageID {
		t.Fatalf("esc from the frozen view = %q, want the history page", got)
	}
	_, _ = r.m.Update(pages.SendHistoryPopMsg{})
}

// TestSendHistoryEmptyToasts: with nothing sent yet the overlay stays
// closed (a toast explains) — the UAT's "no dangling empty menus" rule.
func TestSendHistoryEmptyToasts(t *testing.T) {
	r := newSendTestRoot(t)
	_, _ = r.m.Update(palette.SendHistoryMsg{})

	if got := r.m.Current().ID(); got != pages.DashboardPageID {
		t.Fatalf("empty history must not push a page, current = %q", got)
	}
}

func mkMsg(t *testing.T, spec *iso8583.MessageSpec, mti string, vals map[int]string) *iso8583.Message {
	t.Helper()

	msg := iso8583.NewMessage(spec)
	msg.MTI(mti)

	nums := make([]int, 0, len(vals))
	for n := range vals {
		nums = append(nums, n)
	}
	for _, n := range nums {
		if err := msg.Field(n, vals[n]); err != nil {
			t.Fatalf("set field %d: %v", n, err)
		}
	}

	return msg
}

// cannedRequest/cannedResponse mirror the §D exchange: the PAN
// and STAN echo, field 38 is response-only, RC 00 approves.
func cannedRequest(t *testing.T, spec *iso8583.MessageSpec) *iso8583.Message {
	t.Helper()

	return mkMsg(t, spec, "0200", map[int]string{
		2: "4242424242424242", 3: "000000", 4: "100", 7: "0908120000",
		11: "041822", 37: "123456789012", 41: "TERM0001", 43: "SHOP", 49: "840",
	})
}

func cannedResponse(t *testing.T, spec *iso8583.MessageSpec) *iso8583.Message {
	t.Helper()

	return mkMsg(t, spec, "0210", map[int]string{
		2: "4242424242424242", 11: "041822", 38: "482913", 39: "00", 49: "840",
	})
}

func cannedExchange(t *testing.T, spec *iso8583.MessageSpec) *liveExchange {
	t.Helper()

	return &liveExchange{
		Request: cannedRequest(t, spec), Response: cannedResponse(t, spec),
		Wrote: true, Elapsed: 3 * time.Millisecond,
	}
}

// TestSendStageMachineHappy: five stage msgs in walk order, the state
// closes on Validate, and the elapsed timer freezes at the fake-clock
// span between start and the final transition.
func TestSendStageMachineHappy(t *testing.T) {
	r := newSendTestRoot(t)
	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		return cannedExchange(t, r.spec()), nil
	}

	_, cmd := r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	if cmd == nil {
		t.Fatal("start returned no elapsed tick cmd")
	}
	if got := r.m.Current().ID(); got != pages.SendPageID {
		t.Fatalf("current page = %q, want %q", got, pages.SendPageID)
	}

	for want := 0; want < pages.SendStageCount; want++ {
		sm := r.nextStage(t)
		if sm.Stage != want {
			t.Fatalf("stage order: got %d, want %d", sm.Stage, want)
		}
		if !sm.OK {
			t.Fatalf("stage %d not OK: %v", sm.Stage, sm.Err)
		}
		if want == pages.SendStageCount-1 {
			r.advance(3200 * time.Millisecond)
		}
		r.pump(t, sm)
	}
	r.wantQuiet(t)

	st := r.m.send.State()
	if !st.Done || !st.Validated || !st.CorrelationOK || st.TimedOut {
		t.Errorf("state = Done:%v Validated:%v Corr:%v TimedOut:%v", st.Done, st.Validated, st.CorrelationOK, st.TimedOut)
	}
	if st.RC != "00" || st.RCLabel != "APPROVED" || !st.RCok {
		t.Errorf("RC badge = %q/%q/%v", st.RC, st.RCLabel, st.RCok)
	}
	if len(st.StageOK) != pages.SendStageCount {
		t.Fatalf("StageOK = %v, want 5 entries", st.StageOK)
	}
	if st.Elapsed != 3200*time.Millisecond {
		t.Errorf("elapsed frozen = %v, want 3.2s", st.Elapsed)
	}
	if len(st.Request) == 0 || len(st.Response) == 0 {
		t.Error("panes empty after parse")
	}
}

// TestSendConnectFail: stage 0 fails and nothing after it ever fires.
func TestSendConnectFail(t *testing.T) {
	r := newSendTestRoot(t)
	dial := errors.New("dial tcp: refused")
	r.m.liveConnect = func(context.Context) error { return dial }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		t.Error("send leg ran after a failed connect")

		return nil, nil
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})

	sm := r.nextStage(t)
	if sm.Stage != 0 || sm.OK || !errors.Is(sm.Err, dial) {
		t.Fatalf("stage 0 = %d/%v/%v", sm.Stage, sm.OK, sm.Err)
	}
	r.pump(t, sm)
	r.wantQuiet(t)

	st := r.m.send.State()
	if !st.Done || st.TimedOut || len(st.StageOK) != 1 || st.StageOK[0] {
		t.Errorf("state = Done:%v TimedOut:%v StageOK:%v", st.Done, st.TimedOut, st.StageOK)
	}
}

// TestSendTimeoutNoRetry: the budget is the config response-timeout (the
// CLI send --wait source); a context deadline closes the run as TimedOut
// and the leg is called exactly once — no auto-retry (project rule).
func TestSendTimeoutNoRetry(t *testing.T) {
	r := newSendTestRoot(t)
	config.GetConfig().SetResponseTimeout(80 * time.Millisecond)

	var calls int

	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(ctx context.Context, txName string) (*liveExchange, error) {
		calls++

		req := cannedRequest(t, r.spec())
		<-ctx.Done()

		return &liveExchange{Request: req, Wrote: true}, ctx.Err()
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})

	seen := map[int]bool{}

	for i := 0; i < 3; i++ {
		sm := r.nextStage(t)
		seen[sm.Stage] = true
		if sm.Stage == 2 {
			if sm.OK || !errors.Is(sm.Err, context.DeadlineExceeded) {
				t.Fatalf("timeout stage = OK:%v Err:%v", sm.OK, sm.Err)
			}
		}
		r.pump(t, sm)
	}
	r.wantQuiet(t)

	if calls != 1 {
		t.Fatalf("send leg called %d times, want exactly 1 (no auto-retry)", calls)
	}
	st := r.m.send.State()
	if !st.Done || !st.TimedOut || st.Validated {
		t.Errorf("state = Done:%v TimedOut:%v Validated:%v", st.Done, st.TimedOut, st.Validated)
	}
	if seen[3] || seen[4] {
		t.Error("Parse/Validate fired after the timeout")
	}
}

// TestSendInFlightIgnored: a second TxSendMsg while one op runs is
// ignored — no queue, no second leg call, no second push.
func TestSendInFlightIgnored(t *testing.T) {
	r := newSendTestRoot(t)
	release := make(chan struct{})

	var calls atomic.Int32

	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		calls.Add(1)
		<-release

		return cannedExchange(t, r.spec()), nil
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})

	// The leg has been entered (calls may lag by microseconds; poll).
	deadline := time.Now().Add(time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	depth := r.m.StackDepth()

	_, cmd := r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	if cmd != nil {
		t.Error("in-flight send returned a cmd; expected an ignored no-op")
	}
	close(release)

	for i := 0; i < 5; i++ {
		r.pump(t, r.nextStage(t))
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("send leg called %d times, want 1 (Enter while in-flight ignored)", got)
	}
	if r.m.StackDepth() != depth {
		t.Fatalf("stack depth changed mid-flight: %d → %d", depth, r.m.StackDepth())
	}
}

// TestSendEscPops: Esc on the §D page pops back (root owns the stack).
func TestSendEscPops(t *testing.T) {
	r := newSendTestRoot(t)
	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		return cannedExchange(t, r.spec()), nil
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	for i := 0; i < pages.SendStageCount; i++ {
		r.pump(t, r.nextStage(t))
	}
	if r.m.StackDepth() != 2 {
		t.Fatalf("depth = %d, want 2", r.m.StackDepth())
	}

	_, cmd := r.m.Update(special(tea.KeyEscape)) // esc → page yields SendPopMsg
	if cmd == nil {
		t.Fatal("esc produced no pop msg")
	}
	_, _ = r.m.Update(cmd())
	if r.m.StackDepth() != 1 {
		t.Fatalf("depth after pop = %d, want 1", r.m.StackDepth())
	}
}

// TestSendElapsedRefreshStopsOnDone: the tick refreshes Elapsed while in
// flight and stops being re-armed once the run is Done (fake clock).
func TestSendElapsedRefreshStopsOnDone(t *testing.T) {
	r := newSendTestRoot(t)
	r.m.liveConnect = func(context.Context) error { return nil }
	release := make(chan struct{})
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		<-release

		return cannedExchange(t, r.spec()), nil
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	r.pump(t, r.nextStage(t)) // Connect ✓; now waiting in the leg

	r.advance(1200 * time.Millisecond)

	_, cmd := r.m.Update(sendElapsedMsg{gen: r.m.sendRun.gen})
	if cmd == nil {
		t.Fatal("in-flight tick produced no refresh cmd")
	}
	if got := r.m.send.State().Elapsed; got != 1200*time.Millisecond {
		t.Fatalf("live elapsed = %v, want 1.2s", got)
	}

	close(release)
	for i := 1; i < pages.SendStageCount; i++ {
		r.pump(t, r.nextStage(t))
	}
	frozen := r.m.send.State().Elapsed

	if _, cmd := r.m.Update(sendElapsedMsg{gen: r.m.sendRun.gen}); cmd != nil {
		t.Error("tick re-armed after Done; the timer must stop refreshing")
	}
	if got := r.m.send.State().Elapsed; got != frozen {
		t.Errorf("elapsed moved after Done: %v → %v", frozen, got)
	}
}

// TestSendValidateFail: a request failing the shared ValidateMessage gate
// lands as a failed stage 4 (validated ✗), final until the user re-sends.
func TestSendValidateFail(t *testing.T) {
	r := newSendTestRoot(t)
	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		ex := cannedExchange(t, r.spec())
		ex.Request.UnsetField(3) // required by validateFinancialMessage

		return ex, nil
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	for i := 0; i < pages.SendStageCount; i++ {
		sm := r.nextStage(t)
		if sm.Stage == 4 && sm.OK {
			t.Fatal("validate stage passed with a required field missing")
		}
		r.pump(t, sm)
	}
	st := r.m.send.State()
	if !st.Done || st.Validated {
		t.Errorf("state = Done:%v Validated:%v", st.Done, st.Validated)
	}
}

// TestSendWithoutApp: nil app keeps TxSendMsg the old logged no-op — the
// stack never changes and no command runs.
func TestSendWithoutApp(t *testing.T) {
	m := NewRootModel(nil)
	_, cmd := m.Update(pages.TxSendMsg{ID: "Purchase"})
	if cmd != nil {
		t.Error("send without an app returned a cmd")
	}
	wantStack(t, m, "dashboard")
}

// TestSendBudgetSource: the §D budget is the config response timeout —
// the same value App.New hands the service and the CLI send --wait waits
// on — and 0 without an app.
func TestSendBudgetSource(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	config.GetConfig().SetResponseTimeout(3 * time.Second)
	if got := m.responseBudget(); got != 3*time.Second {
		t.Fatalf("budget = %v, want 3s", got)
	}
	if got := NewRootModel(nil).responseBudget(); got != 0 {
		t.Fatalf("nil-app budget = %v, want 0", got)
	}
}

// TestSendTickSingleFlightAcrossResend: a re-send right after Done (the
// 250ms tick of the old chain still pending) must leave EXACTLY ONE
// chain: the orphaned generation is dropped without re-arming, the live
// chain re-arms once and its cmd carries the current generation, and
// nothing re-arms after the new run is Done (E5-A5-4).
func TestSendTickSingleFlightAcrossResend(t *testing.T) {
	r := newSendTestRoot(t)
	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		return cannedExchange(t, r.spec()), nil
	}

	// Run 1: started and closed to Done.
	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	for i := 0; i < pages.SendStageCount; i++ {
		r.pump(t, r.nextStage(t))
	}
	orphanGen := r.m.sendRun.gen

	// Run 2: re-sent immediately (well inside the 250ms tick window).
	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	if r.m.sendRun.gen == orphanGen {
		t.Fatal("re-send did not bump the run generation")
	}
	r.pump(t, r.nextStage(t)) // Connect ✓; run 2 in flight

	// The orphaned chain's tick lands while run 2 runs: dropped, not
	// re-armed (this is where chains used to multiply).
	if _, cmd := r.m.Update(sendElapsedMsg{gen: orphanGen}); cmd != nil {
		t.Error("orphaned generation re-armed; stale ticks must retire")
	}

	// The live chain re-arms exactly once, stamped with the live gen.
	_, cmd := r.m.Update(sendElapsedMsg{gen: r.m.sendRun.gen})
	if cmd == nil {
		t.Fatal("in-flight tick produced no refresh cmd")
	}
	msg := cmd() // blocks sendTickInterval, then yields the next tick
	if em, ok := msg.(sendElapsedMsg); !ok || em.gen != r.m.sendRun.gen {
		t.Fatalf("tick cmd yielded %#v, want sendElapsedMsg gen %d", msg, r.m.sendRun.gen)
	}

	// Run 2 closes: no chain re-arms any more.
	for i := 1; i < pages.SendStageCount; i++ {
		r.pump(t, r.nextStage(t))
	}
	if _, cmd := r.m.Update(sendElapsedMsg{gen: r.m.sendRun.gen}); cmd != nil {
		t.Error("tick re-armed after Done; exactly one chain must exist")
	}
}

// TestSendNilSenderTerminal: driving RootModel without SetSendSender
// (library use) must NOT wedge §D: the run closes synchronously with a
// failed Connect stage (Done, no cmd, no goroutine), and a later
// TxSendMsg re-arms (closing again) instead of being ignored forever.
func TestSendNilSenderTerminal(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	stampConnected(t, m) // the connected direct-send fixture (see newSendTestRoot)

	_, cmd := m.Update(pages.TxSendMsg{ID: "Purchase"})
	if cmd != nil {
		t.Error("nil-sender send armed a cmd")
	}
	if m.sendRun == nil || !m.sendRun.state.Done {
		t.Fatal("nil-sender run never reached Done")
	}
	st := m.send.State()
	if !st.Done || len(st.StageOK) != 1 || st.StageOK[0] || st.TimedOut {
		t.Errorf("page state = Done:%v StageOK:%v TimedOut:%v, want closed stage-0 failure",
			st.Done, st.StageOK, st.TimedOut)
	}

	// Not wedged: a second send is NOT ignored in-flight.
	m.Update(pages.TxSendMsg{ID: "Purchase"})
	if m.sendRun.gen != 2 {
		t.Fatalf("generation = %d, want 2 (second send must re-arm and close)", m.sendRun.gen)
	}
	if !m.sendRun.state.Done {
		t.Error("second nil-sender run not closed")
	}
}

// TestViewLastSend: the ":last send" action reopens §D on the last
// completed run; with no completed run it toasts and the stack is
// untouched (returning to a previously sent transaction).
func TestViewLastSend(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	m.Update(palette.LastSendViewMsg{})
	if m.Current().ID() == pages.SendPageID {
		t.Fatal("no last send: must not push §D")
	}
	if !strings.Contains(m.View().Content, "no previous send yet") {
		t.Fatal("no last send: must toast")
	}

	m.lastSend = &pages.SendState{TxID: "echo", TxName: "Echo"}
	m.Update(palette.LastSendViewMsg{})
	if m.Current().ID() != pages.SendPageID {
		t.Fatalf("last send view current=%q, want send", m.Current().ID())
	}
	if !strings.Contains(m.View().Content, "Echo") {
		t.Fatalf("last send view must show the saved tx:\n%s", m.View().Content)
	}
}
