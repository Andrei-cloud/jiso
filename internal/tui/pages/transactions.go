package pages

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Transactions is the §B transactions page: a filterable, sortable table
// over the loaded tx file. It is a reference type: the router keeps one
// canonical instance in its registry, so filter, sort, and cursor state
// survive page jumps. All app data arrives via SetState from the root
// model — the page never touches internal/app and never reads the clock
// (the SCR-501 data-flow contract dashboard.go established).
//
// Composition rules (ticket SCR-502): the filter keeps a row when the
// lowercased haystack of its display fields contains the lowercased filter
// (substring match, live as you type); sort is a stable sort on the exact
// display cell text the Table widget sees, so page and widget order never
// diverge; selection is tracked by row ID (transaction name) and survives
// recomposition when the selected row still matches, else clamps to the
// nearest valid index.
type Transactions struct {
	th    *theme.Theme
	state TransactionsState

	table *widgets.Table
	nav   txNav

	filtering bool
	filter    string
	sortStep  int // index into sortCycle; 0 = name asc (wireframe default)

	view       []TxRow // filtered+sorted rows, parallel to table rows
	selectedID string  // identity of the row under the cursor

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// txRect is the DRAWN table box (content-relative, measured from the
	// rendered string like every sectionRect) recorded during the last
	// render; the zero value means the table was not on screen (error or
	// empty state). It is the geometry the wheel hit map registers under
	// RegionTxTable.
	txRect geom.Rect
}

// §B owns one wheel-scrollable region (the transactions table) and
// implements the Task 8.2c region seam.
var _ Scroller = (*Transactions)(nil)

// txNav is the page keymap: filter-mode esc/backspace/enter plus the
// page-local triggers; navigation itself is owned by widgets.Table.
type txNav struct {
	Cancel    key.Binding
	Backspace key.Binding
	Enter     key.Binding
	Filter    key.Binding
	Sort      key.Binding
	Send      key.Binding
	PickFile  key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newTxNav() txNav {
	nav := txNav{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		Enter:     key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Sort:      key.NewBinding(key.WithKeys("o")),
		Send:      key.NewBinding(key.WithKeys("s")),
		PickFile:  key.NewBinding(key.WithKeys("f")),
	}
	nav.help = append(tableNavHelp(),
		actEntry("detail", nav.Enter),
		actEntry("send", nav.Send),
		actEntry("pick tx file", nav.PickFile),
		actEntry("filter", nav.Filter),
		actEntry("sort", nav.Sort),
		actEntry("back", nav.Cancel),
	)

	return nav
}

// sortCycle is the `o` order (UAT round 8 D3 moved it off `f`, which now
// picks the tx file everywhere): name → mti → description, ascending
// then descending per column, then wrapping.
var sortCycle = [6]struct {
	col int
	asc bool
}{
	{0, true}, {0, false}, {1, true}, {1, false}, {2, true}, {2, false},
}

// sortColumnNames labels the sort indicator per cycle column.
var sortColumnNames = [3]string{"name", "mti", "description"}

// NewTransactions builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewTransactions(th *theme.Theme) *Transactions {
	if th == nil {
		th = theme.Default()
	}
	t := &Transactions{th: th, nav: newTxNav(), table: widgets.NewTable(th, txMinTableWidth)}
	t.table.SetColumns(txColumns())

	return t
}

// ID reports the router slot this page fills ("send", the §B wire-compat
// slot name; see TransactionsPageID).
func (t *Transactions) ID() string { return TransactionsPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (t *Transactions) Theme() *theme.Theme { return t.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (t *Transactions) Size() (width, height int) { return t.width, t.height }

// Filter exposes the live filter text and whether filter mode owns the
// keyboard (root tests and future deep links).
func (t *Transactions) Filter() (string, bool) { return t.filter, t.filtering }

// SelectedID reports the identity of the row under the cursor in the
// filtered view ("" when the view is empty).
func (t *Transactions) SelectedID() string { return t.selectedID }

// ClaimsKeyboard implements KeyboardClaimer: while the live filter owns
// the keyboard the router forwards every key (except the global graceful
// exit) here, so tx names containing q, digits, or : stay typeable.
func (t *Transactions) ClaimsKeyboard() bool { return t.filtering }

// ScrollRegions publishes the table's drawn rect: the region exists
// exactly while the table is on screen, so the error and empty states
// publish nothing and the wheel over them stays inert.
func (t *Transactions) ScrollRegions() []ScrollRegion {
	if t.txRect.W <= 0 || t.txRect.H <= 0 {
		return nil
	}

	return []ScrollRegion{{ID: RegionTxTable, Rect: t.txRect}}
}

// ScrollRegion drives the SAME row window the pgup/pgdn keys move: the
// wheel's content-direction delta (d>0 = down) passes straight into
// Table.ScrollBy, which clamps at both ends. The window is sized to the
// real pane height on every render (tableGridChrome), so it cannot go
// stale.
func (t *Transactions) ScrollRegion(id string, d int) bool {
	if id != RegionTxTable || t.txRect.W <= 0 {
		return false
	}
	t.table.ScrollBy(d)

	return true
}

// SetState replaces the rendered snapshot (root pushes it on boot and on
// every Update). Filter, sort, and selection are recomposed over the new
// rows: selection is preserved by ID when the row still matches.
func (t *Transactions) SetState(state TransactionsState) {
	prev := t.table.Cursor()
	t.state = state
	t.rebuild(prev)
}

// rebuild recomposes the filtered+sorted view and re-places the cursor.
func (t *Transactions) rebuild(prevCursor int) {
	rows := make([]TxRow, 0, len(t.state.Rows))
	f := strings.ToLower(t.filter)
	for _, r := range t.state.Rows {
		if f == "" || strings.Contains(r.matchText(), f) {
			rows = append(rows, r)
		}
	}

	step := sortCycle[t.sortStep%len(sortCycle)]
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := t.txCell(rows[i], step.col), t.txCell(rows[j], step.col)
		if step.asc {
			return a < b
		}

		return a > b
	})

	display := make([]widgets.Row, len(rows))
	for i, r := range rows {
		display[i] = widgets.Row{
			t.txCell(r, 0), t.txCell(r, 1), t.txCell(r, 2),
			dashIf(t.th, r.Dataset), dashIf(t.th, r.Spec),
		}
	}

	t.table.SetRows(display)
	t.table.SetEmptyMessage(t.emptyText())
	t.selectAfterRebuild(prevCursor, rows)
	t.table.SortBy(step.col, step.asc) // indicator; stable no-op on sorted input
	t.view = rows
}

// selectAfterRebuild preserves the selected row by ID, else clamps the
// previous cursor index into the new view.
func (t *Transactions) selectAfterRebuild(prevCursor int, rows []TxRow) {
	idx := -1
	if t.selectedID != "" {
		for i, r := range rows {
			if r.ID == t.selectedID {
				idx = i

				break
			}
		}
	}
	if idx < 0 {
		idx = min(max(prevCursor, 0), max(len(rows)-1, 0))
	}
	if len(rows) == 0 {
		t.selectedID = ""
		t.table.SetCursor(0)

		return
	}
	t.table.SetCursor(idx)
	t.selectedID = rows[idx].ID
}

// emptyText is the table's empty-state line (distinct per cause).
func (t *Transactions) emptyText() string {
	switch {
	case t.filtering || t.filter != "":
		return "no transactions match filter"
	case t.state.FileName != "":
		return "no transactions in " + t.state.FileName
	default:
		return "no transactions"
	}
}

// Update routes sizes and keys; bus events and pane-focus toggles do not
// concern this single-pane page and are ignored with a nil command.
func (t *Transactions) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return t.updateKey(msg)
	}

	return t, nil
}

// updateKey is the page-local keymap: filter-mode editing first (the live
// filter owns the keyboard), then the page triggers, then table nav.
func (t *Transactions) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if t.filtering {
		switch {
		case key.Matches(msg, t.nav.Cancel):
			t.filter, t.filtering = "", false
			t.rebuild(t.table.Cursor())

			return t, nil
		case key.Matches(msg, t.nav.Backspace):
			t.filter = dropLastRune(t.filter)
			t.rebuild(t.table.Cursor())

			return t, nil
		case key.Matches(msg, t.nav.Enter):
			t.filtering = false // enter applies the filter and yields the keyboard

			return t, nil
		}
		if r, ok := printableRune(msg.Text); ok {
			t.filter += string(r)
			t.rebuild(t.table.Cursor())

			return t, nil
		}
		// Printable keys type (j/k/f/s/t included — a live filter must
		// be typeable); navigation inside the filter runs on the
		// non-printable codes (arrows, pgup/pgdn, home/end).
		return t.updateNav(msg)
	}

	switch {
	case key.Matches(msg, t.nav.Filter):
		t.filtering = true

		return t, nil
	case key.Matches(msg, t.nav.Sort):
		t.sortStep = (t.sortStep + 1) % len(sortCycle)
		t.rebuild(t.table.Cursor())

		return t, nil
	case key.Matches(msg, t.nav.Enter):
		return t, t.yieldSelected(func(id string) tea.Msg { return TxDetailMsg{ID: id} })
	case key.Matches(msg, t.nav.Send):
		return t, t.yieldSelected(func(id string) tea.Msg { return TxSendMsg{ID: id} })
	case key.Matches(msg, t.nav.PickFile):
		return t, func() tea.Msg { return TxPickFileMsg{} }
	case key.Matches(msg, t.nav.Cancel):
		// Proposal 05 §4: esc unwinds to the dashboard (the registry
		// "back" entry finally means back).
		return t, func() tea.Msg { return TxPopMsg{} }
	default:
		return t.updateNav(msg)
	}
}

// updateNav forwards navigation to the table and re-syncs the selected
// identity; unknown keys reach the table and are ignored there.
func (t *Transactions) updateNav(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	var cmd tea.Cmd
	t.table, cmd = t.table.Update(msg)
	if n := len(t.view); n > 0 {
		t.selectedID = t.view[min(t.table.Cursor(), n-1)].ID
	}

	return t, cmd
}

// yieldSelected builds the cmd carrying msgFor(selected row ID), or nil
// when the view is empty.
func (t *Transactions) yieldSelected(msgFor func(string) tea.Msg) tea.Cmd {
	if len(t.view) == 0 {
		return nil
	}
	id := t.view[min(t.table.Cursor(), len(t.view)-1)].ID

	return func() tea.Msg { return msgFor(id) }
}

// Hints is the §B context keymap; detail/send are primary so the narrow
// footer keeps them (the router appends the global bindings). UAT round 8
// D3: `f` picks the tx file (the universal file key), `o` cycles the sort.
func (t *Transactions) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: "/", Desc: "filter"},
		{Key: "o", Desc: "sort"},
		{Key: "f", Desc: "file"},
		{Key: theme.KeyEnter, Desc: "detail", Primary: true},
		{Key: "s", Desc: "send", Primary: true},
		{Key: theme.KeyNavJK, Desc: "nav"},
	}
}

// txCell is the display text of row r's sortable column (the same string
// the Table sees, so page-side and widget-side orders agree).
func (t *Transactions) txCell(r TxRow, col int) string {
	switch col {
	case 0:
		return dashIf(t.th, r.Name)
	case 1:
		return dashIf(t.th, r.MTI)
	default:
		return dashIf(t.th, r.Description)
	}
}

// dropLastRune removes one rune (rune-safe backspace for the filter).
func dropLastRune(s string) string {
	_, size := utf8.DecodeLastRuneInString(s)

	return s[:len(s)-size]
}

// printableRune reports the single printable rune a key press carries
// (control chords and empty Text yield ok=false).
func printableRune(text string) (rune, bool) {
	rs := []rune(text)
	if len(rs) != 1 || !unicode.IsPrint(rs[0]) {
		return 0, false
	}

	return rs[0], true
}
