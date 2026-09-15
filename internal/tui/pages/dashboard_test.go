package pages

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/colorprofile"

	"jiso/internal/app/events"
	"jiso/internal/tui/palette"
)

// TestPageInterfaceAlias proves pages.Page is the interface internal/tui
// aliases: *Dashboard satisfies it without importing its parent package.
var _ Page = (*Dashboard)(nil)

// TestIDAndSlotName: the dashboard fills the "dashboard" boot slot so palette
// page jumps and hotkey 1 keep working.
func TestIDAndSlotName(t *testing.T) {
	t.Parallel()

	d := NewDashboard(nil)
	if got := d.ID(); got != "dashboard" {
		t.Errorf("ID() = %q, want dashboard", got)
	}
}

// TestEmptyStateAllDashes: a zero snapshot renders dashes and the
// teaching empty states — never zero-values, never a fake OFFLINE.
// (ASCII theme: the dash is "-".)
func TestEmptyStateAllDashes(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, DashboardState{}, 120, 32)
	body := strings.Join(bodyLines(t, d), "\n")

	for _, want := range []string{
		"ID" + " -", // "ID - · tx - · ok - · fail -"
		"no send yet", "s sends",
		"no stress run", "t starts one",
		"no server output yet",
		"stopped", "opens the server page",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty state lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "OFFLINE") || strings.Contains(body, "0.0ms") {
		t.Errorf("empty state leaked a zero-value state:\n%s", body)
	}
}

// TestOnlineStateRendersSample: the §A sample snapshot shows status+role,
// target, the folded header/TLS/uptime/retries line, session counters
// with one-decimal latencies, and the last-send / last-stress cards.
func TestOnlineStateRendersSample(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, onlineState(), 120, 32)
	body := strings.Join(bodyLines(t, d), "\n")

	for _, want := range []string{
		"[ok] ONLINE (caller)", "10.0.0.5:8080", "binary2", "TLS mTLS",
		"up 02:20:11", "retries 0", "9f3ca1",
		"tx 148", "ok 146", "fail 2", "avg 3.4ms", "./sessions.db",
		"12:04:09", "Echo", "0200", "0210", "RC 00 APPROVED",
		"1.9ms", "validated", "correlation",
		"w-2", "100.0% ok", "82.3 tps", "p99 0.3ms",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("online state lacks %q:\n%s", want, body)
		}
	}
}

// TestFailedState: failure pairs the error symbol with the word (never
// color alone; ascii theme => "[x]").
func TestFailedState(t *testing.T) {
	t.Parallel()

	st := DashboardState{Conn: ConnectionCard{Status: ConnFailed}}
	d := dashDashboard(t, st, 120, 32)
	body := strings.Join(bodyLines(t, d), "\n")

	if !strings.Contains(body, "[x] FAILED") {
		t.Errorf("failed state lacks [x] FAILED:\n%s", body)
	}
}

// TestDashLeftColIsRelative: UAT round 5 — the wide-grid left column
// grows with the terminal; UAT round 8 finding 5 — it keeps growing at
// its 35% ratio (floor 40 only): the old 64-cell ceiling froze the split
// and starved the ratio on wide terminals.
func TestDashLeftColIsRelative(t *testing.T) {
	t.Parallel()

	if dashLeftCol(200) <= dashLeftCol(140) {
		t.Errorf("left column does not grow: 200→%d, 140→%d", dashLeftCol(200), dashLeftCol(140))
	}
	if got := dashLeftCol(1000); got != 350 {
		t.Errorf("left column must keep its 35%% ratio, got %d (want 350)", got)
	}
	if got := dashLeftCol(130); got < 40 {
		t.Errorf("left column must floor at 40, got %d", got)
	}
}

// TestServer3ColRoutesRelative: UAT round 5 — the wide-layout ROUTES
// column is relative; UAT round 8 finding 5 — it keeps its 28% ratio at
// every width (floor 36 only): the old 64-cell ceiling froze the split
// and left a trailing gap. The three columns plus the two gaps must
// always sum exactly to the content width.
func TestServer3ColRoutesRelative(t *testing.T) {
	t.Parallel()

	s140, l140, r140 := server3ColWidths(140)
	s240, l240, r240 := server3ColWidths(240)
	if r240 <= r140 {
		t.Errorf("routes column does not grow: 240→%d, 140→%d", r240, r140)
	}
	if _, _, got := server3ColWidths(1000); got != 280 {
		t.Errorf("routes column must keep its 28%% ratio, got %d (want 280)", got)
	}
	if s140+l140+r140 != 140-2*serverSectionGap {
		t.Errorf("140: stats %d + log %d + routes %d does not fill the width", s140, l140, r140)
	}
	if s240+l240+r240 != 240-2*serverSectionGap {
		t.Errorf("240: stats %d + log %d + routes %d does not fill the width", s240, l240, r240)
	}
}

// TestRowsCarryNoBadges: UAT round 5 — quick-actions rows render plain
// titles: the "(3)"/"(c)"-style badges duplicated the footer legend and
// advertised keys that do not act on the row (Enter runs the selected
// row, the footer keys jump directly).
func TestRowsCarryNoBadges(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, onlineState(), 120, 32)
	body := strings.Join(bodyLines(t, d), "\n")

	for _, title := range []string{"Send transaction", "Show help"} {
		i := strings.Index(body, title)
		if i < 0 {
			t.Fatalf("body lacks the %q row:\n%s", title, body)
		}
		line := oneLine(body, i)
		after := line[strings.Index(line, title)+len(title):]
		if strings.Contains(after, "(") {
			t.Errorf("row %q still trails a key badge: %q", title, strings.TrimRight(after, " "))
		}
	}
}

// oneLine returns the whole rendered line containing index i of body, with the
// frame's right-hand padding removed so a cell offset means the same thing on
// every row.
func oneLine(body string, i int) string {
	start := strings.LastIndex(body[:i], "\n") + 1
	end := strings.Index(body[i:], "\n")
	if end < 0 {
		end = len(body) - i
	}

	return strings.TrimRight(body[start:i+end], " ")
}

// TestTitlesCarryNoBadges: UAT round 5 — card titles carry NO hotkey
// badge: the "(c)"/"(4)"-style suffix advertised keys whose action lives
// on another screen, duplicating the footer legend and misleading the
// operator into thinking the key acts on the tile.
func TestTitlesCarryNoBadges(t *testing.T) {
	t.Parallel()

	th := testTheme(t, colorprofile.TrueColor)
	d := NewDashboard(th)
	d.SetState(onlineState())
	_, _ = d.Update(windowSize(150, 44))
	view := d.View().Content

	for _, key := range []string{"c", "s", "5", "4", "6"} {
		if want := th.HotKey.Render("(" + key + ")"); strings.Contains(view, want) {
			t.Errorf("titles still carry the HotKey-styled (%s) badge:\n%q", key, view)
		}
	}
	for _, title := range []string{"CONNECTION", "MOCK SERVER", "LAST SEND", "LAST STRESS", "SESSION", "SERVER LOG", "QUICK ACTIONS"} {
		if !strings.Contains(view, title) {
			t.Errorf("titles lack %q", title)
		}
	}

	a := dashDashboard(t, onlineState(), 150, 44)
	abody := strings.Join(bodyLines(t, a), "\n")
	for _, title := range []string{"CONNECTION (c)", "MOCK SERVER (4)", "LAST SEND (s)", "LAST STRESS (5)", "SESSION (6)"} {
		if strings.Contains(abody, title) {
			t.Errorf("ascii titles trail the removed badge %q:\n%s", title, abody)
		}
	}
}

// TestLastSendCardBody: the LAST SEND card renders the wireframe lines
// from a stub card — time + tx, the MTI turn with the RC badge, the
// elapsed/validation line, and the reopen affordance as body copy.
func TestLastSendCardBody(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, onlineState(), 150, 44)
	body := strings.Join(bodyLines(t, d), "\n")

	if !strings.Contains(body, "12:04:09") || !strings.Contains(body, "Echo") {
		t.Errorf("last-send time/name line missing:\n%s", body)
	}
	if !strings.Contains(body, "0200 -> 0210") || !strings.Contains(body, "RC 00 APPROVED") {
		t.Errorf("last-send MTI/RC line missing:\n%s", body)
	}
	if !strings.Contains(body, "1.9ms") || !strings.Contains(body, "[ok] validated") ||
		!strings.Contains(body, "[ok] correlation") {
		t.Errorf("last-send elapsed/validation line missing:\n%s", body)
	}
	if !strings.Contains(body, "enter") {
		t.Errorf("last-send reopen affordance missing:\n%s", body)
	}
	// UAT round 9 (F-9g): the card must NOT advertise "h hexdump" — h
	// toggles the §D send exchange's panes only after you enter open it;
	// on the dashboard there is no view to hexdump, so the glyph was a
	// displayed-but-dead key.
	if strings.Contains(body, "hexdump") {
		t.Errorf("last-send card advertises the unbacked h hexdump glyph:\n%s", body)
	}
}

// TestLastSendFailureState: a failed validation pairs the error symbol
// with the word (never color alone).
func TestLastSendFailureState(t *testing.T) {
	t.Parallel()

	st := DashboardState{LastSend: &LastSendCard{TxName: "Echo", Elapsed: 500 * time.Millisecond}}
	d := dashDashboard(t, st, 150, 44)
	body := strings.Join(bodyLines(t, d), "\n")

	if !strings.Contains(body, "[x] failed") || !strings.Contains(body, "[x] correlation") {
		t.Errorf("failed send must pair error symbols:\n%s", body)
	}
}

// TestMockServerStoppedCard: the stopped MOCK SERVER card is the
// wireframe's empty state teaching the 4 hotkey.
func TestMockServerStoppedCard(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, DashboardState{}, 150, 44)
	body := strings.Join(bodyLines(t, d), "\n")

	if !strings.Contains(body, "o stopped - 4 opens the server page") {
		t.Errorf("stopped server card missing the hotkey-taught empty state:\n%s", body)
	}
}

// TestMockServerRunningCard: the running card carries the live line and
// the stats line; unknown stats render dashes, not zeros.
func TestMockServerRunningCard(t *testing.T) {
	t.Parallel()

	st := DashboardState{Server: ServerCard{Running: true, Port: "9999", Header: "binary2", Uptime: 152 * time.Second}}
	d := dashDashboard(t, st, 150, 44)
	body := strings.Join(bodyLines(t, d), "\n")

	if !strings.Contains(body, "* running :9999 (binary2) - up 02:32 - conns -") {
		t.Errorf("running server line wrong:\n%s", body)
	}
	if !strings.Contains(body, "served - - matched -") {
		t.Errorf("unknown stats must render dashes:\n%s", body)
	}
}

// TestServerLogTailCompaction: the SERVER LOG card compacts raw lines
// with the §G renderer, keeps the newest at the bottom, and the narrow
// stack caps the card at the wireframe's 4 body rows (dropping the
// oldest).
func TestServerLogTailCompaction(t *testing.T) {
	t.Parallel()

	raw := []string{
		"09:17:01 [SERVER] 🟢 Matched Route 'One' for MTI 0800 -> Responding 0810 (RC: 00)",
		"09:17:02 [SERVER] 🟢 Matched Route 'Two' for MTI 0800 -> Responding 0810 (RC: 00)",
		"09:17:03 [SERVER] ⚠️ Fallback (No Route Match) for MTI 0200 -> Responding 0210 (RC: 12)",
		"09:17:04 [SERVER] 🟢 Matched Route 'Four' for MTI 0800 -> Responding 0810 (RC: 00)",
		"09:17:05 [SERVER] 🟢 Matched Route 'Five' for MTI 0800 -> Responding 0810 (RC: 00)",
	}
	st := DashboardState{ServerLog: raw}
	d := dashDashboard(t, st, 90, 60)
	body := strings.Join(bodyLines(t, d), "\n")

	sep := asciiTheme(t).Separator()
	for _, want := range []string{
		"09:17:03 warn Fallback" + sep + "0200->0210" + sep + "RC 12",
		"09:17:05 ok Five" + sep + "0800->0810" + sep + "RC 00",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("compacted tail lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "09:17:01") {
		t.Errorf("narrow cap (4 rows) must drop the oldest line:\n%s", body)
	}
	if i, j := strings.Index(body, "09:17:04"), strings.Index(body, "09:17:05"); i > j || i < 0 {
		t.Errorf("newest line must sit at the bottom (4 at %d, 5 at %d)", i, j)
	}
}

// TestNarrowPriorityOrder: the narrow single column stacks the cards in
// the proposal's priority order, LAST STRESS last.
func TestNarrowPriorityOrder(t *testing.T) {
	t.Parallel()

	lines := bodyLines(t, dashDashboard(t, logGoldState(), 90, 64))

	order := []string{"CONNECTION", "MOCK SERVER", "SERVER LOG", "LAST SEND", "SESSION", "QUICK ACTIONS", "LAST STRESS"}
	prev := -1
	for _, title := range order {
		i := lineIndex(lines, title)
		if i < 0 {
			t.Fatalf("narrow stack lost %q:\n%s", title, strings.Join(lines, "\n"))
		}
		if i <= prev {
			t.Errorf("%q out of priority order (line %d <= %d)", title, i, prev)
		}
		prev = i
	}
}

// TestEnterDispatchesActionMsg: Enter runs the selected action and returns
// a cmd yielding exactly the action's Msg (fake registry).
func TestEnterDispatchesActionMsg(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, onlineState(), 120, 32)
	_, _ = d.Update(press('j')) // down once to "Send transaction"

	_, cmd := d.Update(tea.KeyPressMsg{Code: '\r'})
	if cmd == nil {
		t.Fatal("enter on action returned no cmd")
	}
	if got, ok := cmd().(gotoMsg); !ok || got != "send" {
		t.Errorf("enter dispatched %T %v, want gotoMsg send", cmd(), cmd())
	}
}

// TestStressWizardKeyDispatch: UAT round 9 (F-9g) — the LAST STRESS
// card's "no stress run · t starts one" glyph must be a real page
// binding: t dispatches the very WorkersOpenFormMsg{stress} the §H page
// emits, so root opens the same wizard from either page (the pages
// package never opens the wizard itself — the import fence holds).
func TestStressWizardKeyDispatch(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, DashboardState{}, 120, 32)

	_, cmd := d.Update(press('t'))
	if cmd == nil {
		t.Fatal("t on the dashboard returned no cmd")
	}
	if m, ok := cmd().(WorkersOpenFormMsg); !ok || m.Kind != "stress" {
		t.Errorf("t msg = %#v, want WorkersOpenFormMsg{stress}", cmd())
	}
}

// TestEnterEmptyRegistry: Enter with no actions is a nil-cmd no-op.
func TestEnterEmptyRegistry(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, DashboardState{}, 120, 32)

	_, cmd := d.Update(tea.KeyPressMsg{Code: '\r'})
	if cmd != nil {
		t.Errorf("enter on empty registry = %v, want nil cmd", cmd())
	}
}

// TestCIsNotPageLocal: UAT round 5 removed the page-local "c parks the
// cursor on the connect row" handler — the global c binding always
// claims the key first (connect/disconnect), so the preselect was
// production-dead. At the page, a plain 'c' now just reaches the list
// (which ignores it), leaving the cursor where the user left it.
func TestCIsNotPageLocal(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, onlineState(), 120, 32)
	_, _ = d.Update(press('j'))
	if d.actions.Cursor() != 1 {
		t.Fatalf("cursor after j = %d, want 1", d.actions.Cursor())
	}

	_, _ = d.Update(press('c'))

	if d.actions.Cursor() != 1 {
		t.Errorf("cursor after c = %d, want 1 (no page-local preselect)", d.actions.Cursor())
	}
}

// TestActionsOwnJK: with the EVENT FEED pane gone the quick-actions list
// is the page's only scroll target — j/k move its cursor, and a
// PaneFocusMsg (the old feed/actions toggle) changes nothing.
func TestActionsOwnJK(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, onlineState(), 120, 32)

	_, _ = d.Update(press('j'))
	if d.actions.Cursor() != 1 {
		t.Errorf("actions cursor = %d, want 1", d.actions.Cursor())
	}

	before := d.actions.Cursor()
	_, _ = d.Update(PaneFocusMsg{})
	_, _ = d.Update(PaneFocusMsg{Reverse: true})
	if d.actions.Cursor() != before {
		t.Errorf("PaneFocusMsg must be inert (single focusable list): cursor %d, want %d",
			d.actions.Cursor(), before)
	}
}

// TestUnknownMsgsIgnored: unrelated messages leave state untouched and
// return nil cmds (Update purity).
func TestUnknownMsgsIgnored(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, onlineState(), 120, 32)

	type stranger struct{ N int }
	for _, msg := range []tea.Msg{stranger{1}, events.ConnectionEvent{}, nil} {
		next, cmd := d.Update(msg)
		if cmd != nil {
			t.Errorf("Update(%T) returned a cmd", msg)
		}
		updated, ok := next.(*Dashboard)
		if !ok {
			t.Fatalf("Update(%T) = %T, want *Dashboard", msg, next)
		}
		if updated.actions.Cursor() != d.actions.Cursor() {
			t.Errorf("Update(%T) mutated the page", msg)
		}
	}
}

// TestEventMsgIgnored: the EVENT FEED pane is gone (proposal 05 §3) — a
// stamped bus event is inert on the page (its content lives in the
// CONNECTION card, the status strip and the SERVER LOG card).
func TestEventMsgIgnored(t *testing.T) {
	t.Parallel()

	d := dashDashboard(t, DashboardState{}, 120, 32)

	_, cmd := d.Update(EventMsg{Event: connEv(events.StateConnected, "10.0.0.5:8080"), Time: testTime})
	if cmd != nil {
		t.Errorf("EventMsg returned a cmd")
	}
	body := strings.Join(bodyLines(t, d), "\n")
	if strings.Contains(body, "connected 10.0.0.5:8080") {
		t.Errorf("event text must not render on the page anymore:\n%s", body)
	}
}

// TestUptimeNegativeClamps: negative durations clamp to 00:00:00.
func TestUptimeNegativeClamps(t *testing.T) {
	t.Parallel()

	d := -time.Second
	if got := FormatUptime(&d); got != "00:00:00" {
		t.Errorf("FormatUptime(-1s) = %q", got)
	}
}

// TestShortDur: the card uptime cells are MM:SS (minutes may exceed 59)
// and roll to HH:MM:SS past an hour; formatElapsed renders ms / s /
// HH:MM:SS by magnitude.
func TestShortDur(t *testing.T) {
	t.Parallel()

	if got := shortDur(340 * time.Second); got != "05:40" {
		t.Errorf("shortDur(5m40s) = %q, want 05:40", got)
	}
	if got := shortDur(8411 * time.Second); got != "02:20:11" {
		t.Errorf("shortDur(2h20m11s) = %q, want 02:20:11", got)
	}
	if got := formatElapsed(1900 * time.Microsecond); got != "1.9ms" {
		t.Errorf("formatElapsed(1.9ms) = %q", got)
	}
	if got := formatElapsed(12300 * time.Millisecond); got != "12.3s" {
		t.Errorf("formatElapsed(12.3s) = %q", got)
	}
}

// TestDashboardLastSendRow: the "View last send" quick action renders
// only after a send result exists (UAT), and "Stress summary" only
// after a stress run completed (proposal 05 §3).
func TestDashboardLastSendRow(t *testing.T) {
	t.Parallel()

	d := NewDashboard(testTheme(t, colorprofile.TrueColor))
	d.SetState(DashboardState{Actions: palette.DashboardActions(), HasConnection: true})
	_, _ = d.Update(windowSize(120, 32))
	view := d.View().Content
	if strings.Contains(view, "View last send") {
		t.Fatal("last-send row must stay hidden before any send")
	}
	if strings.Contains(view, "Stress summary") {
		t.Fatal("stress-summary row must stay hidden before any stress run")
	}
	d.SetState(DashboardState{
		Actions: palette.DashboardActions(), HasConnection: true,
		LastSend: &LastSendCard{TxName: "Echo"}, LastStress: &LastStressCard{ID: "w-2"},
	})
	view = d.View().Content
	if !strings.Contains(view, "View last send") {
		t.Fatal("last-send row must render once a send result exists")
	}
	if !strings.Contains(view, "Stress summary") {
		t.Fatal("stress-summary row must render once a stress run completed")
	}
}

// TestDashboardOfflineHidesLiveActions pins the UAT contract: quick
// actions that only work on a live link (send wizard, run scenario,
// start stress test) are hidden while there is no connection; the mock
// server and PCAP rows stay (they work offline).
func TestDashboardOfflineHidesLiveActions(t *testing.T) {
	t.Parallel()

	d := NewDashboard(testTheme(t, colorprofile.TrueColor))
	d.SetState(DashboardState{Actions: palette.DashboardActions()})

	_, _ = d.Update(windowSize(120, 32))
	view := d.View().Content
	for _, hidden := range []string{"Send transaction", "Run scenario", "Start stress test"} {
		if strings.Contains(view, hidden) {
			t.Errorf("offline dashboard shows %q, want it hidden", hidden)
		}
	}
	for _, shown := range []string{"Connect / reconnect", "Start mock server", "Analyze PCAP"} {
		if !strings.Contains(view, shown) {
			t.Errorf("offline dashboard hides %q, want it shown", shown)
		}
	}
}

// TestStressSummaryActionDispatch: Enter on the "Stress summary" row
// dispatches exactly the LastStressSummaryMsg the router interprets —
// the reopen goes through the router's single summary-overlay path, no
// parallel card mechanism.
func TestStressSummaryActionDispatch(t *testing.T) {
	t.Parallel()

	d := NewDashboard(testTheme(t, colorprofile.TrueColor))
	d.SetState(DashboardState{
		Actions:       palette.DashboardActions(),
		HasConnection: true,
		LastStress:    &LastStressCard{ID: "w-2"},
	})
	_, _ = d.Update(windowSize(120, 40))

	for i := 0; i < 32; i++ {
		item, ok := d.actions.Selected()
		if ok {
			if a, ok := item.Data.(palette.Action); ok && a.ID == stressSummaryActionID {
				break
			}
		}
		_, _ = d.Update(press('j'))
	}
	item, ok := d.actions.Selected()
	if !ok {
		t.Fatal("stress-summary row never reachable in the actions list")
	}
	a, ok := item.Data.(palette.Action)
	if !ok {
		t.Fatalf("cursor parked on %v (%T), want %q", item.Data, item.Data, stressSummaryActionID)
	}
	if a.ID != stressSummaryActionID {
		t.Fatalf("cursor parked on %q, want %q", a.ID, stressSummaryActionID)
	}
	_, cmd := d.Update(tea.KeyPressMsg{Code: '\r'})
	if cmd == nil {
		t.Fatal("enter on the stress-summary row returned no cmd")
	}
	if _, ok := cmd().(palette.LastStressSummaryMsg); !ok {
		t.Errorf("enter dispatched %T, want palette.LastStressSummaryMsg", cmd())
	}
}
