// analyze.go is the §J analyze wizard page (SCR-510): a 4-step wizard
// (1 capture ▸ 2 spec ▸ 3 header ▸ 4 run) rendered from the root-owned
// AnalyzeState snapshot and styled after the send wizard — a step rail,
// ▸-cursor candidate lists with a filter line that doubles as a typed
// path input, and a hints footer. The wizard state machine, the gates,
// and every async leg live root-side (root_analyze*.go). The page is a
// reference type kept canonical in the router registry, owns only
// presentation state (the step filter/cursor), and yields key-driven
// messages; it never touches internal/app, never reads the clock, and
// ignores bus events. The capture/spec list steps claim the keyboard
// like the send wizard's list steps do, so paths containing q, digits,
// or : stay typeable (Ctrl+C stays global); the run step claims it only
// while the "/" flow filter is open.
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

	// Output-path editing (UAT round 5): [o] on the run step opens a
	// one-line editor; Enter commits the typed path, Esc cancels.
	// outTyped (UAT round 8 finding 6) is the two-mode gate of the
	// capture step applied here: while nothing has been edited yet, [f]
	// browses the output location through the shared picker; after the
	// first edit [f] is a path byte again.
	outEditing bool
	outDraft   string
	outTyped   bool

	// Generated-item picker (UAT round 6): opens automatically when a
	// run attaches (ItemsID change) and reopens with [x]; space/a toggle
	// the LOCAL inclusion set, Enter applies it to root, Esc closes and
	// discards unapplied toggles.
	itemsOpen    bool
	itemsShownID int
	itemCursor   int
	itemOff      int // roster window offset
	itemSel      []bool

	// Generated-item preview sub-pane (UAT round 8 finding 8: "it
	// should be possible to scroll content if it does not fit into
	// screen"): the picker's preview is a window over the item's file
	// form, not a clip. [tab]/[shift+tab] move the picker focus between
	// the roster and the preview; while the preview is focused the
	// scroll keys drive previewOff instead of the roster cursor. The
	// mouse wheel (Phase 8) calls ScrollPreview directly.
	previewFocused bool
	previewOff     int // preview window offset (rows from the top)

	// Unparsable-message viewer (UAT round 6): opened on demand with
	// [u] on the run step (never auto-opens); a read-only hexdump
	// browser over the failure samples. UnparsableID change re-seats
	// the cursor over a fresh capture.
	unparsableOpen    bool
	unparsableShownID int
	unparsableCursor  int
	unparsableOff     int

	// itemsRect and previewRect are the DRAWN picker panes (content-
	// relative) recorded during the last render of the generated-item
	// overlay; the zero value means the pane was not on screen. They are
	// the geometry the wheel hit map registers under RegionAnalyzeItems /
	// RegionAnalyzePreview (Task 8.2c).
	itemsRect   geom.Rect
	previewRect geom.Rect
}

// §J owns two wheel-scrollable regions (the generated-item roster and
// its preview sub-pane) and implements the Task 8.2c region seam.
var _ Scroller = (*Analyze)(nil)

// ScrollRegions publishes the picker panes' drawn rects: the regions
// exist exactly while the generated-item overlay is on screen, so the
// wizard steps and the unparsable reviewer publish nothing and the wheel
// over them stays inert.
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

// ScrollRegion routes the wheel's content-direction delta (d>0 = down):
// the preview sub-pane drives ScrollPreview (the one scroll contract
// Task 7.3 pinned), and the roster walks the item cursor with the window
// dragged along — the same offsets the [tab]-focused keys drive, clamped
// the same way; a new item previews from the top like the keys do.
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
		actEntry("browse capture", nav.Browse),
		actEntry("flow filter", nav.Filter),
		actEntry("goal t/r/s"),
		actEntry("write report", nav.Write),
		actEntry("back / abort", nav.Cancel),
	}

	return nav
}

// NewAnalyze builds the page. A nil theme selects theme.Default()
// (production); golden tests inject an explicit NewWith profile.
func NewAnalyze(th *theme.Theme) *Analyze {
	if th == nil {
		th = theme.Default()
	}

	return &Analyze{th: th, nav: newAnalyzeNav(), step: -1}
}

// ID reports the router id (AnalyzePageID — the wire-compat slot name
// "analyze", hotkey 7; the slot's frame tab title is §J's
// "PCAP ANALYZE").
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

// ScrollPreview moves the generated-item preview window by d rows: a
// positive d moves the window down through the content (revealing later
// lines), a negative d moves it up. The offset is clamped to
// 0..max(0, contentH-paneH), so calling it on a preview that fits is a
// no-op. This is the one scroll contract: the keyboard route (preview
// sub-pane focused) and the Phase 8 mouse wheel both call it.
func (a *Analyze) ScrollPreview(d int) {
	maxOff := max(a.previewContentHeight()-a.previewWindow(), 0)
	a.previewOff = min(max(a.previewOff+d, 0), maxOff)
}

// Editing reports the two-mode flag of the page (UAT round 8 / D3,
// Task 5.2): the capture/spec steps are in EDIT mode while a typed
// path/filter draft is in progress, the run step while the "/" flow
// filter or the [o] output-path editor is open. This is the predicate
// ClaimsKeyboard delegates to and the [f] browse gate reads: navigate
// mode sends f to the root-side picker, edit mode types f literally into
// the draft (the §G server-form pattern). It is the mode proper, not the
// weaker step-level caret question of editingStep.
func (a *Analyze) Editing() bool {
	switch a.state.Step {
	case StepCapture, StepSpec:
		return a.draft != ""
	case StepRun:
		return a.filtering || a.outEditing
	}

	return false
}

// ClaimsKeyboard implements KeyboardClaimer with the UAT round 8 (D2)
// scoping: the claim is EDIT mode (Editing), not "a step that has an
// editable field". The capture/spec steps claim only while a typed path
// is in progress (the first byte reaches them through the global layer,
// the SCR-502 typeahead); the run step claims while the "/" flow filter
// or the [o] output-path editor is open. The generated-item picker and
// the unparsable-message viewer are overlays: they own the keyboard
// wholesale while open. Ctrl+C stays global.
func (a *Analyze) ClaimsKeyboard() bool {
	if a.itemsOpen || a.unparsableOpen {
		return true
	}

	return a.Editing()
}

// editingStep reports whether the current step is a text-editing
// surface for the view caret and Draft (the step-level question); the
// keyboard claim proper is ClaimsKeyboard (UAT round 8: claim ==
// actively typing).
func (a *Analyze) editingStep() bool {
	switch a.state.Step {
	case StepCapture, StepSpec:
		return true
	case StepRun:
		return a.filtering
	}

	return false
}

// SetState replaces the rendered snapshot (root pushes it after every
// wizard mutation and async result). Page-local state survives: the
// draft/cursor are dropped when the step changes, the cursor re-places
// on the selected item, and the run step seeds its flow filter draft
// from the root-committed filter so a revisit shows what will run.
func (a *Analyze) SetState(state AnalyzeState) {
	if state.Step != a.step {
		a.draft = ""
		a.filtering = false
		a.step = state.Step
		if state.Step == StepRun {
			a.draft = state.FlowFilter
		}
		// The cursor homes on the current/selected row once per step
		// ENTRY — and only then (UAT round 6: root re-pushes the snapshot
		// after every Update, so re-seeding on every sync snapped the
		// arrow cursor back onto the current row and the arrows looked
		// dead). The send wizard's SetState has always been seed-on-entry.
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
		// A fresh enumeration re-seats the viewer cursor over the new
		// samples but does NOT auto-open it (the operator opens it with
		// [u]); if it was open it stays open over the new roster.
		a.unparsableShownID = state.UnparsableID
		a.unparsableCursor = 0
		a.unparsableOff = 0
		if len(state.UnparsableRows) == 0 {
			a.unparsableOpen = false
		}
	}
}

// Hints is the §J context keymap; the wizard transitions are primary
// so the narrow footer keeps them (the router appends the global
// bindings).
func (a *Analyze) Hints() []frame.KeyHint {
	if a.unparsableOpen { // the hexdump viewer's keys replace the step hints
		return []frame.KeyHint{
			{Key: "j/k", Desc: "sample"},
			{Key: theme.KeyEsc, Desc: "close", Primary: true},
		}
	}
	if a.itemsOpen { // the picker overlay's keys replace the step hints
		return []frame.KeyHint{
			{Key: "space", Desc: "include", Primary: true},
			{Key: "a", Desc: "all/none"},
			{Key: theme.KeyTab, Desc: hintPreview},
			{Key: theme.KeyEnter, Desc: "apply", Primary: true},
			{Key: theme.KeyEsc, Desc: "close", Primary: true},
		}
	}
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
	default:
		return []frame.KeyHint{
			{Key: theme.KeyEnter, Desc: "run", Primary: true},
			{Key: theme.KeyNavJK, Desc: "flow"},
			{Key: "space", Desc: "include"},
			{Key: "/", Desc: "flow filter"},
			{Key: "o", Desc: "output file"},
			{Key: "x", Desc: "items"},
			{Key: "w", Desc: "write"},
			{Key: theme.KeyEsc, Desc: "back", Primary: true},
		}
	}
}

// pick returns the ascii form under theme.ASCII, the truecolor form
// otherwise (the send wizard's rule).
func (a *Analyze) pick(truecolor, ascii string) string {
	if a.th.ASCII {
		return ascii
	}

	return truecolor
}
