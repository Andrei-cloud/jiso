// ctf.go is the §K CTF export page (SCR-511): a two-pane screen —
// SESSIONS (Visa tx-eligible sessions with approved counts, a `/`
// client-side filter) and PARAMETERS (four label+input rows: CIB,
// filter card BIN, batch number, output path) — with the wireframe
// SUMMARY line and a preview overlay (first/last record + counts, Esc
// closes, w writes). It is a reference type kept canonical in the
// router registry, so filter/cursor/form state survives page jumps.
// All data arrives via SetState from root — the page never touches
// internal/app, never opens the database, never writes a file, and
// never reads the clock. Form state is page-local: while PARAMETERS
// holds pane focus the page claims the keyboard and Tab/shift-Tab
// move the field focus ring locally (Esc returns focus to the list);
// the router's Tab moves pane focus while the list holds it. Below
// frame.FullWidth the parameters pane stacks below the list.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Ctf is the §K page.
type Ctf struct {
	th    *theme.Theme
	state CtfState

	list *widgets.Table
	nav  ctfNav

	pane      int
	filtering bool
	filter    string
	view      []CtfSessionRow // filtered list, parallel to list rows
	selID     string          // list row under the cursor (identity-tracked)

	fieldFocus int
	edits      [FormFieldCount]string
	edited     [FormFieldCount]bool

	// previewOpen tracks the overlay per preview identity: a newly
	// pushed Preview re-arms the overlay, Esc closes it (the §I review
	// pattern — the overlay owns Esc first). The record viewer keeps a
	// record cursor, its vertical window offset, and the horizontal
	// column-window offset (records are wider than the terminal; UAT
	// round 6 wireframe).
	previewOpen    bool
	previewShownID int
	recCursor      int
	recOff         int
	colOff         int

	width  int
	height int

	// sections records the geom.Rect of every widgets.Section this
	// page drew during the last render, in draw order and with a
	// content-relative origin (Phase 8's hit-map finalises the
	// absolute offsets into the frame chrome).
	sections []geom.Rect
}

// ctfNav is the page keymap: pane focus arrives as PaneFocusMsg from
// the router (the §C contract); field focus, editing, generate, write,
// filter, and refresh are page-local.
type ctfNav struct {
	Cancel    key.Binding
	Backspace key.Binding
	Enter     key.Binding
	Filter    key.Binding
	Refresh   key.Binding
	Write     key.Binding
	Tab       key.Binding
	TabBack   key.Binding
	Down      key.Binding
	Up        key.Binding
	PgUp      key.Binding
	PgDn      key.Binding
	Left      key.Binding
	Right     key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newCtfNav() ctfNav {
	nav := ctfNav{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		Enter:     key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Refresh:   key.NewBinding(key.WithKeys("r")),
		Write:     key.NewBinding(key.WithKeys("w")),
		Tab:       key.NewBinding(key.WithKeys(theme.KeyTab)),
		TabBack:   key.NewBinding(key.WithKeys("shift+tab")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		PgUp:      key.NewBinding(key.WithKeys("pgup")),
		PgDn:      key.NewBinding(key.WithKeys("pgdown")),
		Left:      key.NewBinding(key.WithKeys("left", "h")),
		Right:     key.NewBinding(key.WithKeys("right", "l")),
	}
	nav.help = []HelpEntry{
		navEntry("move", nav.Up, nav.Down),
		navEntry("record page / columns", nav.PgUp, nav.PgDn, nav.Left, nav.Right),
		actEntry("generate", nav.Enter),
		actEntry("write", nav.Write),
		actEntry("filter", nav.Filter),
		actEntry("reload", nav.Refresh),
		actEntry("pane/field", nav.Tab, nav.TabBack),
		actEntry("close / back", nav.Cancel),
	}

	return nav
}

// NewCtf builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewCtf(th *theme.Theme) *Ctf {
	if th == nil {
		th = theme.Default()
	}
	c := &Ctf{
		th: th, nav: newCtfNav(),
		list: widgets.NewTable(th, ctfMinListWidth),
	}
	c.list.SetGrid(false) // §K pane boxes itself; inner list stays flat
	c.list.SetColumns(ctfListColumns())

	return c
}

// ID reports the router id of this page (CtfPageID; the palette ":ctf"
// jump resolves it; the frame-visible title is §K's "CTF EXPORT").
func (c *Ctf) ID() string { return CtfPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (c *Ctf) Theme() *theme.Theme { return c.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (c *Ctf) Size() (width, height int) { return c.width, c.height }

// Pane reports the focused pane (tests).
func (c *Ctf) Pane() int { return c.pane }

// FieldFocus reports the PARAMETERS field focus ring (tests).
func (c *Ctf) FieldFocus() int { return c.fieldFocus }

// PreviewOpen reports the preview overlay state (tests).
func (c *Ctf) PreviewOpen() bool { return c.previewOpen }

// Filter exposes the live filter text and whether filter mode owns the
// keyboard (root tests; the §B Transactions contract).
func (c *Ctf) Filter() (string, bool) { return c.filter, c.filtering }

// FieldValue exposes one form field's effective value (draft over
// committed), for tests and the view.
func (c *Ctf) FieldValue(i int) string {
	if i >= 0 && i < FormFieldCount && c.edited[i] {
		return c.edits[i]
	}

	return c.state.Params.Field(i)
}

// ClaimsKeyboard implements KeyboardClaimer: the overlay, the live
// filter, and the PARAMETERS form (text fields full of paths with q,
// digits, and :) own the keyboard wholesale while active; Ctrl+C
// stays global.
func (c *Ctf) ClaimsKeyboard() bool {
	return c.previewOpen || c.filtering || c.pane == CtfPaneParams
}

// SelectedSessionID reports the session under the list cursor in the
// filtered view ("" when the view is empty).
func (c *Ctf) SelectedSessionID() string {
	if len(c.view) == 0 {
		return ""
	}

	return c.view[min(c.list.Cursor(), len(c.view)-1)].ID
}

// CommittedParams returns the effective form values (drafts over the
// root-pushed committed values).
func (c *Ctf) CommittedParams() CtfParams {
	return CtfParams{
		CIB:     c.FieldValue(FieldCIB),
		Bin:     c.FieldValue(FieldBin),
		Batch:   c.FieldValue(FieldBatch),
		OutPath: c.FieldValue(FieldOut),
	}
}

// SetState replaces the rendered snapshot (root pushes it on load, on
// every refresh, and after preview/write results). Page-local state
// survives: the list cursor re-places by identity, edited fields keep
// their drafts, and a Preview whose PreviewID differs from the last
// shown one re-arms the overlay.
func (c *Ctf) SetState(state CtfState) {
	c.state = state
	for i := 0; i < FormFieldCount; i++ {
		if !c.edited[i] {
			c.edits[i] = state.Params.Field(i)
		}
	}
	c.rebuild()

	switch {
	case state.Preview == nil:
		c.previewOpen = false
		c.previewShownID = 0
	case state.PreviewID != c.previewShownID:
		c.previewOpen = true
		c.previewShownID = state.PreviewID
		c.recCursor, c.recOff, c.colOff = 0, 0, 0 // a fresh preview re-homes the viewer
	}
}

// rebuild recomposes the filtered session list and re-places the list
// cursor (identity wins over root's SelectedID; the §B/§I contract).
func (c *Ctf) rebuild() {
	f := strings.ToLower(c.filter)
	rows := make([]CtfSessionRow, 0, len(c.state.Sessions))
	for _, r := range c.state.Sessions {
		if f == "" || ctfRowMatches(r, f) {
			rows = append(rows, r)
		}
	}

	display := make([]widgets.Row, len(rows))
	for i, r := range rows {
		display[i] = widgets.Row{dashIf(c.th, r.ShortID), dashIf(c.th, r.When), dashIf(c.th, r.Approved)}
	}
	c.list.SetRows(display)
	c.list.SetEmptyMessage(c.emptyText())

	idx := 0
	if n := len(rows); n > 0 {
		idx = c.rowIndexByID(rows, c.selID)
		if idx < 0 {
			idx = c.rowIndexByID(rows, c.state.SelectedID)
		}
		if idx < 0 {
			idx = min(c.list.Cursor(), n-1)
		}
	}
	c.list.SetCursor(max(idx, 0))
	c.view = rows
	if n := len(rows); n > 0 {
		c.selID = rows[min(c.list.Cursor(), n-1)].ID
	} else {
		c.selID = ""
	}
}

// ctfRowMatches is the client-side filter: the lowercased id (short or
// full), the relative stamp, or the approved cell contains the
// lowercased filter.
func ctfRowMatches(r CtfSessionRow, f string) bool {
	return strings.Contains(strings.ToLower(r.ID+" "+r.ShortID+" "+r.When+" "+r.Approved), f)
}

// rowIndexByID finds a session row by full id, -1 when absent.
func (c *Ctf) rowIndexByID(rows []CtfSessionRow, id string) int {
	if id == "" {
		return -1
	}
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}

	return -1
}

// Update routes sizes, the router's pane-focus tabs, and keys; bus
// events are root-side truth and are ignored with a nil command.
func (c *Ctf) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width, c.height = msg.Width, msg.Height
	case PaneFocusMsg:
		if !c.previewOpen {
			c.pane = (c.pane + ctfPaneCount + boolToStep(msg.Reverse)) % ctfPaneCount
		}
	case tea.KeyPressMsg:
		return c.updateKey(msg)
	}

	return c, nil
}

// updateKey is the page-local keymap: the preview overlay owns the
// keyboard first (Esc closes, w writes), then filter mode, then the
// PARAMETERS form (field focus + editing), then the list pane.
func (c *Ctf) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if c.previewOpen {
		return c.updatePreviewKeys(msg)
	}
	if c.filtering {
		return c.updateFilter(msg)
	}
	if c.pane == CtfPaneParams {
		return c.updateForm(msg)
	}

	switch {
	case key.Matches(msg, c.nav.Filter):
		c.filtering = true

		return c, nil
	case key.Matches(msg, c.nav.Refresh):
		return c, func() tea.Msg { return CtfRefreshMsg{} }
	case key.Matches(msg, c.nav.Enter):
		return c.generate()
	case key.Matches(msg, c.nav.Cancel):
		return c, func() tea.Msg { return CtfPopMsg{} }
	default:
		return c.updateListNav(msg)
	}
}

// generate yields the Enter contract of either pane: preview the
// selected session with the committed form values.
func (c *Ctf) generate() (Page, tea.Cmd) {
	id := c.SelectedSessionID()
	if id == "" {
		return c, nil
	}
	params := c.CommittedParams()

	return c, func() tea.Msg { return CtfGenerateMsg{SessionID: id, Params: params} }
}

// updateForm edits the PARAMETERS pane: Tab/shift-Tab (and up/down)
// move the field focus ring, printable/backspace edit the focused
// field, Enter generates, Esc returns pane focus to the list.
func (c *Ctf) updateForm(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	var cmd tea.Cmd // an edit re-runs the dry leg: the SUMMARY follows the form too
	switch {
	case key.Matches(msg, c.nav.Cancel):
		c.pane = CtfPaneSessions
	case key.Matches(msg, c.nav.TabBack):
		c.fieldFocus = (c.fieldFocus + FormFieldCount - 1) % FormFieldCount
	case key.Matches(msg, c.nav.Tab), key.Matches(msg, c.nav.Down):
		c.fieldFocus = (c.fieldFocus + 1) % FormFieldCount
	case key.Matches(msg, c.nav.Up):
		c.fieldFocus = (c.fieldFocus + FormFieldCount - 1) % FormFieldCount
	case key.Matches(msg, c.nav.Enter):
		return c.generate()
	case key.Matches(msg, c.nav.Backspace):
		c.editField(dropLastRune(c.FieldValue(c.fieldFocus)))
		cmd = c.selectCmd()
	default:
		if r, ok := printableRune(msg.Text); ok {
			c.editField(c.FieldValue(c.fieldFocus) + string(r))
			cmd = c.selectCmd()
		}
	}

	return c, cmd
}

// editField stores an uncommitted draft for the focused field.
func (c *Ctf) editField(v string) {
	c.edits[c.fieldFocus] = v
	c.edited[c.fieldFocus] = true
}

// updateFilter is filter-mode editing (the §B live-filter contract):
// Esc clears, Enter yields the keyboard, printable keys narrow the
// list as typed.
func (c *Ctf) updateFilter(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, c.nav.Cancel):
		c.filter, c.filtering = "", false
		c.rebuild()

		return c, nil
	case key.Matches(msg, c.nav.Backspace):
		c.filter = dropLastRune(c.filter)
		c.rebuild()

		return c, nil
	case key.Matches(msg, c.nav.Enter):
		c.filtering = false

		return c, nil
	}
	if r, ok := printableRune(msg.Text); ok {
		c.filter += string(r)
		c.rebuild()

		return c, nil
	}

	return c, nil
}

// updatePreviewKeys is the record viewer's keyboard (the overlay owns
// it wholesale): Esc closes, w writes, ↑↓/j/k walk records, PgUp/PgDn
// page, ←→/h/l shift the column window (UAT round 6: every record is
// shown and its character position is readable through the rulers).
func (c *Ctf) updatePreviewKeys(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	total := 0
	if c.state.Preview != nil {
		total = len(c.state.Preview.Records)
	}
	_, pageH := c.viewerSize()
	vis := max(pageH-2, 1)

	switch {
	case key.Matches(msg, c.nav.Cancel):
		c.previewOpen = false
	case key.Matches(msg, c.nav.Write):
		return c, func() tea.Msg { return CtfWriteMsg{} }
	case key.Matches(msg, c.nav.Up):
		c.recCursor = max(c.recCursor-1, 0)
	case key.Matches(msg, c.nav.Down):
		c.recCursor = min(c.recCursor+1, max(total-1, 0))
	case key.Matches(msg, c.nav.PgUp):
		c.recCursor = max(c.recCursor-vis, 0)
	case key.Matches(msg, c.nav.PgDn):
		c.recCursor = min(c.recCursor+vis, max(total-1, 0))
	case key.Matches(msg, c.nav.Left):
		c.colOff = max(c.colOff-c.colStep(), 0)
	case key.Matches(msg, c.nav.Right):
		if maxOff := c.maxColOff(); maxOff > 0 {
			c.colOff = min(c.colOff+c.colStep(), maxOff)
		}
	}
	c.scrollRecordsIntoView(vis)

	return c, nil
}

// selectCmd yields the cursor-following dry leg (UAT round 6): the
// summary under the cursor must recalculate, so a cursor move onto a
// different session — or a form edit — asks root for a fresh dry
// preview of the row under the cursor with the committed params.
func (c *Ctf) selectCmd() tea.Cmd {
	id := c.SelectedSessionID()
	if id == "" {
		return nil
	}
	params := c.CommittedParams()

	return func() tea.Msg { return CtfSelectMsg{SessionID: id, Params: params} }
}

// updateListNav forwards navigation to the list table, re-syncs the
// tracked identity, and yields the dry-leg command when the cursor
// landed on a different session.
func (c *Ctf) updateListNav(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	before := c.selID
	next, cmd := c.list.Update(msg)
	c.list = next
	if n := len(c.view); n > 0 {
		c.selID = c.view[min(c.list.Cursor(), n-1)].ID
	}
	if c.selID != before {
		cmd = c.selectCmd()
	}

	return c, cmd
}

// Hints is the §K context keymap; generate/pane/write are primary so
// the narrow footer keeps them (the router appends the global
// bindings).
func (c *Ctf) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "generate", Primary: true},
		{Key: theme.KeyTab, Desc: "pane/field", Primary: true},
		{Key: "w", Desc: "write", Primary: true},
		{Key: "/", Desc: "filter"},
		{Key: "r", Desc: "reload"},
		{Key: theme.KeyEsc, Desc: "close/back", Primary: true},
	}
}
