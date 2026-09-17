// root_sessions_test.go proves the SCR-509 root contract with a fake
// façade (no real DB above the seam): entry onto §I arms the session-
// list query off the UI thread and folds the result into the page, the
// queries never run for other pages, `r` re-queries, a WorkerStopped
// bus event dirties the cache so the next Update re-queries (the state
// carries the new tx only when the query result arrives — no
// optimistic writes), typed DB errors render as empty-state text,
// Enter loads the selected session's detail, [t] opens the
// reconstructed review, and a root without any façade leg just sits in
// its empty state.
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	app "jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/db"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// fakeSessions is the injectable §I façade: canned rows, per-call
// hooks, and call counters (the fake never blocks; the pump runs the
// cmds the root returns).
type fakeSessions struct {
	mu       sync.Mutex
	path     string
	sessions []app.DbSessionView
	stats    *app.DbSessionStats
	history  []app.DbTransactionView
	review   *app.DbTransactionRetrospective
	listErr  error

	listN, detailN, reviewN int
	detailIDs               []string
	reviewIDs               []int64
	onList                  func(calls int)
}

func (f *fakeSessions) DBPath() string { return f.path }

func (f *fakeSessions) ListSessions(_ context.Context, _ int) ([]app.DbSessionView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listN++
	if f.onList != nil {
		f.onList(f.listN)
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]app.DbSessionView(nil), f.sessions...), nil
}

func (f *fakeSessions) SessionStats(_ context.Context, id string) (*app.DbSessionStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detailN++
	f.detailIDs = append(f.detailIDs, id)

	return f.stats, nil
}

func (f *fakeSessions) TxHistory(_ context.Context, _ string, _ int) ([]app.DbTransactionView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]app.DbTransactionView(nil), f.history...), nil
}

func (f *fakeSessions) ReviewTx(_ context.Context, txID int64) (*app.DbTransactionRetrospective, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviewN++
	f.reviewIDs = append(f.reviewIDs, txID)
	if f.review == nil {
		return nil, fmt.Errorf("transaction ID %d not found", txID)
	}
	if f.review.ID == 0 {
		rev := *f.review
		rev.ID = txID
		f.review = &rev
	}

	return f.review, nil
}

// fakeSessionsFixture is the wireframe §I data in façade shapes.
func fakeSessionsFixture() *fakeSessions {
	ts := func(h, m int) time.Time { return time.Date(2026, 9, 9, h, m, 0, 0, time.UTC) }

	return &fakeSessions{
		path: "./sessions.db",
		sessions: []app.DbSessionView{
			{SessionID: "9f3ca1e2b7d84455a1", LastActiveTime: ts(12, 1)},
			{SessionID: "77b255c9e4d3", LastActiveTime: ts(9, 55)},
		},
		stats: &app.DbSessionStats{
			TotalTransactions: 150, SuccessfulTransactions: 148, FailedTransactions: 2,
			AverageProcessingTimeMs:  3.4,
			ResponseCodeDistribution: map[string]int{"00": 148, "96": 2},
		},
		history: []app.DbTransactionView{
			{ID: 9, Timestamp: ts(12, 4), TxName: "Purchase", MTI: "0210", ResponseCode: "00", Success: true, ProcessingTime: 3 * time.Millisecond},
			{ID: 8, Timestamp: ts(12, 3), TxName: "Sign On", MTI: "0810", ResponseCode: "00", Success: true, ProcessingTime: time.Millisecond},
		},
		review: &app.DbTransactionRetrospective{
			ID: 9, TxName: "Purchase", ResponseCode: "96", ProcessingTime: 2 * time.Millisecond,
			Request: &app.DbMessageReconstruction{HEX: "30 32 30 30 f0 00 00 00", DescribeText: "MTI 0200\n  2 (PAN) 4242424242424242"},
		},
	}
}

type sessionsTestRoot struct {
	m     *RootModel
	clock time.Time
}

func newSessionsTestRoot(t *testing.T, fake *fakeSessions) *sessionsTestRoot {
	t.Helper()
	r := &sessionsTestRoot{m: NewRootModel(nil), clock: time.Date(2026, 9, 9, 12, 4, 11, 0, time.UTC)}
	t.Setenv("JISO_ASCII", "")
	r.m.theme = theme.NewWith(colorprofile.ASCII, true)
	r.m.now = func() time.Time { return r.clock }
	if fake != nil {
		r.m.sessionsSrc = fake
	}
	r.pump(tea.WindowSizeMsg{Width: 120, Height: 40}) // pumped: the
	// resize flush cmd must run (an unpumped leading resize leaves the
	// coalescer pending and later sizes are dropped).

	return r
}

// pump feeds msg and every resulting command result back through
// Update until quiescent (tea.Cmd results may be batches → []tea.Msg).
func (r *sessionsTestRoot) pump(msg tea.Msg) {
	queue := []tea.Msg{msg}
	for i := 0; i < 32 && len(queue) > 0; i++ {
		next := r.upd(queue[0])
		queue = queue[1:]
		queue = append(queue, flattenMsgs(next)...)
	}
}

func (r *sessionsTestRoot) upd(msg tea.Msg) tea.Cmd {
	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		panic("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

func flattenMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch v := cmd().(type) {
	case nil:
		return nil
	case []tea.Msg:
		return v
	default:
		return []tea.Msg{v}
	}
}

// gotoPage jumps to §I and nudges the size (root only forwards
// WindowSizeMsg to pages on the stack; the fresh page needs a new size
// to lay out wide — the resize coalescer skips a repeat of the applied
// size by design).
func (r *sessionsTestRoot) gotoPage() {
	r.pump(ch('6'))
	r.pump(tea.WindowSizeMsg{Width: 121, Height: 41})
}

func (r *sessionsTestRoot) bus(ev events.Event) { r.pump(bridge.Msg{Event: ev}) }

func (r *sessionsTestRoot) body() string { return r.m.View().Content }

func TestSessionsEntryLoadsThroughFacade(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()

	body := r.body()
	// "today 12:" — the inner width mid-elides the WHEN column tail.
	for _, want := range []string{"SESSIONS", "9f3c~a1", "today 12:", "150", "148 (98.7%)", "3.4 ms", "00:148 96:2", "Purchase", "0210", "[ok] ok"} {
		if !strings.Contains(body, want) {
			t.Errorf("page body lacks %q:\n%s", want, body)
		}
	}
	if fake.listN != 1 || fake.detailN != 1 {
		t.Fatalf("calls list=%d detail=%d, want 1/1", fake.listN, fake.detailN)
	}
	if fake.detailIDs[0] != "9f3ca1e2b7d84455a1" {
		t.Fatalf("detail id = %q, want the newest session", fake.detailIDs[0])
	}
}

func TestSessionsNoQueriesOffPage(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.pump(ch('2'))
	r.pump(ch('4'))
	if fake.listN != 0 {
		t.Fatalf("queries ran off-page: listN=%d", fake.listN)
	}
}

func TestSessionsRefreshKeyRequeries(t *testing.T) {
	fake := fakeSessionsFixture()
	fake.onList = func(calls int) {
		if calls == 2 {
			fake.sessions = append(fake.sessions, app.DbSessionView{
				SessionID: "31a000f4", LastActiveTime: fake.sessions[0].LastActiveTime.Add(time.Hour),
			})
		}
	}
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()
	r.pump(ch('r'))

	if fake.listN != 2 {
		t.Fatalf("r must re-query: listN=%d", fake.listN)
	}
	if !strings.Contains(r.body(), "31a000f4") {
		t.Errorf("new session missing after refresh:\n%s", r.body())
	}
}

func TestSessionsWorkerStoppedTriggersRefresh(t *testing.T) {
	fake := fakeSessionsFixture()
	fake.onList = func(calls int) {
		if calls == 2 {
			fake.history = append([]app.DbTransactionView{{
				ID: 10, Timestamp: fake.history[0].Timestamp.Add(time.Second), TxName: "Reversal",
				MTI: "0420", ResponseCode: "00", Success: true, ProcessingTime: 4 * time.Millisecond,
			}}, fake.history...)
		}
	}
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()
	if strings.Contains(r.body(), "Reversal") {
		t.Fatal("row existed before the event")
	}
	r.bus(events.WorkerStopped{ID: "w-1", Reason: "done"})

	if fake.listN < 2 {
		t.Fatalf("WorkerStopped must dirty the cache: listN=%d", fake.listN)
	}
	if !strings.Contains(r.body(), "Reversal") {
		t.Errorf("state must carry the new tx after the refresh result:\n%s", r.body())
	}
}

func TestSessionsMissingDBIsEmptyStateText(t *testing.T) {
	fake := fakeSessionsFixture()
	fake.listErr = &app.ConfigError{Path: "./gone.db", Err: fmt.Errorf("%w: %s: stat", db.ErrDBNotFound, "./gone.db")}
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()

	if !strings.Contains(r.body(), "no sessions in ./gone.db") {
		t.Errorf("missing-DB text missing:\n%s", r.body())
	}
	if strings.Contains(r.body(), "coming in M5") {
		t.Fatal("page must not be a placeholder")
	}
}

func TestSessionsUnsetDBIsEmptyStateText(t *testing.T) {
	fake := fakeSessionsFixture()
	fake.listErr = app.ErrDBNotConfigured
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()

	if !strings.Contains(r.body(), "database not configured") {
		t.Errorf("unset-DB text missing:\n%s", r.body())
	}
}

func TestSessionsNoFacadeLegStaysEmpty(t *testing.T) {
	r := newSessionsTestRoot(t, nil)
	r.gotoPage()
	if !strings.Contains(r.body(), "database not configured") {
		t.Errorf("nil-leg empty state missing:\n%s", r.body())
	}
}

func TestSessionsEnterSelectLoadsDetail(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()
	r.pump(tea.KeyPressMsg{Code: tea.KeyDown})
	r.pump(tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(fake.detailIDs) < 2 || fake.detailIDs[len(fake.detailIDs)-1] != "77b255c9e4d3" {
		t.Fatalf("detail ids = %v, want the second session last", fake.detailIDs)
	}
}

// a bare down (no Enter) re-points the detail subject and loads the newly
// focused session; a clamped same-row move must not re-fire the load.
func TestSessionsCursorMoveLoadsDetail(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()
	if fake.detailN != 1 {
		t.Fatalf("entry detail loads = %d, want 1", fake.detailN)
	}
	r.pump(tea.KeyPressMsg{Code: tea.KeyDown})

	if got := r.m.sessionsSelected; got != "77b255c9e4d3" {
		t.Fatalf("sessionsSelected = %q, want the newly-focused session", got)
	}
	if fake.detailN != 2 {
		t.Fatalf("detail loads = %d, want 2 after the cursor move", fake.detailN)
	}
	if fake.detailIDs[len(fake.detailIDs)-1] != "77b255c9e4d3" {
		t.Fatalf("detail ids = %v, want the new session last", fake.detailIDs)
	}

	// Clamped at the last row: no cursor move, no load.
	r.pump(tea.KeyPressMsg{Code: tea.KeyDown})
	if fake.detailN != 2 {
		t.Fatalf("clamped same-row move re-fired the load: detailN=%d, want 2", fake.detailN)
	}
}

// while the detail leg is in flight the detail panes show the loading
// marker, not the false "no transactions recorded" empty state.
func TestSessionsDetailWaitShowsLoadingText(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()

	// Move the cursor and deliver the focus msg to root, but keep the
	// detail leg unpumped: it is in flight while we inspect the frame.
	var leg tea.Cmd
	for _, m := range flattenMsgs(r.upd(tea.KeyPressMsg{Code: tea.KeyDown})) {
		leg = r.upd(m)
	}
	if fake.detailN != 1 {
		t.Fatalf("the unpumped leg must not have queried yet: detailN=%d", fake.detailN)
	}
	body := r.body()
	if !strings.Contains(body, "loading") {
		t.Errorf("in-flight detail load must show the loading marker:\n%s", body)
	}
	if strings.Contains(body, "no transactions recorded") {
		t.Errorf("in-flight detail load must not claim \"no transactions\":\n%s", body)
	}

	for _, m := range flattenMsgs(leg) {
		r.pump(m)
	}
	if strings.Contains(r.body(), "loading") {
		t.Errorf("loading marker survived the fold:\n%s", r.body())
	}
}

// a click-select loads the clicked session's detail through the same seam
// as the keyboard focus leg.
func TestSessionsClickLoadsDetail(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()
	r.pump(selectMsg{region: pages.RegionSessionsList, index: 1})

	if got := r.m.sessionsSelected; got != "77b255c9e4d3" {
		t.Fatalf("sessionsSelected = %q, want the clicked session", got)
	}
	if fake.detailN != 2 {
		t.Fatalf("detail loads = %d, want 2 after the click", fake.detailN)
	}
}

// a detail result landing after a page jump must not fold while another
// page is current; §I re-arms the leg when it becomes current again.
func TestSessionsLateDetailFoldStaysOffPage(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()

	// Arm the focus leg, then jump away while it is still in flight.
	var leg tea.Cmd
	for _, m := range flattenMsgs(r.upd(tea.KeyPressMsg{Code: tea.KeyDown})) {
		leg = r.upd(m)
	}
	r.pump(ch('2'))
	for _, m := range flattenMsgs(leg) {
		r.pump(m) // the late detail result lands on §B
	}
	if r.m.sessionsStats != nil {
		t.Fatal("a late detail result folded while §I was not current")
	}
	if got := r.m.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("current = %q, want still on §B", got)
	}

	// Back on §I the leg re-arms (the stale flag survived the drop).
	before := fake.detailN
	r.gotoPage()
	if fake.detailN <= before {
		t.Fatalf("detail leg must re-arm when §I becomes current again: detailN=%d, want >%d",
			fake.detailN, before)
	}
	if !strings.Contains(r.body(), "150") {
		t.Errorf("stats must show after the re-armed fold:\n%s", r.body())
	}
}

func TestSessionsReviewFlow(t *testing.T) {
	fake := fakeSessionsFixture()
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()
	r.pump(ch('t'))

	body := r.body()
	for _, want := range []string{"REQUEST HEX", "30 32 30 30 f0 00 00 00", "2 (PAN) 4242424242424242", "esc close"} {
		if !strings.Contains(body, want) {
			t.Errorf("review overlay lacks %q:\n%s", want, body)
		}
	}
	if len(fake.reviewIDs) != 1 || fake.reviewIDs[0] != 9 {
		t.Fatalf("review ids = %v, want [9]", fake.reviewIDs)
	}
	// Esc closes the overlay (root stays on the page).
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(r.body(), "REQUEST HEX") {
		t.Errorf("overlay survived Esc:\n%s", r.body())
	}
	if got := r.m.Current().ID(); got != pages.SessionsPageID {
		t.Fatalf("current = %q, want still on §I", got)
	}
}

func TestSessionsReviewErrorIsNoteNotOverlay(t *testing.T) {
	fake := fakeSessionsFixture()
	fake.review = nil
	r := newSessionsTestRoot(t, fake)
	r.gotoPage()
	r.pump(ch('t'))
	if strings.Contains(r.body(), "REQUEST HEX") {
		t.Fatal("overlay opened without a reconstruction")
	}
	if !strings.Contains(r.body(), "not found") {
		t.Errorf("review error note missing:\n%s", r.body())
	}
}
