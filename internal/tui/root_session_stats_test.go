// root_session_stats_test.go proves the proposal-05 §3 P4 contract: the
// §A SESSION card is fed by the async App.SessionStats leg — armed only
// while the dashboard is current (steady ~2s refresh) or while a
// send/worker completion dirtied the read (one off-page re-query), with
// the seq token dropping stale results (the serve-stats lifecycle). The
// DB read runs inside the tea.Cmd (the fake leg counts invocations from
// the cmd, never from Update); failures leave the previous snapshot.
// The LAST STRESS card is also pinned here: stamped ONCE at worker
// completion and reopened through the existing §H summary-overlay path.
package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
)

type sessionStatsTestRoot struct {
	m     *RootModel
	mu    sync.Mutex
	stats *app.DbSessionStats
	err   error
	reads int
	ticks []func() tea.Msg // sessionStatsTickf recordings (senders)
}

func newSessionStatsTestRoot(t *testing.T, stats *app.DbSessionStats, err error) *sessionStatsTestRoot {
	t.Helper()

	r := &sessionStatsTestRoot{stats: stats, err: err}
	m := NewRootModel(nil)
	clock := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }
	m.sessionStatsFn = func(_ context.Context, id string) (*app.DbSessionStats, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.reads++
		_ = id

		return r.stats, r.err
	}
	m.sessionStatsTickf = func(d time.Duration, mk func() tea.Msg) tea.Cmd {
		r.mu.Lock()
		defer r.mu.Unlock()
		if d != 2*time.Second {
			t.Errorf("session stats tick interval = %v, want 2s", d)
		}
		r.ticks = append(r.ticks, mk)

		return func() tea.Msg { return mk() }
	}
	r.m = m
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})

	return r
}

// upd feeds one message and returns the wrapper's cmd (batched with the
// tick arms; the fake tickf already ran its sender synchronously, so
// results reach Update without a clock).
func (r *sessionStatsTestRoot) upd(msg tea.Msg) tea.Cmd {
	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		panic("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

func (r *sessionStatsTestRoot) tickCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.ticks)
}

// fire runs the idx-th recorded sender (the DB read + result msg), then
// folds the result through Update.
func (r *sessionStatsTestRoot) fire(t *testing.T, idx int) {
	t.Helper()
	r.mu.Lock()
	if idx >= len(r.ticks) {
		r.mu.Unlock()
		t.Fatalf("only %d tick(s) armed", idx)
	}
	mk := r.ticks[idx]
	r.mu.Unlock()
	r.upd(mk())
}

func TestSessionStatsTickArmsOnDashboard(t *testing.T) {
	t.Parallel()

	r := newSessionStatsTestRoot(t, &app.DbSessionStats{
		TotalTransactions: 415, SuccessfulTransactions: 415,
		AverageProcessingTimeMs: 0.4,
	}, nil)

	if got := r.tickCount(); got != 1 {
		t.Fatalf("tick armed %d times on the dashboard, want 1", got)
	}
	r.fire(t, 0)

	snap := r.m.dashboardState().Session
	if snap == nil || !snap.Known {
		t.Fatalf("session card must be Known after the first snapshot: %+v", snap)
	}
	if snap.TxSent != 415 || snap.OK != 415 || !strings.Contains(r.m.View().Content, "tx 415") {
		t.Errorf("session card did not fold the snapshot: %+v", snap)
	}
	if !strings.Contains(r.m.View().Content, "avg 0.4ms") {
		t.Errorf("avg cell wrong:\n%s", r.m.View().Content)
	}
	// The wrapper re-arms immediately after a fold (steady 2s cadence).
	if got := r.tickCount(); got != 2 {
		t.Errorf("tick re-armed %d times after the fold, want 2", got)
	}
}

func TestSessionStatsTickDisarmsOffPage(t *testing.T) {
	t.Parallel()

	r := newSessionStatsTestRoot(t, &app.DbSessionStats{TotalTransactions: 7}, nil)
	before := r.m.sessionStatsSeq

	r.upd(ch('2')) // transactions page: dashboard no longer current
	if got := r.m.sessionStatsSeq; got != before+1 {
		t.Fatalf("leaving the dashboard armed a stale tick: seq = %d, want %d", got, before+1)
	}
	if r.m.sessionStatsWait {
		t.Fatal("wait flag must clear on disarm")
	}

	// A straggler result from the pre-leave generation is dropped.
	r.fire(t, 0)
	if r.m.sessionStatsSnap != nil {
		t.Fatal("stale tick result must not fold")
	}
}

func TestSessionStatsDirtyReadsOffPage(t *testing.T) {
	r := newSessionStatsTestRoot(t, &app.DbSessionStats{TotalTransactions: 42}, nil)
	r.fire(t, 0) // fold the first snapshot (42)
	r.upd(ch('2'))

	// A worker stop dirties the read while another page is current:
	// exactly one query runs, and the card is fresh when returning.
	r.mu.Lock()
	r.stats = &app.DbSessionStats{TotalTransactions: 43}
	r.mu.Unlock()
	armedBefore := r.tickCount()
	r.upd(bridge.Msg{Event: events.WorkerStopped{ID: "w-1", Reason: "done"}})

	armed := r.tickCount()
	if armed != armedBefore+1 {
		t.Fatalf("dirty event must arm exactly one off-page read (before=%d after=%d)",
			armedBefore, armed)
	}
	r.fire(t, armed-1)
	if got := r.m.sessionStatsSnap; got == nil || got.TotalTransactions != 43 {
		t.Fatalf("off-page dirty read did not fold: %+v", got)
	}
	if r.CurrentIsWorkers() {
		// the workers page body must not carry the §A card
		if strings.Contains(r.m.View().Content, "tx 43") {
			t.Error("§A card text leaked onto §H")
		}
	}
}

func (r *sessionStatsTestRoot) CurrentIsWorkers() bool {
	return r.m.Current().ID() == pages.WorkersPageID
}

func TestSessionStatsFailureKeepsPrevious(t *testing.T) {
	t.Parallel()

	r := newSessionStatsTestRoot(t, &app.DbSessionStats{TotalTransactions: 9}, nil)
	r.fire(t, 0)
	r.mu.Lock()
	r.stats, r.err = nil, errors.New("db locked")
	r.mu.Unlock()

	r.fire(t, 1) // the re-armed read fails
	if got := r.m.sessionStatsSnap; got == nil || got.TotalTransactions != 9 {
		t.Fatalf("failed read must leave the previous snapshot: %+v", got)
	}
	body := r.m.View().Content
	if !strings.Contains(body, "tx 9") {
		t.Errorf("card must keep the last truth:\n%s", body)
	}
}

func TestSessionStatsNoLegStaysDashed(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil) // no app, no injected leg
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	if m.sessionStatsWait {
		t.Fatal("no leg must not arm a tick")
	}
	st := m.dashboardState().Session
	if st == nil || st.Known {
		t.Fatalf("session card must stay unknown without a leg: %+v", st)
	}
}

// TestSessionStatsSendDoneDirties pins the wrapper's dirty stamp: a
// Done send stage marks the read so the re-arm fires right away.
func TestSessionStatsSendDoneDirties(t *testing.T) {
	t.Parallel()

	r := newSessionStatsTestRoot(t, &app.DbSessionStats{TotalTransactions: 1}, nil)
	r.upd(ch('2')) // leave; disarm

	r.mu.Lock()
	r.stats = &app.DbSessionStats{TotalTransactions: 2}
	r.mu.Unlock()
	r.m.sendRun = &sendRun{state: pages.SendState{Done: true}}
	armedBefore := r.tickCount()
	r.upd(SendStageMsg{Stage: 4, OK: true})

	if r.tickCount() != armedBefore+1 {
		t.Fatalf("Done send run must arm the dirty re-read (before=%d after=%d)",
			armedBefore, r.tickCount())
	}
}

// TestSessionStatsSessionChangeDropsFold pins the id guard with a real
// app config: a result stamped for a session that is no longer live is
// dropped (the card never shows another session's counters).
func TestSessionStatsSessionChangeDropsFold(t *testing.T) {
	a := newTxFileApp(t)
	r := &sessionStatsTestRoot{m: NewRootModel(a), stats: &app.DbSessionStats{TotalTransactions: 5}}
	r.m.sessionStatsFn = func(_ context.Context, id string) (*app.DbSessionStats, error) {
		return r.stats, nil
	}
	r.m.sessionStatsTickf = func(d time.Duration, mk func() tea.Msg) tea.Cmd {
		r.ticks = append(r.ticks, mk)

		return func() tea.Msg { return mk() }
	}
	cfg := a.Config()
	cfg.SetSessionID("sess-live-1")
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.upd(SendStageMsg{Stage: 0, OK: true}) // any msg: arm pass

	if len(r.ticks) == 0 {
		t.Fatal("a configured session id must arm the read")
	}
	cfg.SetSessionID("sess-live-2") // the session rotated before the fold
	r.upd(r.ticks[0]())
	if r.m.sessionStatsSnap != nil {
		t.Fatal("a fold for a superseded session id must be dropped")
	}
}

// --- LAST SEND / LAST STRESS card derivation ----------------------------

// TestLastSendCardDerivation: the §A LAST SEND card is derived from the
// frozen §D state — field-0 MTIs, RC badge, elapsed and the completion
// time stamp (never the live clock).
func TestLastSendCardDerivation(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	stamp := time.Date(2026, 9, 10, 9, 21, 44, 0, time.UTC)
	m.lastSend = &pages.SendState{
		TxName: "Echo", RC: "00", RCLabel: "APPROVED",
		Elapsed: 1900 * time.Microsecond, Validated: true, CorrelationOK: true,
		Request:  []pages.ExchangeRow{{Num: "0", Display: "0200"}},
		Response: []pages.ExchangeRow{{Num: "0", Display: "0210"}},
	}
	m.lastSendAt = stamp

	card := m.dashboardState().LastSend
	if card == nil {
		t.Fatal("lastSend must derive a card")
	}
	if card.Time != "09:21:44" || card.TxName != "Echo" ||
		card.ReqMTI != "0200" || card.RespMTI != "0210" ||
		card.RC != "00" || card.RCNote != "APPROVED" ||
		card.Elapsed != 1900*time.Microsecond || !card.Validated || !card.Correlation {
		t.Errorf("card derivation wrong: %+v", card)
	}
}

func TestLastSendCardTimeZeroWhenNeverStamped(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.lastSend = &pages.SendState{TxName: "Echo", Done: true}

	if card := m.dashboardState().LastSend; card == nil || card.Time != "" {
		t.Fatalf("an unstamped completion must render no time, got %+v", card)
	}
}

// TestLastStressCardStampedOnceAtCompletion: the LAST STRESS card
// stamps when the stress worker completes — from the SAME single
// summary fetch the §H overlay uses, never per tick.
func TestLastStressCardStampedOnceAtCompletion(t *testing.T) {
	r := newWorkerTestRoot(t)
	var fetches int
	r.m.workerSummaryFn = func(id string) (*app.StressSummary, error) {
		fetches++

		return &app.StressSummary{
			Workers: 1, Sent: 100, Successful: 100,
			ActualTPS: 82.3, P99LatencyMs: 0.3,
		}, nil
	}
	r.bus(events.WorkerStarted{ID: "w-2", Kind: "stress"})
	if r.m.lastStress != nil {
		t.Fatal("no card before completion")
	}
	r.bus(events.WorkerStopped{ID: "w-2", Reason: "done"})

	card := r.m.lastStress
	if card == nil {
		t.Fatal("completed stress run must stamp the LAST STRESS card")
	}
	if fetches != 1 {
		t.Errorf("summary fetched %d times, want exactly 1 (never per tick)", fetches)
	}
	if card.ID != "w-2" || !card.Done || card.OkPct != "100.0%" ||
		card.Workers != "1 worker" || card.TPS != "82.3 tps" || card.P99 != "0.3ms" {
		t.Errorf("card fields wrong: %+v", card)
	}
	if card.Time != "12:00:00" {
		t.Errorf("card time = %q, want the fake-clock completion stamp", card.Time)
	}
}

// TestStressSummaryMsgReopensClosedOverlay: the §A "Stress summary" row
// dispatches LastStressSummaryMsg; the router lands on §H and re-opens
// the EXISTING summary overlay for the same worker id (the SetState
// identity alone would not re-open a closed overlay — OpenSummary does).
func TestStressSummaryMsgReopensClosedOverlay(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.m.workerSummaryFn = func(id string) (*app.StressSummary, error) {
		return &app.StressSummary{
			WorkerID: "w-2", Workers: 1, Sent: 10,
			Successful: 10, ActualTPS: 5, P99LatencyMs: 1.5,
		}, nil
	}
	r.bus(events.WorkerStarted{ID: "w-2", Kind: "stress"})
	r.bus(events.WorkerStopped{ID: "w-2", Reason: "done"})
	r.upd(tea.KeyPressMsg{Code: tea.KeyEscape}) // close the overlay
	if r.m.workers.SummaryOpen() {
		t.Fatal("esc must close the overlay first")
	}

	r.upd(ch('1')) // back to the dashboard (the card + row live there)
	if !strings.Contains(r.m.View().Content, "Stress summary") {
		t.Fatalf("dashboard must list the reopen row while a run exists:\n%s",
			r.m.View().Content)
	}

	r.upd(palette.LastStressSummaryMsg{})
	if r.m.Current().ID() != pages.WorkersPageID {
		t.Fatalf("reopen must land on §H, got %q", r.m.Current().ID())
	}
	if !r.m.workers.SummaryOpen() {
		t.Fatal("the existing summary overlay must reopen")
	}
	if !strings.Contains(r.m.View().Content, "w-2") {
		t.Errorf("overlay must show the run:\n%s", r.m.View().Content)
	}
}

// TestStressSummaryMsgNoRunToasts: without a completed run the row's
// Msg is a sane info no-op — never a page jump, never a panic.
func TestStressSummaryMsgNoRunToasts(t *testing.T) {
	r := newWorkerTestRoot(t)
	r.upd(ch('1'))

	r.upd(palette.LastStressSummaryMsg{})
	if r.m.Current().ID() != pages.DashboardPageID {
		t.Fatal("no stress run must not leave the dashboard")
	}
}

// TestDashboardLastStressRowHiddenWithoutRun: the reopen row appears in
// the dashboard snapshot only once a stress run completed.
func TestDashboardLastStressRowHiddenWithoutRun(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if strings.Contains(m.View().Content, "Stress summary") {
		t.Fatal("row must stay hidden before any stress run")
	}
	m.lastStress = &pages.LastStressCard{ID: "w-2", Done: true}
	m.syncDashboard()
	if !strings.Contains(m.View().Content, "Stress summary") {
		t.Fatal("row must render once a stress run completed")
	}
}
