// send_scroll_test.go pins §D scrolling (UAT: "on send the request and
// response panes cannot fit the full message parsing, make both panes
// scrollable"): per-pane offsets, per-pane wheel regions, clamps against
// truthful drawn heights, markers that only speak when content overflows,
// and a fresh exchange starting back at the top.
package pages

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/stretchr/testify/require"
)

// scrollTestSend is a completed §D snapshot with a REQUEST pane that
// overflows (80 rows) and a RESPONSE pane that fits (10 rows).
func scrollTestSend(t *testing.T) *Send {
	t.Helper()

	s := NewSend(asciiTheme(t))
	st := SendState{TxID: "Tx A", TxName: "Tx A", Target: "127.0.0.1:9999", Done: true}
	for i := 0; i < 80; i++ {
		st.Request = append(st.Request, ExchangeRow{
			Num: fmt.Sprint(i + 2), Display: fmt.Sprintf("REQ-%03d", i),
		})
	}
	for i := 0; i < 10; i++ {
		st.Response = append(st.Response, ExchangeRow{
			Num: fmt.Sprint(i + 2), Display: fmt.Sprintf("RSP-%03d", i),
		})
	}
	s.SetState(st)
	_, _ = s.Update(tea.WindowSizeMsg{Width: 150, Height: 44})
	_ = s.View().Content // one render records the truthful pane heights

	return s
}

func TestSendScrollKeysClamp(t *testing.T) {
	s := scrollTestSend(t)

	_, _ = s.Update(pressKey('j', "j"))
	require.Equal(t, 1, s.reqScroll)
	require.Zero(t, s.respScroll, "j/k scroll the focused pane, never both")

	_, _ = s.Update(pressKey('k', "k"))
	require.Zero(t, s.reqScroll)

	_, _ = s.Update(special(tea.KeyEnd))
	require.Equal(t, s.maxScroll(true), s.reqScroll)
	require.Greater(t, s.reqScroll, 0, "the request pane must overflow")

	_, _ = s.Update(special(tea.KeyHome))
	require.Zero(t, s.reqScroll)

	// Tab moves the keyboard scroll focus to the response pane; that pane
	// fits, so every key on it stays inert at zero.
	_, _ = s.Update(PaneFocusMsg{})
	_, _ = s.Update(special(tea.KeyEnd))
	require.Zero(t, s.respScroll, "a pane that fits scrolls nowhere")

	// Arrows ride the same offsets as j/k.
	_, _ = s.Update(PaneFocusMsg{})
	_, _ = s.Update(special(tea.KeyDown))
	require.Equal(t, 1, s.reqScroll)
	_, _ = s.Update(special(tea.KeyUp))
	require.Zero(t, s.reqScroll)

	// Page jumps clamp at both ends.
	_, _ = s.Update(special(tea.KeyPgDown))
	require.Greater(t, s.reqScroll, 1)
	for i := 0; i < 40; i++ {
		_, _ = s.Update(special(tea.KeyPgUp))
	}
	require.Zero(t, s.reqScroll)
	for i := 0; i < 40; i++ {
		_, _ = s.Update(special(tea.KeyPgDown))
	}
	require.Equal(t, s.maxScroll(true), s.reqScroll)
}

func TestSendScrollWheelRegions(t *testing.T) {
	s := scrollTestSend(t)
	regions := s.ScrollRegions() // the last render drew nothing yet
	_ = s.View().Content
	regions = s.ScrollRegions()
	require.Len(t, regions, 2)
	require.Equal(t, RegionSendRequest, regions[0].ID)
	require.Equal(t, RegionSendResponse, regions[1].ID)

	require.True(t, s.ScrollRegion(RegionSendRequest, 5))
	require.Equal(t, 5, s.reqScroll)
	require.True(t, s.ScrollRegion(RegionSendResponse, 5), "the wheel owns the pane under the cursor")
	require.Zero(t, s.respScroll, "a fitting pane clamps to zero")
	require.False(t, s.ScrollRegion("nope:zone", 1))

	for i := 0; i < 200; i++ {
		s.ScrollRegion(RegionSendRequest, 1)
	}
	require.Equal(t, s.maxScroll(true), s.reqScroll, "the wheel clamps at the deepest line")
	for i := 0; i < 200; i++ {
		s.ScrollRegion(RegionSendRequest, -1)
	}
	require.Zero(t, s.reqScroll)
}

// The scrolled window is what renders: the first row vanishes, the last
// arrives, and the other pane stays where its own offset says.
func TestSendScrollRenderWindow(t *testing.T) {
	s := scrollTestSend(t)

	out := s.View().Content
	require.Contains(t, out, "REQ-000")
	require.NotContains(t, out, "REQ-079")
	require.NotContains(t, out, "^", "a pane at its top carries no scroll marker")

	s.setFocusedEnd()
	out = s.View().Content
	require.Contains(t, out, "REQ-079")
	require.NotContains(t, out, "REQ-000")
	require.Contains(t, out, "RSP-000", "panes scroll independently")
	require.Contains(t, out, fmt.Sprintf("%d/80", s.reqScroll+1), "the focused pane spells out its position once scrolled")

	s2 := scrollTestSend(t) // no scroll at all
	plain := s2.View().Content
	require.NotContains(t, plain, "/80", "a pane at rest shows arrows, not a position")
}

// A fresh exchange (root pushing a live snapshot after a completed one)
// starts at the top; pushes of the same exchange keep the reader's place.
func TestSendScrollResetsOnFreshExchange(t *testing.T) {
	s := scrollTestSend(t)
	_, _ = s.Update(pressKey('j', "j"))
	require.Equal(t, 1, s.reqScroll)

	same := s.state
	same.Response = append(same.Response, ExchangeRow{Num: "99", Display: "RSP-010"})
	s.SetState(same)
	require.Equal(t, 1, s.reqScroll, "a mid-exchange push must not reset the reader")

	fresh := SendState{TxID: "Tx B", TxName: "Tx B", Target: "127.0.0.1:9999"}
	s.SetState(fresh)
	require.Zero(t, s.reqScroll, "a new exchange starts at its top")
}
