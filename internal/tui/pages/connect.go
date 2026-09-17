// connect.go is the §E connect dialog: a modal overlay (not a page)
// that never touches internal/app, the filesystem, or the clock. The
// form is two-mode: typing a printable enters edit mode and keys go to
// the field; esc leaves the field first, the next esc reaches the router
// (start connect / cancel). Enter commits in both modes.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// ConnectDialog is the §E modal. It is a reference type owned by the root
// while open; SetState preserves the page-owned focus (clamped onto an
// enabled field) and the last size, so root pushes never reset the form.
type ConnectDialog struct {
	th    *theme.Theme
	state ConnectFormState
	focus int

	// editing is the two-mode flag: false = navigate (field highlighted,
	// nothing typed), true = the focused field is being typed into.
	editing bool

	width, height int // last tea.WindowSizeMsg (terminal, not content area)
	nav           connectNav

	// Header picker overlay, page-owned like focus: SetState never touches
	// it. pickerSel indexes the filtered options.
	pickerOpen   bool
	pickerFilter string
	pickerSel    int
}

// PickerOpen reports whether the header picker overlay owns the keyboard
// (the router must route Enter/Esc here before startConnect/cancel).
func (d *ConnectDialog) PickerOpen() bool { return d.pickerOpen }

// FocusedIsPicker reports whether the focused field is a FieldPicker
// (Enter on it opens the overlay instead of starting the action).
func (d *ConnectDialog) FocusedIsPicker() bool {
	f := d.focused()

	return f != nil && f.Kind == FieldPicker
}

// OpenPicker opens (or re-opens) the overlay on the focused picker field,
// seeding the cursor at the current value and clearing the filter.
func (d *ConnectDialog) OpenPicker() {
	f := d.focused()
	if f == nil || f.Kind != FieldPicker {
		return
	}
	d.pickerOpen = true
	d.pickerFilter = ""
	d.pickerSel = f.Selected
}

// ClosePicker dismisses the overlay without applying (Esc).
func (d *ConnectDialog) ClosePicker() { d.pickerOpen = false }

// connectNav is the dialog keymap: focus cycling and radio adjustment.
// Text editing consumes printable runes directly (no bindings: every
// printable key types while a text field is focused).
type connectNav struct {
	FocusNext key.Binding
	FocusPrev key.Binding
	Up        key.Binding
	Down      key.Binding
	Toggle    key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newConnectNav() connectNav {
	nav := connectNav{
		FocusNext: key.NewBinding(key.WithKeys(theme.KeyTab)),
		FocusPrev: key.NewBinding(key.WithKeys("shift+tab")),
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Toggle:    key.NewBinding(key.WithKeys("space")),
	}
	nav.help = []HelpEntry{
		navEntry("focus field", nav.FocusNext, nav.FocusPrev),
		navEntry("move field", nav.Up, nav.Down),
		actEntry("toggle option", nav.Toggle),
	}

	return nav
}

// NewConnectDialog builds a dialog over an empty form; the root pushes the
// built form via SetState before the first View. A nil theme selects
// theme.Default() (production); golden tests inject an explicit profile.
func NewConnectDialog(th *theme.Theme) *ConnectDialog {
	if th == nil {
		th = theme.Default()
	}

	return &ConnectDialog{th: th, nav: newConnectNav()}
}

// Theme exposes the resolved theme (view helpers and tests).
func (d *ConnectDialog) Theme() *theme.Theme { return d.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (d *ConnectDialog) Size() (width, height int) { return d.width, d.height }

// State returns a deep copy of the rendered snapshot (tests, root sync).
func (d *ConnectDialog) State() ConnectFormState { return d.state.clone() }

// Focus reports the focused field index (always an enabled field while any
// enabled field exists).
func (d *ConnectDialog) Focus() int { return d.focus }

// Editing reports whether the focused field is being typed into; the
// root router browses with f only while this is false.
func (d *ConnectDialog) Editing() bool { return d.editing }

// SetFocus moves the highlighted field, clamping onto the nearest enabled
// one; the new field lands in navigate mode and an open header picker
// closes, so no stale overlay stays drawn over the field taking focus.
func (d *ConnectDialog) SetFocus(i int) {
	d.focus = clampFocus(d.state.Fields, i)
	d.editing = false
	d.pickerOpen = false
}

// SetState replaces the rendered snapshot, preserving the page-owned
// focus (moved to the nearest enabled field if the old one got disabled)
// and the last size. The edit mode survives a root push; it clears when
// the clamp moves the focus.
func (d *ConnectDialog) SetState(state ConnectFormState) {
	d.state = state
	if next := clampFocus(state.Fields, d.focus); next != d.focus {
		d.focus = next
		d.editing = false
	}
}

// Update routes sizes and keys; everything else is ignored with a nil
// command. While InFlight the form is frozen (progress line only): every
// key except the router-owned esc/enter is swallowed.
func (d *ConnectDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width, d.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		d.updateKey(msg)
	}

	return d, nil
}

// updateKey is the edit state machine. Two-mode esc order: esc while
// editing leaves the FIELD first (focus and value untouched); the router
// only sees the esc that leaves the screen.
func (d *ConnectDialog) updateKey(msg tea.KeyPressMsg) {
	if d.state.InFlight {
		return
	}
	if len(d.state.Fields) == 0 {
		return
	}

	if d.pickerOpen {
		if key.Matches(msg, d.nav.FocusNext) || key.Matches(msg, d.nav.FocusPrev) {
			d.pickerOpen = false
			dir := 1
			if key.Matches(msg, d.nav.FocusPrev) {
				dir = -1
			}
			d.focus = d.moveFocus(dir)
		} else {
			d.updatePicker(msg)
		}

		return
	}

	if d.editing && key.Matches(msg, key.NewBinding(key.WithKeys(theme.KeyEsc))) {
		d.editing = false // esc leaves the field before it leaves the screen

		return
	}

	switch {
	case key.Matches(msg, d.nav.FocusNext):
		d.focus = d.moveFocus(+1)
		d.editing = false // a newly-highlighted field starts in navigate mode
	case key.Matches(msg, d.nav.FocusPrev):
		d.focus = d.moveFocus(-1)
		d.editing = false
	default:
		d.editFocused(msg)
	}
}

// updatePicker routes one key while the header overlay owns the keyboard:
// j/k/arrows move, Enter applies, Esc dismisses, printables filter.
// Enter/Esc reach here only because the router checks PickerOpen first.
func (d *ConnectDialog) updatePicker(msg tea.KeyPressMsg) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys(theme.KeyEnter))):
		d.pickerPick()
	case key.Matches(msg, key.NewBinding(key.WithKeys(theme.KeyEsc))):
		d.pickerOpen = false
	case key.Matches(msg, d.nav.Up):
		d.pickerSel = max(d.pickerSel-1, 0)
	case key.Matches(msg, d.nav.Down):
		d.pickerSel = min(d.pickerSel+1, max(len(d.pickerFiltered())-1, 0))
	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		if r := []rune(d.pickerFilter); len(r) > 0 {
			d.pickerFilter = string(r[:len(r)-1])
		}
		d.clampPickerSel()
	default:
		if r, ok := printableRune(msg.Text); ok {
			d.pickerFilter += string(r)
			d.clampPickerSel()
		}
	}
}

// pickerFiltered returns the option indices matching the filter
// (case-insensitive substring).
func (d *ConnectDialog) pickerFiltered() []int {
	f := d.focused()
	if f == nil {
		return nil
	}
	filter := strings.ToLower(d.pickerFilter)
	idx := make([]int, 0, len(f.Options))
	for i, o := range f.Options {
		if filter == "" || strings.Contains(strings.ToLower(o), filter) {
			idx = append(idx, i)
		}
	}

	return idx
}

// clampPickerSel keeps the overlay cursor inside the filtered list.
func (d *ConnectDialog) clampPickerSel() {
	if n := len(d.pickerFiltered()); d.pickerSel >= n {
		d.pickerSel = max(n-1, 0)
	}
}

// pickerPick applies the overlay selection into the field and closes.
func (d *ConnectDialog) pickerPick() {
	d.pickerOpen = false
	f := d.focused()
	if f == nil {
		return
	}
	idx := d.pickerFiltered()
	if len(idx) == 0 || d.pickerSel >= len(idx) {
		return
	}
	f.Selected = idx[d.pickerSel]
	f.Value = f.Options[f.Selected]
}

// editFocused routes input to the focused field: arrows/j/k adjust radios
// and checklists; printables and backspace edit text fields (entering edit
// mode). The router owns ctrl chords, enter, and esc.
func (d *ConnectDialog) editFocused(msg tea.KeyPressMsg) {
	f := d.focused()
	if f == nil {
		return
	}

	if f.Kind == FieldChecklist {
		switch {
		case key.Matches(msg, d.nav.Up):
			d.moveChecklist(f, -1)
		case key.Matches(msg, d.nav.Down):
			d.moveChecklist(f, +1)
		case key.Matches(msg, d.nav.Toggle):
			f.toggleAt(f.Selected)
		}

		return
	}

	if f.Kind == FieldRadio {
		switch {
		case key.Matches(msg, d.nav.Up):
			d.adjustRadio(f, -1)
		case key.Matches(msg, d.nav.Down):
			d.adjustRadio(f, +1)
		}

		return
	}

	if f.Kind == FieldPicker {
		if key.Matches(msg, d.nav.Toggle) {
			d.OpenPicker()
		}

		return
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		if r := []rune(f.Value); len(r) > 0 {
			f.Value = string(r[:len(r)-1])
		}
		d.editing = true // clearing a value is typing into the field
	default:
		if r, ok := printableRune(msg.Text); ok {
			f.Value += string(r)
			d.editing = true // the first printable enters edit mode (D3)
		}
	}
}

// focused returns the focused field (nil when the focus is out of range or
// the field is disabled — SetState's clamp keeps that impossible in
// practice).
func (d *ConnectDialog) focused() *FormField {
	if d.focus < 0 || d.focus >= len(d.state.Fields) {
		return nil
	}
	f := &d.state.Fields[d.focus]
	if !f.Enabled {
		return nil
	}

	return f
}

// moveFocus advances from the current focus by dir, skipping disabled
// fields and wrapping; with no enabled field it stays put.
func (d *ConnectDialog) moveFocus(dir int) int {
	n := len(d.state.Fields)
	if n == 0 {
		return 0
	}
	i := d.focus
	for range n {
		i = (i + dir + n) % n
		if d.state.Fields[i].Enabled {
			return i
		}
	}

	return d.focus
}

// moveChecklist moves the checklist cursor by delta with wrap (the
// cursor is Selected; Checked is the tick mirror).
func (d *ConnectDialog) moveChecklist(f *FormField, delta int) {
	n := len(f.Options)
	if n == 0 {
		return
	}
	f.Selected = (f.Selected + delta + n) % n
}

// adjustRadio moves the selection by delta with wrap and re-mirrors Value.
func (d *ConnectDialog) adjustRadio(f *FormField, delta int) {
	n := len(f.Options)
	if n == 0 {
		return
	}
	f.Selected = (f.Selected + delta + n) % n
	f.Value = f.Options[f.Selected]
}

// clampFocus returns focus when it names an enabled field, else the nearest
// enabled field scanning forward (wrapping), else 0.
func clampFocus(fields []FormField, focus int) int {
	n := len(fields)
	if n == 0 {
		return 0
	}
	if focus >= 0 && focus < n && fields[focus].Enabled {
		return focus
	}
	if focus < 0 || focus >= n {
		focus = 0
	}
	for i := range n {
		j := (focus + i) % n
		if fields[j].Enabled {
			return j
		}
	}

	return 0
}

var _ Modal = (*ConnectDialog)(nil)
