// send_history.go is the send-history overlay (UAT round 5): the
// dashboard used to keep exactly ONE frozen send (LAST SEND); the UAT
// asked for the scrollable history of previously sent transactions
// with detail and hex view modes. This page lists the session's
// completed sends (oldest first, newest at the bottom like the server
// log); Enter freezes the §D exchange view on that entry, whose `h`
// toggle already switches between the Describe detail and the hex
// view. The page is presentation only: the root stamps the ring and
// runs the detail freeze.
package pages

import (
	"strconv"
	"time"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// SendHistoryPageID is the router id of the send-history overlay.
const SendHistoryPageID = "send-history"

// SendHistoryEntry is one completed send: the frozen §D state plus the
// root's completion stamp.
type SendHistoryEntry struct {
	State SendState
	At    time.Time
}

// SendHistoryPickMsg carries the picked entry index (root freezes §D
// on that entry and pushes it).
type SendHistoryPickMsg struct{ Index int }

// SendHistoryPopMsg asks the router to pop the history page.
type SendHistoryPopMsg struct{}

// SendHistory is the overlay page; a reference type kept canonical in
// the root (cursor survives re-pushes while the page stays on the
// stack).
type SendHistory struct {
	th      *theme.Theme
	entries []SendHistoryEntry
	table   *widgets.Table
	nav     sendHistoryNav

	width, height int
}

// sendHistoryNav: the list owns j/k/pgup/pgdn navigation; Enter opens
// the detail, Esc leaves.
type sendHistoryNav struct {
	Cancel   key.Binding
	Enter    key.Binding
	Down     key.Binding
	Up       key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	help []HelpEntry
}

func newSendHistoryNav() sendHistoryNav {
	nav := sendHistoryNav{
		Cancel:   key.NewBinding(key.WithKeys("esc")),
		Enter:    key.NewBinding(key.WithKeys("enter")),
		Down:     key.NewBinding(key.WithKeys("down", "j")),
		Up:       key.NewBinding(key.WithKeys("up", "k")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
	}
	nav.help = append(tableNavHelp(),
		actEntry("open detail", nav.Enter),
		actEntry("back", nav.Cancel),
	)

	return nav
}

// NewSendHistory builds the page. A nil theme selects theme.Default().
func NewSendHistory(th *theme.Theme) *SendHistory {
	if th == nil {
		th = theme.Default()
	}
	s := &SendHistory{
		th:    th,
		nav:   newSendHistoryNav(),
		table: widgets.NewTable(th, 60),
	}
	s.table.SetGrid(false)
	s.table.SetColumns(sendHistoryColumns())
	s.table.SetEmptyMessage("no sends yet this session")

	return s
}

// sendHistoryColumns are the list columns; TRANSACTION gives first on
// narrow panes (the Table's fit clamp never wraps).
func sendHistoryColumns() []widgets.Column {
	return []widgets.Column{
		{Title: "TIME", Width: 8},
		{Title: colTransaction, Width: 20, Flex: true},
		{Title: "RC", Width: 4},
		// STATUS fits the widest honest cell: "[x] timeout".
		{Title: colStatus, Width: 12},
		{Title: "LATENCY", Width: 8, AlignRight: true},
	}
}

// ID reports the router id (SendHistoryPageID).
func (s *SendHistory) ID() string { return SendHistoryPageID }

// Theme exposes the resolved theme.
func (s *SendHistory) Theme() *theme.Theme { return s.th }

// Size reports the last tea.WindowSizeMsg.
func (s *SendHistory) Size() (width, height int) { return s.width, s.height }

// SetEntries replaces the frozen history (root stamps it); the cursor
// is re-plamped and homed on the newest entry when it moved.
func (s *SendHistory) SetEntries(entries []SendHistoryEntry) {
	grew := len(entries) > len(s.entries)
	s.entries = entries
	rows := make([]widgets.Row, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, widgets.Row{
			e.At.Format("15:04:05"),
			dashIf(s.th, e.State.TxName),
			dashIf(s.th, e.State.RC),
			sendHistoryStatus(s.th, e.State),
			s.sendHistoryLatency(e.State),
		})
	}
	s.table.SetRows(rows)
	if grew {
		s.table.SetCursor(len(rows) - 1)
	}
}

// sendHistoryStatus maps a frozen run to its symbol+word cell: every
// resolved stage ok is ok; a timed-out or failed run is fail/timeout.
func sendHistoryStatus(th *theme.Theme, st SendState) string {
	switch {
	case st.TimedOut:
		return th.Status(theme.KindError, TxStatusTimeout)
	case len(st.StageOK) == 0:
		return dashIf(th, "")
	}
	for _, ok := range st.StageOK {
		if !ok {
			return th.Status(theme.KindError, TxStatusFail)
		}
	}

	return th.Status(theme.KindOK, TxStatusOK)
}

// sendHistoryLatency formats the run's elapsed time for the list.
func (s *SendHistory) sendHistoryLatency(st SendState) string {
	if st.Elapsed <= 0 {
		return dashIf(s.th, "")
	}

	return strconv.FormatFloat(float64(st.Elapsed.Microseconds())/1000, 'f', 1, 64) + "ms"
}

// Update routes sizes and keys; bus events and pane focus are ignored.
func (s *SendHistory) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return s.updateKey(msg)
	}

	return s, nil
}

// updateKey routes one key: Enter picks the cursor entry, Esc leaves,
// the rest navigate the table.
func (s *SendHistory) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, s.nav.Cancel):
		return s, func() tea.Msg { return SendHistoryPopMsg{} }
	case key.Matches(msg, s.nav.Enter):
		if n := len(s.entries); n > 0 {
			return s, func() tea.Msg { return SendHistoryPickMsg{Index: min(s.table.Cursor(), n-1)} }
		}

		return s, nil
	default:
		next, cmd := s.table.Update(msg)
		s.table = next

		return s, cmd
	}
}

// View renders the list in a titled box (sessions listBox idiom: the
// table is sized to the box's CONTENT width).
func (s *SendHistory) View() tea.View {
	w, h := frame.ContentSize(s.width, s.height)
	title := titleLine(s.th, "SEND HISTORY")
	inner := max(h-3, 1)
	s.table.SetWidth(max(w-4, 20))
	box := clipBlockStyled(s.th, s.table.View(), inner, max(w-4, 1))

	return tea.NewView(clipBlockStyled(s.th, title+"\n"+
		s.boxStyle().Width(max(w, 4)).Height(inner+2).Render(box), h, w))
}

// boxStyle is the pane border (sessions/servers boxStyle idiom).
func (s *SendHistory) boxStyle() lipgloss.Style {
	b := lipgloss.RoundedBorder()
	if s.th.ASCII {
		b = lipgloss.ASCIIBorder()
	}

	return lipgloss.NewStyle().
		Border(b).
		BorderForeground(s.th.Border.GetBorderTopForeground())
}

// Hints is the send-history context keymap.
func (s *SendHistory) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: "enter", Desc: "open detail", Primary: true},
		{Key: "esc", Desc: "back", Primary: true},
		{Key: theme.KeyNavJK, Desc: hintScroll},
	}
}
