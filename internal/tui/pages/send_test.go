package pages

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// sendTestRows are the §D panes: PAN/STAN echo, field 38
// response-only, RC 00 approves. Hex columns are the true value encodings
// (the `h` toggle is pure display over identical rows).
func sendTestRows() (req, resp []ExchangeRow) {
	req = []ExchangeRow{
		{Num: "0", Display: "0200", Hex: "30323030"},
		{Num: "2", Display: "4242*4242", Hex: "343234323432"},
		{Num: "11", Display: "041822", Hex: "303431383232"},
	}
	resp = []ExchangeRow{
		{Num: "0", Display: "0210", Hex: "30323130"},
		{Num: "2", Display: "4242*4242", Hex: "343234323432", Note: "echo", NoteKind: NotePass},
		{Num: "11", Display: "041822", Hex: "303431383232", Note: "STAN", NoteKind: NotePass},
		{Num: "38", Display: "482913", Hex: "343832393133", Note: "auth code", NoteKind: NoteInfo},
		{Num: "39", Display: "00", Hex: "3030"},
	}

	return req, resp
}

// sendApprovedState is the completed §D snapshot.
func sendApprovedState() SendState {
	req, resp := sendTestRows()

	return SendState{
		TxID: "Purchase", TxName: "Purchase", Target: "10.0.0.5:8080",
		Request: req, Response: resp,
		Stage: 4, StageOK: []bool{true, true, true, true, true},
		Elapsed: 3200 * time.Microsecond, Budget: 5 * time.Second,
		Attempt: 1,
		Done:    true, RC: "00", RCLabel: "APPROVED", RCok: true,
		Validated: true, CorrelationOK: true,
	}
}

// sendTimeoutState is the timed-out snapshot: Receive ✗, no response rows.
func sendTimeoutState() SendState {
	req, _ := sendTestRows()

	return SendState{
		TxID: "Purchase", TxName: "Purchase", Target: "10.0.0.5:8080",
		Request: req,
		Stage:   2, StageOK: []bool{true, true, false},
		Elapsed: 5 * time.Second, Budget: 5 * time.Second,
		Done: true, TimedOut: true,
	}
}

func newSendAt(t *testing.T, prof colorprofile.Profile, st SendState, w, h int) *Send {
	t.Helper()

	s := NewSend(testTheme(t, prof))
	s.SetState(st)
	_, _ = s.Update(windowSize(w, h))

	return s
}

func sendBody(t *testing.T, s *Send) string {
	t.Helper()

	return s.View().Content
}

// TestSendPanesSideBySideWide: at 120 the request/response pane titles
// share a line.
func TestSendPanesSideBySideWide(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.ASCII, sendApprovedState(), 120, 32)

	for _, line := range strings.Split(sendBody(t, s), "\n") {
		if strings.Contains(line, "REQUEST") && strings.Contains(line, "RESPONSE") {
			return
		}
	}
	t.Fatalf("no line carries both pane titles at width 120:\n%s", sendBody(t, s))
}

// TestSendPanesStackedNarrow: below frame.FullWidth (90 and 70) the
// response pane starts on a later line than the request pane.
func TestSendPanesStackedNarrow(t *testing.T) {
	t.Parallel()

	for _, w := range []int{90, 70} {
		s := newSendAt(t, colorprofile.ASCII, sendApprovedState(), w, 32)

		reqAt, respAt := -1, -1
		for j, line := range strings.Split(sendBody(t, s), "\n") {
			switch {
			case reqAt < 0 && strings.Contains(line, "REQUEST"):
				reqAt = j
			case strings.Contains(line, "RESPONSE"):
				respAt = j
			}
		}
		if reqAt < 0 || respAt <= reqAt {
			t.Errorf("width %d: REQUEST line %d, RESPONSE line %d — expected stacked", w, reqAt, respAt)
		}
	}
}

// TestSendHexTogglePureDisplay: `h` swaps the Display column for the Hex
// column of the same rows (same line count, notes preserved).
func TestSendHexTogglePureDisplay(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.ASCII, sendApprovedState(), 120, 32)
	before := strings.Split(sendBody(t, s), "\n")

	_, _ = s.Update(press('h'))
	after := strings.Split(sendBody(t, s), "\n")

	if len(after) != len(before) {
		t.Fatalf("line count moved %d → %d; the toggle must be pure display", len(before), len(after))
	}
	body := strings.Join(after, "\n")
	if !strings.Contains(body, "303431383232") {
		t.Error("hex column not shown after h")
	}
	if strings.Contains(body, "041822") {
		t.Error("display column still shown after h")
	}
	if !strings.Contains(body, "auth code") {
		t.Error("notes lost in hex view")
	}
	if !s.State().HexOn {
		t.Error("HexOn not set")
	}
}

// TestSendSetStatePreservesHexOn: root pushes snapshots on every Update;
// the page-owned toggle survives them.
func TestSendSetStatePreservesHexOn(t *testing.T) {
	t.Parallel()

	s := NewSend(asciiTheme(t))
	s.SetState(sendApprovedState())
	_, _ = s.Update(press('h'))
	s.SetState(sendApprovedState())
	if !s.State().HexOn {
		t.Fatal("SetState clobbered the page-owned HexOn toggle")
	}
}

// TestSendRCBadgeRender: approved on the ok style, declined on the error
// style, unknown codes code-only (no label).
func TestSendRCBadgeRender(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		st   SendState
		want string
		deny string
	}{
		{"approved", sendApprovedState(), "RC 00 APPROVED", "DECLINED"},
		{"declined", declinedState(), "RC 96 DECLINED", "APPROVED"},
		{"unknown", unknownRCState(), "RC 05", "APPROVED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSendAt(t, colorprofile.TrueColor, tc.st, 120, 32)
			body := sendBody(t, s)
			if !strings.Contains(body, tc.want) {
				t.Errorf("badge %q missing:\n%s", tc.want, body)
			}
			if strings.Contains(body, tc.deny) {
				t.Errorf("unexpected %q in badge", tc.deny)
			}
		})
	}
}

func declinedState() SendState {
	st := sendApprovedState()
	for i := range st.Response {
		if st.Response[i].Num == "39" {
			st.Response[i].Display = "96"
		}
	}
	st.RC, st.RCLabel, st.RCok = "96", "DECLINED", false

	return st
}

func unknownRCState() SendState {
	st := sendApprovedState()
	for i := range st.Response {
		if st.Response[i].Num == "39" {
			st.Response[i].Display = "05"
		}
	}
	st.RC, st.RCLabel, st.RCok = "05", "", false

	return st
}

// TestSendTimeoutBanner: the timed-out view shows the ✗ TIMEOUT banner via
// the theme error kind (ascii "[x] TIMEOUT") and freezes the elapsed.
func TestSendTimeoutBanner(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		prof colorprofile.Profile
		want string
	}{
		{colorprofile.TrueColor, "✗ TIMEOUT"},
		{colorprofile.ASCII, "[x] TIMEOUT"},
	} {
		s := newSendAt(t, tc.prof, sendTimeoutState(), 120, 32)
		if body := sendBody(t, s); !strings.Contains(body, tc.want) {
			t.Errorf("profile %v: no %q banner:\n%s", tc.prof, tc.want, body)
		}
	}
}

// TestSendStageIndicator: the five stage names joined by the separator
// with per-stage glyphs; pending stages dim, and ascii mode is 7-bit.
func TestSendStageIndicator(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.TrueColor, sendApprovedState(), 120, 32)
	body := sendBody(t, s)
	for _, want := range []string{"Connect", "Send", "Receive", "Parse", "Validate", "▸"} {
		if !strings.Contains(body, want) {
			t.Errorf("indicator lacks %q", want)
		}
	}

	a := newSendAt(t, colorprofile.ASCII, sendTimeoutState(), 120, 32)
	abody := sendBody(t, a)
	if !strings.Contains(abody, ">") || !strings.Contains(abody, "..") {
		t.Errorf("ascii indicator lacks > and .. separators:\n%s", abody)
	}
	for _, r := range abody {
		if r > 127 {
			t.Fatalf("ascii render carries non-ASCII rune %q", r)
		}
	}
}

// TestSendInFlightWaiting: while the op runs the response pane header
// carries the waiting line with live elapsed and budget.
func TestSendInFlightWaiting(t *testing.T) {
	t.Parallel()

	st := sendApprovedState()
	st.Done, st.TimedOut = false, false
	st.Stage, st.StageOK = 1, []bool{true}
	st.Response = nil
	st.Elapsed = 2100 * time.Millisecond

	s := newSendAt(t, colorprofile.TrueColor, st, 120, 32)
	body := sendBody(t, s)
	if !strings.Contains(body, "waiting for response") || !strings.Contains(body, "2.1s / budget 5s") {
		t.Errorf("no waiting line with elapsed/budget:\n%s", body)
	}
	if strings.Contains(body, "APPROVED") {
		t.Error("badge shown before completion")
	}
}

// stripANSI flattens styled output to its visible text (widgets idiom).
func stripANSI(s string) string { return ansi.Strip(s) }

// TestSendNotesRender: echo/STAN/auth-code notes render with the theme
// symbols (never color alone).
func TestSendNotesRender(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.TrueColor, sendApprovedState(), 120, 32)
	body := stripANSI(sendBody(t, s))
	for _, want := range []string{"(echo ✓)", "(STAN ✓)", "(auth code)"} {
		if !strings.Contains(body, want) {
			t.Errorf("notes lack %q:\n%s", want, body)
		}
	}
}

// TestSendStatusLine: completion line carries elapsed/attempt/validated/
// correlation.
func TestSendStatusLine(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.TrueColor, sendApprovedState(), 120, 32)
	body := stripANSI(sendBody(t, s))
	if !strings.Contains(body, "elapsed 3.2ms") || !strings.Contains(body, "attempt 1") ||
		!strings.Contains(body, "validated ✓") ||
		!strings.Contains(body, "correlation ✓") {
		t.Errorf("status line wrong:\n%s", body)
	}
}

// TestSendHintsAndIDs: page id, Enter/Esc/h hints.
func TestSendHintsAndIDs(t *testing.T) {
	t.Parallel()

	s := NewSend(asciiTheme(t))
	if s.ID() != SendPageID {
		t.Fatalf("ID = %q, want %q", s.ID(), SendPageID)
	}
	hints := s.Hints()
	if len(hints) != 3 {
		t.Fatalf("hints = %+v", hints)
	}
	if hints[0].Key != "enter" || hints[1].Key != "esc" || hints[2].Key != "h" {
		t.Errorf("hints = %+v", hints)
	}
}

// TestSendEnterYieldsTxSendMsg: Enter re-sends through the same TxSendMsg
// path as the §B page (root's one-in-flight rule then applies).
func TestSendEnterYieldsTxSendMsg(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.ASCII, sendApprovedState(), 120, 32)
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no cmd")
	}
	if msg, ok := cmd().(TxSendMsg); !ok || msg.ID != "Purchase" {
		t.Fatalf("enter yielded %T %v, want TxSendMsg{Purchase}", cmd(), cmd())
	}
}

// TestSendEscYieldsPop: Esc yields the router pop request.
func TestSendEscYieldsPop(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.ASCII, sendApprovedState(), 120, 32)
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc produced no cmd")
	}
	if _, ok := cmd().(SendPopMsg); !ok {
		t.Fatalf("esc yielded %T, want SendPopMsg", cmd())
	}
}

// TestSendFormatElapsed: the timer formats like the design ("3.2ms",
// "2.1s", budget "5s").
func TestSendFormatElapsed(t *testing.T) {
	t.Parallel()

	if got := formatSendElapsed(3200 * time.Microsecond); got != "3.2ms" {
		t.Errorf("3.2ms → %q", got)
	}
	if got := formatSendElapsed(2100 * time.Millisecond); got != "2.1s" {
		t.Errorf("2.1s → %q", got)
	}
	if got := formatSendBudget(5 * time.Second); got != "5s" {
		t.Errorf("budget 5s → %q", got)
	}
}

// TestSendEmptyPanes: an empty snapshot renders pending markers, not zeros.
func TestSendEmptyPanes(t *testing.T) {
	t.Parallel()

	s := newSendAt(t, colorprofile.ASCII, SendState{}, 120, 32)
	body := sendBody(t, s)
	if !strings.Contains(body, "..") {
		t.Errorf("empty panes lack pending markers:\n%s", body)
	}
}

// TestSendViewHeight: the body is exactly the content height at each
// width (never wraps, never overflows).
func TestSendViewHeight(t *testing.T) {
	t.Parallel()

	for _, w := range []int{120, 90, 70} {
		s := newSendAt(t, colorprofile.ASCII, sendApprovedState(), w, 32)
		lines := strings.Split(strings.TrimRight(sendBody(t, s), "\n"), "\n")
		if len(lines) > 32 {
			t.Errorf("width %d: %d lines exceed the terminal height", w, len(lines))
		}
	}
}

// TestSendHexToggleSwapsWholePane pins the UAT contract: h switches the
// panes from the Describe rows to the standard hexdump of the packed
// messages (and back), not a per-row hex column.
func TestSendHexToggleSwapsWholePane(t *testing.T) {
	t.Parallel()

	s := NewSend(testTheme(t, colorprofile.TrueColor))
	s.SetState(SendState{
		TxID: "tx-1", TxName: "Echo", Target: "10.0.0.5:8080", Done: true,
		Request: []ExchangeRow{{Num: "7", Text: "F7   Transmission Date & Time..: 0910191523", Display: "0910191523"}},
		// Request/Response dump lines (standard hexdump layout).
		RequestHex:  []string{"00000000  30 38 30 30 82 20 00 00  00 00 00 00 04 00        |0800. .......|"},
		ResponseHex: []string{"00000000  30 38 31 30 02 20 00 02  00 00 00 00              |0810. .....|"},
	})
	_, _ = s.Update(windowSize(120, 32))
	view := s.View().Content
	if !strings.Contains(view, "Transmission Date & Time") {
		t.Fatal("describe rows missing in default view")
	}

	_, _ = s.Update(pressKey('h', "h"))
	view = s.View().Content
	if !strings.Contains(view, "30 38 30 30 82 20") || !strings.Contains(view, "|0800") {
		t.Errorf("hex view missing the standard hexdump lines\n%s", view)
	}
	if strings.Contains(view, "Transmission Date & Time") {
		t.Error("describe rows still visible while hexdump is on")
	}

	_, _ = s.Update(pressKey('h', "h"))
	if !strings.Contains(s.View().Content, "Transmission Date & Time") {
		t.Error("toggling back did not restore the Describe rows")
	}
}
