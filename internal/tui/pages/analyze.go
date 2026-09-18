// analyze.go is the §J analyze wizard page: a 4-step wizard (capture ▸ spec
// ▸ header ▸ run) rendered from the root-owned AnalyzeState snapshot; the
// state machine, gates, and async legs live root-side (root_analyze*.go).
// The page owns only presentation state and claims the keyboard only while
// editing or overlay-open; Ctrl+C stays global.
package pages

import (
	key "charm.land/bubbles/v2/key"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// Analyze is the §J page.
type Analyze struct {
	th    *theme.Theme
	nav   analyzeNav
	state AnalyzeState

	width, height int
	step          int // last step seen (filter/cursor reset on change)

	draft     string // the step's filter / typed path (page-owned, like the send wizard's)
	sel       int    // cursor over the current step's list
	filtering bool   // run step: the "/" flow filter is open (claims the keyboard)

	// Output-path editing: [o] opens a one-line editor (Enter commits,
	// Esc cancels). outTyped is the [f] gate: unedited, [f] browses the
	// location through the shared picker; after the first edit [f] is a
	// path byte again.
	outEditing bool
	outDraft   string
	outTyped   bool

	// Generated-item picker: opens when a run attaches (ItemsID change),
	// reopens with [x]; space/a toggle the LOCAL inclusion set, Enter
	// and Esc apply it to root.
	itemsOpen    bool
	itemsShownID int
	itemCursor   int
	itemOff      int // roster window offset
	itemSel      []bool

	// Preview sub-pane: a WINDOW over the item's file form, not a clip.
	// [tab]/[shift+tab] move focus between roster and preview; while the
	// preview is focused the scroll keys drive previewOff. The wheel
	// calls ScrollPreview directly.
	previewFocused bool
	previewOff     int // preview window offset (rows from the top)

	// Unparsable viewer: a read-only hexdump browser opened on demand
	// with [u] (never auto-opens); an UnparsableID change re-seats the
	// cursor over the fresh samples.
	unparsableOpen    bool
	unparsableShownID int
	unparsableCursor  int
	unparsableOff     int

	// itemsRect and previewRect are the picker panes' DRAWN rects from
	// the last overlay render (zero = not on screen) — the geometry the
	// wheel hit map registers.
	itemsRect   geom.Rect
	previewRect geom.Rect

	// selRows are the DRAWN roster row rects from the last overlay
	// render for click-select: a click moves the roster cursor, the
	// wheel over a row still scrolls it.
	selRows []SelectRegion
}

// §J owns wheel-scrollable roster/preview regions and click-selectable
// roster rows.
var (
	_ Scroller = (*Analyze)(nil)
	_ Selector = (*Analyze)(nil)
)

// ScrollRegions publishes the picker panes' drawn rects: the regions
// exist only while the overlay is on screen, so the wizard steps and the
// unparsable viewer publish nothing and the wheel over them stays inert.
func (a *Analyze) ScrollRegions() []ScrollRegion {
	out := make([]ScrollRegion, 0, 2)
	if a.itemsRect.W > 0 && a.itemsRect.H > 0 {
		out = append(out, ScrollRegion{ID: RegionAnalyzeItems, Rect: a.itemsRect})
	}
	if a.previewRect.W > 0 && a.previewRect.H > 0 {
		out = append(out, ScrollRegion{ID: RegionAnalyzePreview, Rect: a.previewRect})
	}
	if len(out) == 0 {
		return nil
	}

	return out
}

// ScrollRegion routes the wheel's delta (d>0 = content down): the preview
// sub-pane drives ScrollPreview, the roster walks the item cursor with the
// window dragged along; a new item previews from the top.
func (a *Analyze) ScrollRegion(id string, d int) bool {
	if !a.itemsOpen || len(a.state.Items) == 0 {
		return false
	}
	switch id {
	case RegionAnalyzePreview:
		if a.previewRect.W <= 0 {
			return false
		}
		a.ScrollPreview(d)

		return true
	case RegionAnalyzeItems:
		if a.itemsRect.W <= 0 {
			return false
		}
		a.itemCursor = min(max(a.itemCursor+d, 0), max(len(a.state.Items)-1, 0))
		a.previewOff = 0 // a new item previews from the top (the keys' rule)
		a.scrollItemsIntoView(a.itemsWindow())

		return true
	}

	return false
}

// SelectRegions publishes the roster's drawn row rects: a click on a
// visible item moves the roster cursor there (space/a still toggle).
func (a *Analyze) SelectRegions() []SelectRegion { return a.selRows }

// SelectRegion moves the roster cursor to the clicked item — the same
// move the wheel's cursor walk makes, so the preview re-seats from the top.
func (a *Analyze) SelectRegion(id string, index int) bool {
	if id != RegionAnalyzeItems || !a.itemsOpen || index < 0 || index >= len(a.state.Items) {
		return false
	}
	a.itemCursor = index
	a.previewOff = 0 // a new item previews from the top (the keys' rule)
	a.scrollItemsIntoView(a.itemsWindow())

	return true
}

// ItemsCursor reports the generated-item roster cursor index (0 when empty).
func (a *Analyze) ItemsCursor() int { return a.itemCursor }

// analyzeNav is the page keymap; Enter/Esc semantics are wizard
// transitions root validates, so they leave as messages.
type analyzeNav struct {
	Cancel    key.Binding
	Enter     key.Binding
	Backspace key.Binding
	PgUp      key.Binding
	PgDn      key.Binding
	Tab       key.Binding
	TabBack   key.Binding
	Down      key.Binding
	Up        key.Binding
	Space     key.Binding
	Browse    key.Binding
	Write     key.Binding
	Filter    key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newAnalyzeNav() analyzeNav {
	nav := analyzeNav{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Enter:     key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		PgUp:      key.NewBinding(key.WithKeys("pgup")),
		PgDn:      key.NewBinding(key.WithKeys("pgdown")),
		Tab:       key.NewBinding(key.WithKeys(theme.KeyTab)),
		TabBack:   key.NewBinding(key.WithKeys("shift+tab")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		Space:     key.NewBinding(key.WithKeys("space")),
		Browse:    key.NewBinding(key.WithKeys("f")),
		Write:     key.NewBinding(key.WithKeys("w")),
		Filter:    key.NewBinding(key.WithKeys("/")),
	}
	nav.help = []HelpEntry{
		navEntry("move", nav.Up, nav.Down),
		navEntry("step", nav.PgUp, nav.PgDn, nav.Tab, nav.TabBack),
		actEntry("next", nav.Enter),
		actEntry("select", nav.Space),
		actEntry("browse capture/spec", nav.Browse),
		actEntry("flow filter", nav.Filter),
		actEntry("goal t/r/s"),
		actEntry("write report", nav.Write),
		actEntry("back / abort", nav.Cancel),
	}

	return nav
}

// NewAnalyze builds the page. A nil theme selects theme.Default
// (production); golden tests inject an explicit NewWith profile.
func NewAnalyze(th *theme.Theme) *Analyze {
	if th == nil {
		th = theme.Default()
	}

	return &Analyze{th: th, nav: newAnalyzeNav(), step: -1}
}

// ID reports the router id (the wire-compat slot name "analyze", hotkey 7).
func (a *Analyze) ID() string { return AnalyzePageID }

// Theme exposes the resolved theme (view helpers and tests).
func (a *Analyze) Theme() *theme.Theme { return a.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (a *Analyze) Size() (width, height int) { return a.width, a.height }

// Step reports the rendered step (tests).
func (a *Analyze) Step() int { return a.state.Step }

// Draft exposes the live filter/typed path and whether the step edits
// text (root tests).
func (a *Analyze) Draft() (string, bool) { return a.draft, a.editingStep() }

// ListCursor exposes the local list cursor (tests).
func (a *Analyze) ListCursor() int { return a.sel }

// ScrollPreview moves the preview window by d rows (+down, -up through the
// content), clamped so calling it on a preview that fits is a no-op. The
// focused-key route and the mouse wheel both call it.
func (a *Analyze) ScrollPreview(d int) {
	maxOff := max(a.previewContentHeight()-a.previewWindow(), 0)
	a.previewOff = min(max(a.previewOff+d, 0), maxOff)
}

// Editing is the two-mode gate the claim and every [f] browse read
// exactly: capture/spec edit while a draft is in progress, run while the
// "/" filter or the [o] editor is open. Navigate sends f to the root-side
// picker; edit types f literally. editingStep is the weaker caret question.
func (a *Analyze) Editing() bool {
	switch a.state.Step {
	case StepCapture, StepSpec:
		return a.draft != ""
	case StepRun:
		return a.filtering || a.outEditing
	}

	return false
}

// ClaimsKeyboard claims the keyboard only while editing or an overlay is
// open, so global keys stay live in navigate mode: capture/spec claim while
// a typed path is in progress (the first byte reaches them through the
// global layer), run while "/" or [o] is open, overlays wholesale. Ctrl+C global.
func (a *Analyze) ClaimsKeyboard() bool {
	if a.itemsOpen || a.unparsableOpen {
		return true
	}

	return a.Editing()
}

// editingStep reports whether the current step is a text-editing surface
// for the view caret and Draft; ClaimsKeyboard is the claim proper.
func (a *Analyze) editingStep() bool {
	switch a.state.Step {
	case StepCapture, StepSpec:
		return true
	case StepRun:
		return a.filtering
	}

	return false
}

// SetState replaces the rendered snapshot. Page-local state survives: the
// draft/cursor drop on a step change, the cursor re-homes on step entry,
// and the run step seeds its flow filter draft from the root-committed
// filter so a revisit shows what will run.
func (a *Analyze) SetState(state AnalyzeState) {
	if state.Step != a.step {
		a.draft = ""
		a.filtering = false
		a.step = state.Step
		if state.Step == StepRun {
			a.draft = state.FlowFilter
		}
		// The cursor homes on the current/selected row once per step ENTRY —
		// re-seeding on every push would snap the arrow cursor back onto the
		// current row and make the arrows look dead.
		switch state.Step {
		case StepCapture:
			a.sel = wizardItemsIndex(state.CaptureItems, func(it WizardItem) bool { return it.Current })
		case StepSpec:
			a.sel = wizardItemsIndex(state.SpecItems, func(it WizardItem) bool { return it.Current })
		case StepHeader:
			a.sel = headerIndex(state.Headers, func(h AnalyzeHeaderItem) bool { return h.Selected })
		}
	}
	a.state = state
	a.clampSel()
	if state.ItemsID != a.itemsShownID {
		a.itemsShownID = state.ItemsID
		a.itemCursor = 0
		a.itemSel = nil
		a.previewFocused, a.previewOff = false, 0 // a fresh roster previews from the top
		for _, it := range state.Items {
			a.itemSel = append(a.itemSel, it.Included)
		}
		a.itemsOpen = len(state.Items) > 0 // a fresh run re-presents the picker
	}
	if state.UnparsableID != a.unparsableShownID {
		// A fresh enumeration re-seats the cursor over the new samples but
		// does not auto-open (the operator opens it with [u]); if it was
		// open it stays open over the new roster.
		a.unparsableShownID = state.UnparsableID
		a.unparsableCursor = 0
		a.unparsableOff = 0
		if len(state.UnparsableRows) == 0 {
			a.unparsableOpen = false
		}
	}
}

// Hints is the §J context keymap; wizard transitions stay primary in the
// narrow footer. While an inline overlay is open it lists its own keys
// in-body (badged), so Hints drops every entry that would repeat one.
func (a *Analyze) Hints() []frame.KeyHint {
	switch a.state.Step {
	case StepCapture:
		return []frame.KeyHint{
			{Key: theme.KeyEnter, Desc: "next", Primary: true},
			{Key: "f", Desc: "browse"},
			{Key: theme.KeyEsc, Desc: "cancel", Primary: true},
		}
	case StepSpec:
		return []frame.KeyHint{
			{Key: theme.KeyEnter, Desc: "next", Primary: true},
			{Key: theme.KeyEsc, Desc: "back", Primary: true},
		}
	case StepHeader:
		return []frame.KeyHint{
			{Key: theme.KeyEnter, Desc: "next", Primary: true},
			{Key: theme.KeyEsc, Desc: "back", Primary: true},
		}
	}
	run := []frame.KeyHint{
		{Key: theme.KeyEnter, Desc: "run", Primary: true},
		{Key: theme.KeyNavJK, Desc: "flow"},
		{Key: "space", Desc: "include"},
		{Key: "/", Desc: "flow filter"},
		{Key: "o", Desc: "output file"},
		{Key: "x", Desc: "items"},
		{Key: "w", Desc: "write"},
		{Key: theme.KeyEsc, Desc: "back", Primary: true},
	}
	if a.itemsOpen { // the picker lists space/a/enter/esc in-body and
		// drives its roster cursor on j/k, so "flow" would mislabel them
		return hintsMinus(run, theme.KeyEnter, "space", theme.KeyEsc, theme.KeyNavJK)
	}
	if a.unparsableOpen { // the viewer lists j/k/esc in-body
		return hintsMinus(run, theme.KeyNavJK, theme.KeyEsc)
	}

	return run
}

// pick returns the ascii form under theme.ASCII, the truecolor form otherwise.
func (a *Analyze) pick(truecolor, ascii string) string {
	if a.th.ASCII {
		return ascii
	}

	return truecolor
}
