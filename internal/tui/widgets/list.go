package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// Item is one list entry: Label is the rendered text (may carry ANSI
// styling), Data is an opaque payload for the composing page.
type Item struct {
	Label string
	Data  any
}

// List is a virtualized, selectable list. Only rows inside the visible
// window are rendered (a 10k-item list renders `height` lines per View).
// Selection is the gh-dash pattern from the design contract: left ▸
// marker ("> " in ascii) + full-row selection background from theme
// tokens. Cursor clamping invariants:
//
//   - cursor stays in [0, len-1] and inside [top, top+visible-1];
//   - the window never scrolls past either end (0 <= top <= len-visible).
//
// Zero value is not usable; build with NewList.
type List struct {
	theme  *theme.Theme
	items  []Item
	width  int // total line width incl. the 2-cell selector
	height int // visible row count
	cursor int // absolute index of the cursor row
	top    int // absolute index of the first rendered row
	empty  string
	keys   navKeys
}

// NewList builds an empty list sized to width x height (height = number
// of visible rows; the window scrolls beyond it).
func NewList(th *theme.Theme, width, height int) *List {
	if width < 2 {
		width = 2
	}
	if height < 1 {
		height = 1
	}
	return &List{theme: th, width: width, height: height, empty: DefaultEmptyMessage, keys: newNavKeys()}
}

// SetItems replaces the items and clamps the cursor into range.
func (m *List) SetItems(items []Item) {
	m.items = items
	m.clampCursor()
}

// SetSize resizes the widget and re-clamps cursor and window.
func (m *List) SetSize(width, height int) {
	if width >= 2 {
		m.width = width
	}
	if height >= 1 {
		m.height = height
	}
	m.clampCursor()
}

// SetEmptyMessage overrides the empty-state line.
func (m *List) SetEmptyMessage(s string) { m.empty = s }

// Len reports the total item count.
func (m *List) Len() int { return len(m.items) }

// Cursor reports the absolute cursor index (0 when empty).
func (m *List) Cursor() int { return m.cursor }

// SetCursor moves the cursor (clamped) and drags the window along.
func (m *List) SetCursor(i int) {
	m.cursor = i
	m.clampCursor()
}

// ScrollBy moves the cursor by d rows: d>0 scrolls the content DOWN
// (cursor toward later items), d<0 up. It is the wheel step for Task
// 8.2b and deliberately just wraps SetCursor, so the existing clamping
// bounds both ends and drags the window along; an empty list absorbs it.
func (m *List) ScrollBy(d int) { m.SetCursor(m.cursor + d) }

// Selected returns the item under the cursor; ok is false when empty.
func (m *List) Selected() (Item, bool) {
	if len(m.items) == 0 {
		return Item{}, false
	}
	return m.items[m.cursor], true
}

// Window reports the rendered window: first absolute row and row count.
// Exposed for tests and for pages that want scroll indicators.
func (m *List) Window() (top, count int) {
	top = m.top
	count = m.visibleCount()
	return top, count
}

// visibleCount is how many rows View renders (height, or fewer when the
// item count cannot fill the window).
func (m *List) visibleCount() int {
	if n := len(m.items); n < m.height {
		return n
	}
	return m.height
}

// clampCursor keeps the cursor in range and the window around it valid.
func (m *List) clampCursor() {
	if n := len(m.items); n == 0 {
		m.cursor, m.top = 0, 0
		return
	} else if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampWindow()
}

// clampWindow moves the smallest amount that makes the cursor visible,
// then keeps the window inside [0, len].
func (m *List) clampWindow() {
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+m.height {
		m.top = m.cursor - m.height + 1
	}
	if maxTop := len(m.items) - m.height; m.top > maxTop {
		m.top = maxTop
	}
	if m.top < 0 {
		m.top = 0
	}
}

// Update is the pure transition: navigation keys move the cursor, every
// other message is ignored with a nil command.
func (m *List) Update(msg tea.Msg) (*List, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	page := max(1, m.height-1) // one line of context kept across pages
	switch m.keys.resolve(km) {
	case navUp:
		m.cursor--
	case navDown:
		m.cursor++
	case navPageUp:
		m.cursor -= page
	case navPageDown:
		m.cursor += page
	case navHome:
		m.cursor = 0
	case navEnd:
		m.cursor = len(m.items) - 1
	case navNone:
		return m, nil
	}
	m.clampCursor()

	return m, nil
}

// View renders only the visible window: `▸ label` on the cursor row with
// the theme selection background, `  label` elsewhere, every line padded
// to the widget width. Empty state renders the configured message in the
// muted token.
func (m *List) View() string {
	if len(m.items) == 0 {
		return m.theme.TextMuted.Render(m.empty)
	}
	labelWidth := m.width - 2
	lines := make([]string, 0, m.visibleCount())
	for i := m.top; i < m.top+m.visibleCount(); i++ {
		text := clip(m.items[i].Label, labelWidth, truncateTail(m.theme))
		line := pad(m.theme.Selector(i == m.cursor)+text, m.width)
		if i == m.cursor {
			line = m.theme.Selection.Render(line)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}
