// connect_fields.go holds the §E connect-dialog state contract
// (wireframe §E, SCR-505). The form is root-built: the root model builds
// the initial ConnectFormState from config/session prefill, recomputes
// every field's Enabled flag from the form DATA on each relevant change,
// and stamps the TLS note after a filesystem check — the dialog itself is
// presentation + input routing only and never touches internal/app, the
// filesystem, or the clock.
package pages

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Field keys of the §E form, in render order. IP and Port replace the old
// combined Target/Bind pair (proposal 04 §A): the port is shared by both
// modes (dial port for caller, bind port for listener) and the IP is the
// dial-out host, dimmed and skipped in listener mode.
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
	// FieldChecklist is a multi-select option grid (the §H stress form's
	// tx multi-select, SCR-508): j/k and arrows move the cursor, space
	// toggles the option under it. Checked[i] mirrors Options[i];
	// Value holds the comma-join of the checked options (the §N2
	// multi-select pattern the REPL's survey.MultiSelect prompt used).
	FieldChecklist
	// FieldPicker is a collapsed option row (proposal 04 §A.3): the row
	// renders the selected value plus a ▸ affordance; Enter/space/right
	// open a nested overlay list (j/k move, type filters, Enter picks,
	// Esc closes keeping the old value). Value/Selected mirror each other
	// like the radio kind; the overlay state is page-owned.
	FieldPicker
)

// FormField is one §E row. Radio fields keep Value mirrored to
// Options[Selected] so root reads one canonical string per field.
// Enabled/Note/NoteKind are root-stamped DATA (enable rules and the TLS
// loaded ✓/✗ note); the dialog renders enabled vs dim and never derives
// them itself.
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
// dialog (SetState preserves the page-owned focus, like SendState.HexOn).
// InFlight swaps the form for the root-stamped progress line; Error holds
// the final-failure line (rendered with the theme error kind).
type ConnectFormState struct {
	Fields   []FormField
	InFlight bool
	Progress string // "attempt 2/3" (root-stamped)
	Backoff  string // "backoff 1.5s" (root-stamped, shown while waiting)
	Error    string
	// Title overrides the box title ("" = "CONNECT"). SCR-507 reuses
	// this dialog machinery for the §G server start form ("SERVER").
	Title string
	// EnterLabel names the Enter action ("" = "connect").
	EnterLabel string
}

// Option lists (§E data). The header list mirrors the REAL selectable
// length types of internal/utils/length.go SelectLength (ascii4, binary2,
// bcd2, binary4, NAPS, visa — NAPS shares the binary2 codec and is offered
// separately, as the REPL connect prompt does), displayed in the §E
// wireframe order with binary2 first.
var (
	ConnectModeOptions   = []string{"Caller", "Listener"}
	ConnectHeaderOptions = []string{"binary2", "ascii4", "bcd2", "binary4", "NAPS", "visa"}
	ConnectYesNoOptions  = []string{"Yes", "No"}
)

// ConnectHeaderHints annotates each header option in the §E header picker
// (proposal 04 §A.3), index-aligned with ConnectHeaderOptions.
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

// ConnectHeaderIsVisa reports whether a header option is the visa header —
// identified exactly the way internal/utils/length.go SelectLength and
// App.ConnectWithOptions special-case it (case-insensitive "visa", the one
// length type that carries a station ID).
func ConnectHeaderIsVisa(option string) bool {
	return strings.EqualFold(strings.TrimSpace(option), "visa")
}

// Modal is the overlay contract the connect dialog implements: the root
// renders frame + overlay (the dialog's View composed over the content
// area, the palette pattern) and the page stack stays untouched.
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
