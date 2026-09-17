// inspector_state.go holds the §C state contract and
// the page→router messages.
// Root builds InspectorState from internal/app (compose-without-send path,
// the same InfoView builders `jiso inspect` uses) and pushes it via
// SetState; the page never touches the app or the clock (pattern).
package pages

// InspectorPageID is the router id of the message inspector (§C).
// It is a drill-down entered from the §B transactions page (Enter on
// a row), not a 1..8 hotkey slot.
const InspectorPageID = "inspector"

// View tab indices in cycle order (the §C [fields] [bitmap] [packed]
// [raw json] tabs; Tab/shift+Tab cycle the page-local index).
const (
	viewFields = iota
	viewBitmap
	viewPacked
	viewRawJSON

	// ViewTabCount is the number of view tabs.
	ViewTabCount = 4
)

// ViewsState carries root-preformatted tab labels (e.g. with counts).
// Empty labels fall back to the built-in ViewTabLabels names; the cycle
// order stays fixed, so an unknown tab is impossible.
type ViewsState struct {
	Fields  string
	Bitmap  string
	Packed  string
	RawJSON string
}

// ViewTabLabels are the built-in tab labels in cycle order.
var ViewTabLabels = [ViewTabCount]string{"fields", "bitmap", "packed", "raw json"}

// label returns the label for tab i (built-in name when unset).
func (v ViewsState) label(i int) string {
	s := ""
	switch i {
	case viewFields:
		s = v.Fields
	case viewBitmap:
		s = v.Bitmap
	case viewPacked:
		s = v.Packed
	case viewRawJSON:
		s = v.RawJSON
	}
	if s == "" {
		return ViewTabLabels[i]
	}

	return s
}

// ValidationRow is one validation-line entry: text plus truth. The page
// renders it as symbol + word (never colour alone).
type ValidationRow struct {
	Text string
	OK   bool
}

// FieldRow is one row of the §C fields tree, every display string
// pre-formatted by root:
//   - Auto rows render "raw → value" (the runtime-generated preview);
//     AutoPreviews is root's small pool of preview variants that `r`
//     cycles locally (page advances an index, never recomputes).
//   - Masked rows carry an already-masked Display (root never ships a raw
//     PAN to the page); the page only substitutes the mask glyph per
//     theme (• / *).
//   - Children renders as an indented tree (composite fields expand on
//     right/enter, collapse on left).
//   - Error red-lines the row with the error symbol.
type FieldRow struct {
	Num          string
	Name         string
	Display      string
	Auto         bool
	AutoPreviews []string
	RawPreview   string
	Masked       bool
	Children     []FieldRow
	Error        string
}

// InspectorState is the immutable snapshot root pushes into the inspector.
// MsgIndex/MsgTotal are the §C "Messages(i/N)" position; 0 total renders as
// the dash (unknown ≠ zero). PackedHex holds pre-wrapped hex lines (plain
// lowercase hex digits, 16 bytes per line at the root's default); the page
// regroups them for the [h] 8/16 toggle — pure display. RawJSON is the
// pre-serialized inspect view JSON shown verbatim by the raw json tab.
type InspectorState struct {
	TxID     string
	TxName   string
	MsgIndex int
	MsgTotal int
	Views    ViewsState
	Fields   []FieldRow
	// DescribeText is the utils.Describe output of the composed message
	// (the fields tab renders the Describe view); empty falls back
	// to the interpolated tree.
	DescribeText []string
	// PackedDump holds the standard hexdump lines (offset, 16 byte
	// pairs, ASCII gutter) of the packed message for the hex pane.
	PackedDump []string
	PackedHex  []string
	HeaderNote string
	Validation []ValidationRow
	RawJSON    string
}

// TxComposeMsg asks the router to run the compose-with-dataset path for
// the inspected transaction (§C bottom-right "Enter compose-with-dataset").
// Root owns it (lands the real composition); today it is a logged
// no-op, like the earlier row-message stubs.
type TxComposeMsg struct {
	ID string
}

// InspectorPopMsg asks the router to pop the inspector (Esc). Root owns
// the stack; the page never pops itself.
type InspectorPopMsg struct{}
