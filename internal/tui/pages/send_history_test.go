package pages

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestSendHistoryListKeys: UAT round 5 — the send-history overlay lists
// the session's completed sends, Enter yields the pick, Esc pops; the
// newest entry is homed at the bottom.
func TestSendHistoryListKeys(t *testing.T) {
	t.Parallel()

	s := NewSendHistory(asciiTheme(t))
	entries := []SendHistoryEntry{
		{State: sendApprovedState(), At: time.Unix(0, 0).Add(time.Hour)},
		{State: sendTimeoutState(), At: time.Unix(0, 0).Add(2 * time.Hour)},
	}
	s.SetEntries(entries)
	_, _ = s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	body := s.View().Content
	for _, want := range []string{"SEND HISTORY", "Purchase", "ok", "timeout"} {
		if !strings.Contains(body, want) {
			t.Errorf("list lacks %q:\n%s", want, body)
		}
	}

	if _, cmd := s.Update(press('j')); cmd != nil {
		t.Fatalf("j must navigate the table without a cmd, got %v", cmd())
	}

	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pick, ok := cmd().(SendHistoryPickMsg)
	if !ok {
		t.Fatalf("enter msg = %#v, want SendHistoryPickMsg", cmd())
	}
	if pick.Index != 1 {
		t.Errorf("index = %d, want 1 (SetEntries homes the cursor on the newest)", pick.Index)
	}

	_, cmd = s.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(SendHistoryPopMsg); !ok {
		t.Fatalf("esc msg = %#v, want SendHistoryPopMsg", cmd())
	}
}

// TestSendHistoryEmptyState: an empty ring renders the honest empty
// line and Enter yields nothing.
func TestSendHistoryEmptyState(t *testing.T) {
	t.Parallel()

	s := NewSendHistory(asciiTheme(t))
	_, _ = s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if body := s.View().Content; !strings.Contains(body, "no sends yet this session") {
		t.Errorf("empty state missing:\n%s", body)
	}
	if _, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter on an empty history must yield no message")
	}
}
