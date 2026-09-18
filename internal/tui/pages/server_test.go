// server_test.go covers the §G page contract: the wire-compat
// id, the symbol+word status header, stats-card formatting (thousands
// separators, match percent), the routes table + detail overlay, the
// start-form hint, the responsive split, and the page→router messages.
// Root-side live-op tests live in internal/tui/root_server_test.go.
package pages

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// serverRoutes is the §G route set (three rows; the third
// drops connections). Match is the summary the root emits — name plus the
// matched MTI (/DE3), or "any" — while Detail still carries every pair.
func serverRoutes() []RouteRow {
	return []RouteRow{
		{
			ID: "0200/proc", Match: "0200/proc 0200/000000", Resp: "0210", Hits: 812, Latency: "100±25ms",
			Detail: RouteDetail{
				Name: "0200/proc", Description: "purchase auth",
				MatchLines: []string{"0=0200", "11=000000", "3=000000"}, RequiredLines: []string{"3", "11"},
				EchoLines: []string{"11"}, RespMTI: "0210",
				RespLines: []string{"39=00"}, Latency: "100ms ±25ms",
			},
		},
		{
			ID: "0800/nmc", Match: "0800/nmc any", Resp: "0810", Hits: 380, Latency: "0ms",
			Detail: RouteDetail{
				Name: "0800/nmc", RespMTI: "0810", Latency: "0ms",
			},
		},
		{
			ID: "0200/proc-mc", Match: "0200/proc-mc 0200", Resp: "0210", Hits: 6, Latency: "50ms",
			Detail: RouteDetail{
				Name: "0200/proc-mc", MatchLines: []string{"0=0200"}, RespMTI: "0210",
				Latency: "50ms", DropConnection: true,
			},
		},
	}
}

// serverRunningState is the §G running snapshot (1,204 served
// of which 1,198 matched = 99.5%, 4 fallback, 2 dropped, 2 req errors,
// 3 live conns, uptime 00:42:11).
func serverRunningState() ServerState {
	return ServerState{
		Running: true, Port: "9999", Header: "binary2",
		Uptime: 42*time.Minute + 11*time.Second,
		Stats: StatsCard{
			Served: 1204, Matched: 1198, MatchPct: "99.5%",
			Fallback: 4, Dropped: 2, ReqErr: 2, LiveConns: 3,
		},
		Routes:     serverRoutes(),
		StatsKnown: true,
	}
}

// serverStoppedState is the never-started §G state: config routes with
// unknown hits and the start-form hint in place of the stats card.
func serverStoppedState() ServerState {
	return ServerState{Routes: serverRoutes()}
}

// serverPage builds the page at a terminal size with the ascii theme.
func serverPage(t *testing.T, state ServerState, w, h int) *Server {
	t.Helper()

	s := NewServer(asciiTheme(t))
	s.SetState(state)
	_, _ = s.Update(windowSize(w, h))

	return s
}

func TestServerIDWireCompat(t *testing.T) {
	t.Parallel()

	if got := NewServer(nil).ID(); got != "server" {
		t.Fatalf("ID = %q, want the wire-compat slot id %q", got, "server")
	}
}

func TestServerHeaderRunning(t *testing.T) {
	t.Parallel()

	lines := bodyLinesOf(t, serverPageProf(t, serverRunningState(), 120, 32, colorprofile.TrueColor))
	head := lines[0]
	for _, want := range []string{"MOCK SERVER", "●", "running", ":9999", "(binary2)", "uptime 00:42:11"} {
		if !strings.Contains(head, want) {
			t.Errorf("running header lacks %q:\n%s", want, head)
		}
	}
}

func TestServerHeaderStopped(t *testing.T) {
	t.Parallel()

	lines := bodyLinesOf(t, serverPageProf(t, serverStoppedState(), 120, 32, colorprofile.TrueColor))
	if head := lines[0]; !strings.Contains(head, "○ stopped") || strings.Contains(head, "uptime") {
		t.Errorf("stopped header wrong:\n%s", head)
	}
}

func TestServerStatsCardFormatting(t *testing.T) {
	t.Parallel()

	body := collapsedBody(t, serverPage(t, serverRunningState(), 120, 32))
	// Compact card: percent and drop_conn are their own
	// indented continuation lines.
	for _, want := range []string{"served 1,204", "matched 1,198", "99.5%", "fallback 4", "drop_conn 2", "req err 2", "live conns 3"} {
		if !strings.Contains(body, want) {
			t.Errorf("stats card lacks %q", want)
		}
	}
}

func TestServerMatchPctUnknownIsDash(t *testing.T) {
	t.Parallel()

	st := serverRunningState()
	st.Stats.MatchPct = ""
	body := collapsedBody(t, serverPage(t, st, 120, 32))
	if !strings.Contains(body, "matched 1,198") || !strings.Contains(body, " - ") {
		t.Errorf("empty match percent must render the dash:\n%s", body)
	}
}

func TestServerMatchPctRoundingIsRoots(t *testing.T) {
	t.Parallel()

	// The page renders MatchPct verbatim; the rounding contract is the
	// root's (matchPct in root_server_test.go). Here: one decimal shows
	// through untouched.
	st := serverRunningState()
	st.Stats.MatchPct = "66.7%"
	body := strings.Join(bodyLinesOf(t, serverPage(t, st, 120, 32)), "\n")
	if !strings.Contains(body, "66.7%") {
		t.Errorf("66.7%% percent missing:\n%s", body)
	}
}

func TestServerRoutesTableRender(t *testing.T) {
	t.Parallel()

	body := strings.Join(bodyLinesOf(t, serverPage(t, serverRunningState(), 120, 32)), "\n")
	for _, want := range []string{"MATCH", "RESP", "HITS", "812", "380"} {
		if !strings.Contains(body, want) {
			t.Errorf("routes table lacks %q", want)
		}
	}
	// The compact routes column set never carries LATENCY
	// (it lives in the route detail overlay instead).
	if strings.Contains(body, "LATENCY") {
		t.Errorf("compact routes table must not carry the LATENCY column at 120:\n%s", body)
	}
}

func TestServerHitsUnknownBeforeSnapshot(t *testing.T) {
	t.Parallel()

	s := serverPage(t, serverStoppedState(), 120, 32)
	body := strings.Join(bodyLinesOf(t, s), "\n")
	if strings.Contains(body, "812") {
		t.Error("never-started hits must not render the config order count")
	}
	for _, want := range []string{"start form (when stopped)", "port - header - spec - routes file", "remembered from your last start"} {
		if !strings.Contains(body, want) {
			t.Errorf("start hint lacks %q", want)
		}
	}
}

func TestServerRoutesFocusNavAndDetail(t *testing.T) {
	t.Parallel()

	s := serverPage(t, serverRunningState(), 120, 32)

	_, _ = s.Update(press('r'))
	if !s.RoutesFocused() {
		t.Fatal("r must focus the routes pane")
	}
	_, _ = s.Update(press('j'))
	_, _ = s.Update(press('j'))
	_, _ = s.Update(specialCode(tea.KeyEnter))

	open, idx := s.DetailOpen()
	if !open || idx != 2 {
		t.Fatalf("detail after r+j+j+enter = (%v,%d), want (true,2)", open, idx)
	}
	body := strings.Join(bodyLinesOf(t, s), "\n")
	for _, want := range []string{"ROUTE 0200/proc-mc", "drops connection"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail overlay lacks %q", want)
		}
	}

	_, _ = s.Update(specialCode(tea.KeyEscape))
	if open, _ := s.DetailOpen(); open {
		t.Fatal("esc must close the detail overlay")
	}
}

func TestServerDetailShowsMatchEchoLatency(t *testing.T) {
	t.Parallel()

	s := serverPage(t, serverRunningState(), 120, 32)
	_, _ = s.Update(press('r'))
	_, _ = s.Update(specialCode(tea.KeyEnter))
	body := strings.Join(bodyLinesOf(t, s), "\n")
	for _, want := range []string{"description", "purchase auth", "11=000000", "3 11", "resp mti", "0210", "100ms ~25ms"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q:\n%s", want, body)
		}
	}
}

func TestServerStartingIndicator(t *testing.T) {
	t.Parallel()

	st := serverStoppedState()
	st.Starting = true
	body := strings.Join(bodyLinesOf(t, serverPage(t, st, 120, 32)), "\n")
	if !strings.Contains(body, "starting") {
		t.Errorf("starting state must show the in-flight line:\n%s", body)
	}
}

func TestServerErrorLineShown(t *testing.T) {
	t.Parallel()

	st := serverStoppedState()
	st.Error = "failed to listen on :9999: bind: address already in use"
	body := strings.Join(bodyLinesOf(t, serverPage(t, st, 120, 32)), "\n")
	if !strings.Contains(body, "address already in use") {
		t.Error("error line missing")
	}
}

func TestServerResponsive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		w    int
		side bool
	}{
		{120, true}, // ≥ frame.FullWidth: STATS and ROUTES share a line
		{90, false},
		{70, false},
	} {
		lines := bodyLinesOf(t, serverPage(t, serverRunningState(), tc.w, 32))
		var shared bool
		for _, l := range lines {
			if strings.Contains(l, "STATS") && strings.Contains(l, "ROUTES") {
				shared = true
			}
		}
		if shared != tc.side {
			t.Fatalf("width %d: side-by-side = %v, want %v\n%s",
				tc.w, shared, tc.side, strings.Join(lines[:min(4, len(lines))], "\n"))
		}
		for i, l := range lines {
			if n := len([]rune(stripANSI(l))); n > tc.w {
				t.Fatalf("width %d: body line %d is %d cells: %q", tc.w, i, n, l)
			}
		}
	}
}

func TestServerHints(t *testing.T) {
	t.Parallel()

	hints := NewServer(nil).Hints()
	want := map[string]bool{"s": false, "r": false, "c": false}
	for _, h := range hints {
		if _, ok := want[h.Key]; ok {
			want[h.Key] = h.Primary
		}
	}
	for k, primary := range want {
		if !primary {
			t.Errorf("hint %q must be primary (narrow-footer survivor)", k)
		}
	}
}

func TestServerPaneFocusTogglesRoutes(t *testing.T) {
	t.Parallel()

	s := serverPage(t, serverRunningState(), 120, 32)
	_, _ = s.Update(PaneFocusMsg{})
	if !s.RoutesFocused() {
		t.Error("tab must focus the routes pane")
	}
	_, _ = s.Update(PaneFocusMsg{Reverse: true})
	if s.RoutesFocused() {
		t.Error("shift+tab must release the routes pane")
	}
}

func TestServerStopMsgYielded(t *testing.T) {
	t.Parallel()

	s := serverPage(t, serverRunningState(), 120, 32)
	_, cmd := s.Update(press('s'))
	if cmd == nil {
		t.Fatal("s must yield a command")
	}
	if _, ok := cmd().(ServerStopMsg); !ok {
		t.Fatalf("s cmd = %T, want ServerStopMsg", cmd())
	}
}

func TestServerPopMsgYielded(t *testing.T) {
	t.Parallel()

	s := serverPage(t, serverRunningState(), 120, 32)
	_, cmd := s.Update(specialCode(tea.KeyEscape))
	if cmd == nil {
		t.Fatal("esc must yield a command")
	}
	if _, ok := cmd().(ServerPopMsg); !ok {
		t.Fatalf("esc cmd = %T, want ServerPopMsg", cmd())
	}
}

// bodyLinesOf renders the page body (no frame chrome) split into lines.
func bodyLinesOf(t *testing.T, s *Server) []string {
	t.Helper()

	return strings.Split(strings.TrimRight(s.View().Content, "\n"), "\n")
}

// serverPageProf builds the page at a terminal size with an explicit
// color profile (truecolor for the ●/○ header glyphs).
func serverPageProf(t *testing.T, state ServerState, w, h int, prof colorprofile.Profile) *Server {
	t.Helper()

	s := NewServer(testTheme(t, prof))
	s.SetState(state)
	_, _ = s.Update(windowSize(w, h))

	return s
}

// collapsedBody is the ANSI-stripped body with runs of spaces collapsed
// to one, so column padding never breaks a "label value" assertion.
func collapsedBody(t *testing.T, s *Server) string {
	t.Helper()

	lines := make([]string, 0, 32)
	for _, l := range bodyLinesOf(t, s) {
		lines = append(lines, strings.Join(strings.Fields(ansi.Strip(l)), " "))
	}

	return strings.Join(lines, "\n")
}

// specialCode builds a named key press (esc, enter, arrows).
func specialCode(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
