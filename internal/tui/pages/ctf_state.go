// ctf_state.go holds the §K state contract (wireframe §K) and the
// page→router messages. Root owns the truth: it queries the app CTF
// façade off the UI thread (tea.Cmd), derives every display string with
// the injectable clock, and pushes CtfState via SetState — the page
// never imports internal/app, never opens the database, and never reads
// the clock (the SCR-501 data-flow contract). The page owns only
// presentation state: list cursor + filter, the parameters form drafts
// with page-local field focus, and the preview overlay's Esc ownership.
package pages

// CtfPageID is the router id of the §K CTF export page:
// hotkey 8, footer label "ctf".
const CtfPageID = "ctf"

// Pane focus slots: the router's Tab/shift-Tab PaneFocusMsg cycles
// SESSIONS ↔ PARAMETERS; while PARAMETERS holds focus the page claims
// the keyboard and Tab moves the field focus ring locally.
const (
	CtfPaneSessions = iota
	CtfPaneParams
	ctfPaneCount
)

// Form field indices (wireframe §K PARAMETERS order).
const (
	FieldCIB = iota
	FieldBin
	FieldBatch
	FieldOut
	FormFieldCount = 4
)

// FieldLabels are the PARAMETERS row labels in field order.
var FieldLabels = [FormFieldCount]string{
	"CIB (interchange BIN)",
	"Filter card BIN",
	"Batch number",
	"Output path",
}

// CtfSessionRow is one SESSIONS list row: ShortID is the shortened
// session id (root ASCII-fies), When the root-derived relative stamp,
// Approved the root-derived "148 approved" cell.
type CtfSessionRow struct {
	ID       string
	ShortID  string
	When     string
	Approved string
}

// CtfParams carries the four PARAMETERS form values as text (root
// parses batch; blank CIB/batch get the PAR-308 defaults, blank BIN
// means "all").
type CtfParams struct {
	CIB     string
	Bin     string
	Batch   string
	OutPath string
}

// Field returns the form value at field i.
func (p CtfParams) Field(i int) string {
	switch i {
	case FieldCIB:
		return p.CIB
	case FieldBin:
		return p.Bin
	case FieldBatch:
		return p.Batch
	default:
		return p.OutPath
	}
}

// CtfPreview is the record-viewer overlay content (UAT round 6
// wireframe): headline lines (record count + totals, root-derived),
// EVERY record string the write would emit, the resolved output path,
// and the overwrite flag root stamped from its os.Stat leg (§N3 confirm
// fronts the write).
type CtfPreview struct {
	Headline  []string
	Records   []string
	OutPath   string
	Overwrite bool
}

// CtfState is the immutable §K snapshot root pushes into the page.
// Params carries the committed/prefill form values (the page overlays
// its uncommitted drafts); SummaryLine is the wireframe SUMMARY line
// ("" renders the dashed placeholder); Preview is the last preview
// result (nil = none yet); PreviewID re-arms the overlay on change;
// WriteLine is the toast-style write result.
type CtfState struct {
	DBPath      string
	Note        string
	Sessions    []CtfSessionRow
	SelectedID  string
	Params      CtfParams
	SummaryLine string
	// SummaryWait marks the dry leg in flight: the SUMMARY line reads
	// "… computing" until the result folds (UAT round 6 wireframe).
	SummaryWait bool
	Preview     *CtfPreview
	PreviewID   int
	WriteLine   string
	WriteOK     bool
}

// CtfSelectMsg is the list cursor moving onto a different session, or a
// form edit: root re-runs the DRY preview leg for the row under the
// cursor and folds it into the SUMMARY line WITHOUT opening the overlay
// (UAT round 6: the summary must follow the cursor, not the last Enter).
type CtfSelectMsg struct {
	SessionID string
	Params    CtfParams
}

// CtfGenerateMsg is Enter: generate the preview for the session under
// the list cursor with the form values (root runs the dry leg off the
// UI thread; the overlay opens when the result arrives).
type CtfGenerateMsg struct {
	SessionID string
	Params    CtfParams
}

// CtfWriteMsg is w inside the preview overlay: write exactly what the
// preview promised (root checks §N3 overwrite confirm first).
type CtfWriteMsg struct{}

// CtfRefreshMsg asks root to re-query the eligible sessions (r).
type CtfRefreshMsg struct{}

// CtfPopMsg asks the router to pop the §K page (Esc in the list pane;
// the overlay and the form own Esc earlier, inside the page).
type CtfPopMsg struct{}
