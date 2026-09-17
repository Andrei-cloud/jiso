// connect_fields.go holds the §E connect-dialog state contract. The
// form is root-built: the root recomputes every Enabled flag from the
// form data and stamps the TLS note; the dialog itself is presentation +
// input routing only and never touches internal/app, the filesystem, or
// the clock.
package pages

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Field keys of the §E form, in render order. The port is shared by both
// modes; the IP is the dial-out host, dimmed and skipped in listener mode.
const (
	ConnectFieldMode        = "mode"
	ConnectFieldIP          = "ip"
	ConnectFieldPort        = "port"
	ConnectFieldHeader      = "header"
	ConnectFieldStation     = "station"
	ConnectFieldUnsolicited = "unsolicited"
	ConnectFieldTLS         = "tls"
)

// ConnectModeListener is the mode radio value that flips the form into
// listener (bind-port) shape; anything else is the caller (dial-out) shape.
const ConnectModeListener = "Listener"

// FieldKind selects how a field renders and consumes keys.
type FieldKind uint8

const (
	// FieldText is a single-line editable value (printable capture).
	FieldText FieldKind = iota
	// FieldRadio is an option row (j/k and arrows adjust, wrapping).
	FieldRadio
	// FieldChecklist is a multi-select option grid: j/k and arrows move
	// the cursor, space toggles; Checked mirrors Options and Value holds
	// the comma-join of the checked options.
	FieldChecklist
	// FieldPicker is a collapsed option row: the selected value plus a ▸
	// affordance; Enter/space open a nested overlay list (type filters,
	// Enter picks, Esc closes keeping the old value). Value/Selected
	// mirror each other; the overlay state is page-owned.
	FieldPicker
)

// FormField is one §E row. Radio fields keep Value mirrored to
// Options[Selected]; Enabled/Note/NoteKind are root-stamped data the
// dialog renders but never derives.
type FormField struct {
	Key      string
	Label    string
	Value    string
	Kind     FieldKind
	Options  []string
	Selected int
	// Checked mirrors Options for FieldChecklist fields (nil until the
	// first toggle for other kinds; clone deep-copies it).
	Checked []bool
	Enabled bool
	Note    string
	NoteKind
	// Browsable marks a path field the file picker can fill: while it
	// is the focused row the dialog footer offers [f] browse.
	Browsable bool
}

// ChecklistValue mirrors Checked onto Options into Value (the canonical
// comma-join root reads) and returns the checked option names.
func (f *FormField) ChecklistValue() []string {
	checked := make([]string, 0, len(f.Options))
	for i, o := range f.Options {
		if i < len(f.Checked) && f.Checked[i] {
			checked = append(checked, o)
		}
	}
	f.Value = strings.Join(checked, ", ")

	return checked
}

// checkedAt reports whether option i is ticked (false when the mirror
// is short).
func (f *FormField) checkedAt(i int) bool {
	return i < len(f.Checked) && f.Checked[i]
}

// toggleAt flips option i, growing the mirror as needed.
func (f *FormField) toggleAt(i int) {
	for len(f.Checked) < len(f.Options) {
		f.Checked = append(f.Checked, false)
	}
	if i < 0 || i >= len(f.Options) {
		return
	}
	f.Checked[i] = !f.Checked[i]
	f.ChecklistValue()
}

// ConnectFormState is the immutable §E snapshot the root pushes into the
// dialog (SetState preserves the page-owned focus). InFlight swaps the
// form for the root-stamped progress line; Error holds the final-failure
// line.
type ConnectFormState struct {
	Fields   []FormField
	InFlight bool
	Progress string // "attempt 2/3" (root-stamped)
	Backoff  string // "backoff 1.5s" (root-stamped, shown while waiting)
	Error    string
	// Title overrides the box title ("" = "CONNECT"; the §G server form
	// uses "SERVER").
	Title string
	// EnterLabel names the Enter action ("" = "connect").
	EnterLabel string
}

// Option lists (§E data), displayed binary2 first. The header options
// mirror the selectable length types of the app's length package; NAPS
// shares the binary2 codec and is offered separately.
var (
	ConnectModeOptions   = []string{"Caller", "Listener"}
	ConnectHeaderOptions = []string{"binary2", "ascii4", "bcd2", "binary4", "NAPS", "visa"}
	ConnectYesNoOptions  = []string{"Yes", "No"}
)

// ConnectHeaderHints annotates each header option, index-aligned with
// ConnectHeaderOptions.
var ConnectHeaderHints = []string{
	"2-digit binary", "4-digit ascii", "2-digit bcd",
	"4-digit binary", "NAPS length", "visa station id",
}

// HeaderHint returns the picker hint for an option ("" when unknown).
func HeaderHint(option string) string {
	for i, o := range ConnectHeaderOptions {
		if strings.EqualFold(o, strings.TrimSpace(option)) {
			return ConnectHeaderHints[i]
		}
	}

	return ""
}

// ConnectHeaderIsVisa reports whether a header option is the visa header
// — the one length type that carries a station ID (case-insensitive).
func ConnectHeaderIsVisa(option string) bool {
	return strings.EqualFold(strings.TrimSpace(option), "visa")
}

// Modal is the overlay contract the connect dialog implements: the root
// renders frame + overlay; the page stack stays untouched.
type Modal interface {
	Update(msg tea.Msg) (Modal, tea.Cmd)
	View() string
}

// clone deep-copies the field slice so callers can mutate a snapshot
// without aliasing the dialog's own state.
func (s ConnectFormState) clone() ConnectFormState {
	out := s
	if s.Fields != nil {
		out.Fields = make([]FormField, len(s.Fields))
		copy(out.Fields, s.Fields)
		for i := range out.Fields {
			if c := s.Fields[i].Checked; c != nil {
				out.Fields[i].Checked = append([]bool(nil), c...)
			}
		}
	}

	return out
}

// Field returns a pointer to the named field (nil when absent).
func (s *ConnectFormState) Field(key string) *FormField {
	for i := range s.Fields {
		if s.Fields[i].Key == key {
			return &s.Fields[i]
		}
	}

	return nil
}
