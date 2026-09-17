package pages

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/app/events"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
)

// testTime is the fake clock every test stamps events with.
var testTime = time.Date(2026, 9, 6, 12, 4, 11, 0, time.UTC)

// testTheme builds an explicit-profile theme (NewWith is pure, so tests
// build themes concurrently without env interference).
func testTheme(t *testing.T, prof colorprofile.Profile) *theme.Theme {
	t.Helper()

	return theme.NewWith(prof, true)
}

// asciiTheme is the plain-glyph theme most behavioural tests render with.
func asciiTheme(t *testing.T) *theme.Theme { return testTheme(t, colorprofile.ASCII) }

// fakeActions is the fake registry: Enter must dispatch exactly these Msgs.
func fakeActions() []palette.Action {
	return []palette.Action{
		{
			ID: "connect", Title: "Connect / reconnect",
			Run: func([]string) tea.Msg { return gotoMsg("connect-sentinel") },
		},
		{
			ID: "goto.send", Title: "Send transaction", Hints: []string{"2"},
			Run: func([]string) tea.Msg { return gotoMsg("send") },
		},
		{
			ID: "help", Title: "Show help", Hints: []string{"?"},
			Run: func([]string) tea.Msg { return gotoMsg("help") },
		},
	}
}

// gotoMsg is an identifiable router-level Msg for dispatch assertions.
type gotoMsg string

// onlineState is a fully-populated snapshot.
// HasConnection stays false so the fake-actions connect row keeps its
// top position (the dispatch tests press j once to reach "Send
// transaction"); logGoldState adds the connected, live-server shape.
func onlineState() DashboardState {
	up := 8411 * time.Second
	retries := 0

	return DashboardState{
		Conn: ConnectionCard{
			Status: ConnOnline, Role: "caller",
			Target: "10.0.0.5:8080", Header: "binary2", TLS: "mTLS",
			Uptime: &up, Retries: &retries,
		},
		Session: &SessionCard{
			ID: "9f3ca1", TxSent: 148, OK: 146, Fail: 2,
			AvgResponse: 3400 * time.Microsecond, DBPath: "./sessions.db",
			Known: true,
		},
		LastSend: &LastSendCard{
			Time: "12:04:09", TxName: "Echo", ReqMTI: "0200", RespMTI: "0210",
			RC: "00", RCNote: "APPROVED", Elapsed: 1900 * time.Microsecond,
			Validated: true, Correlation: true,
		},
		LastStress: &LastStressCard{
			Time: "12:03:58", ID: "w-2", Done: true, OkPct: "100.0%",
			Workers: "1 worker", TPS: "82.3 tps", P99: "0.3ms",
		},
		Actions: fakeActions(),
	}
}

// dashDashboard builds an ascii dashboard at the given terminal size.
func dashDashboard(t *testing.T, state DashboardState, w, h int) *Dashboard {
	t.Helper()

	d := NewDashboard(asciiTheme(t))
	d.SetState(state)
	_, _ = d.Update(tea.WindowSizeMsg{Width: w, Height: h})

	return d
}

// bodyLines renders the page body (no frame chrome) split into lines.
func bodyLines(t *testing.T, d *Dashboard) []string {
	t.Helper()

	return strings.Split(strings.TrimRight(d.View().Content, "\n"), "\n")
}

// press builds a printable key press.
func press(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Text: string(c)} }

// windowSize builds a terminal size message.
func windowSize(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

// connEv is a ConnectionEvent for feed tests.
func connEv(state, detail string) events.ConnectionEvent {
	return events.ConnectionEvent{State: state, Detail: detail}
}
