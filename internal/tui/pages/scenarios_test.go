package pages

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/frame"
)

// scenPress builds a named-key press (esc/enter/backspace/arrows).
func scenPress(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// scenPage builds an ascii §F page at the given size with the state.
func scenPage(t *testing.T, state ScenariosState, w, h int) *Scenarios {
	t.Helper()

	s := NewScenarios(asciiTheme(t))
	s.SetState(state)
	_, _ = s.Update(windowSize(w, h))

	return s
}

// scenListState is the three-scenario §F sample.
func scenListState() ScenariosState {
	return ScenariosState{
		Scenarios: []ScenarioRow{
			{ID: "E2E Purchase and Reversal", Name: "E2E Purchase and Reversal"},
			{ID: "Card update flow", Name: "Card update flow"},
			{ID: "Decline matrix", Name: "Decline matrix"},
		},
		ReportPath: "scenario-report.json",
	}
}

func TestScenariosFilterNarrowsAndClamps(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)

	_, _ = s.Update(press('/'))
	if !s.ClaimsKeyboard() {
		t.Fatal("/ must arm the live filter")
	}
	for _, r := range "decline" {
		_, _ = s.Update(press(r))
	}
	if len(s.view) != 1 || s.SelectedID() != "Decline matrix" {
		t.Fatalf("after filter: view=%v selected=%q", s.view, s.SelectedID())
	}

	_, _ = s.Update(press('z'))
	if len(s.view) != 0 || s.SelectedID() != "" {
		t.Fatalf("empty filter result: view=%v selected=%q", s.view, s.SelectedID())
	}
	body := s.View().Content
	if !strings.Contains(body, "no scenarios match filter") && !strings.Contains(body, "no items") {
		t.Errorf("empty filter result must render the list empty state:\n%s", body)
	}

	_, _ = s.Update(scenPress(tea.KeyEscape))
	f, filtering := s.Filter()
	if s.ClaimsKeyboard() || filtering || f != "" {
		t.Fatalf("esc must leave filter mode with the text cleared: %v %q", s.ClaimsKeyboard(), f)
	}
	if len(s.view) != 3 {
		t.Fatalf("after esc view = %d rows, want 3", len(s.view))
	}
}

func TestScenariosFilterPreservesSelectionByID(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)
	_, _ = s.Update(scenPress(tea.KeyDown)) // cursor on "Card update flow"

	if s.SelectedID() != "Card update flow" {
		t.Fatalf("cursor move: selected=%q", s.SelectedID())
	}

	_, _ = s.Update(press('/'))
	for _, r := range "flow" {
		_, _ = s.Update(press(r))
	}
	if s.SelectedID() != "Card update flow" {
		t.Fatalf("selection must survive re-filter by ID: %q", s.SelectedID())
	}
}

func TestScenariosSelectionClampsWhenRowFilteredOut(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)
	_, _ = s.Update(scenPress(tea.KeyEnd)) // cursor on "Decline matrix"

	_, _ = s.Update(press('/'))
	for _, r := range "card" {
		_, _ = s.Update(press(r))
	}
	if s.SelectedID() != "Card update flow" {
		t.Fatalf("clamped selection: %q", s.SelectedID())
	}
}

func TestScenariosStepsPaneFollowsSelection(t *testing.T) {
	t.Parallel()

	st := scenListState()
	st.SelectedSteps = []StepRow{{Index: 1, Name: "Purchase Auth", MTI: "0200", Status: StepPending}}
	s := scenPage(t, st, 120, 32)

	if body := s.View().Content; !strings.Contains(body, "Purchase Auth") {
		t.Errorf("steps pane must render pushed rows:\n%s", body)
	}

	st.SelectedSteps = []StepRow{{Index: 1, Name: "Sign On Step", MTI: "0800", Status: StepPending}}
	s.SetState(st)
	body := s.View().Content
	if !strings.Contains(body, "Sign On Step") || strings.Contains(body, "Purchase Auth") {
		t.Errorf("steps pane must follow the new selection:\n%s", body)
	}
}

func TestScenariosRunningStreamRenders(t *testing.T) {
	t.Parallel()

	st := scenListState()
	st.Running = true
	st.SelectedSteps = []StepRow{
		{Index: 1, Name: "One", Status: StepPass, Latency: time.Millisecond},
		{Index: 2, Name: "Two", Status: StepRunning},
		{Index: 3, Name: "Three", Status: StepPending},
	}
	s := scenPage(t, st, 120, 32)
	body := s.View().Content

	if !strings.Contains(body, "[ok]") { // ascii theme ok symbol
		t.Errorf("pass row must carry the ok symbol:\n%s", body)
	}
	if !strings.Contains(body, ".. running") {
		t.Errorf("running row must render symbol+word:\n%s", body)
	}
	if !strings.Contains(body, "running") || !strings.Contains(body, "Three") {
		t.Errorf("stream rows missing:\n%s", body)
	}
}

func TestScenariosFailedStepDiffLine(t *testing.T) {
	t.Parallel()

	st := scenListState()
	st.SelectedSteps = []StepRow{{
		Index: 2, Name: "Purchase Auth", MTI: "0200",
		Note:   `39 expect "00" got "96"`,
		Status: StepFail, Latency: 2 * time.Millisecond,
	}}
	st.Summary = "2/3 passed · 6ms total"
	s := scenPage(t, st, 120, 32)
	body := s.View().Content

	if !strings.Contains(body, `expect "00" got "96"`) {
		t.Errorf("failed step must show the validation diff:\n%s", body)
	}
	if !strings.Contains(body, "[x]") {
		t.Errorf("failed step must carry the error symbol:\n%s", body)
	}
	if !strings.Contains(body, "2/3 passed - 6ms total") { // ascii theme degrades "·"
		t.Errorf("final banner must render:\n%s", body)
	}
}

// TestScenariosErrorStrip: UAT round 5 — a long engine error must be
// readable in the dedicated strip under the title (word-wrapped, at
// most two lines), not only as the clipped sub-line; and a passing run
// shows no strip at all.
func TestScenariosErrorStrip(t *testing.T) {
	t.Parallel()

	long := "network send failed: failed to build message payload: failed to pack message: " +
		"failed to pack field 22 (Point of Sale (POS) Entry Mode): failed to encode length: " +
		"field length: 3 should be fixed: 4"
	st := scenListState()
	st.SelectedSteps = []StepRow{
		{Index: 1, Name: "Network Sign On", MTI: "0800", Status: StepPass, Latency: time.Millisecond},
		{Index: 2, Name: "Purchase Auth", MTI: "0200", Note: long, Status: StepFail},
	}
	s := scenPage(t, st, 120, 32)
	lines := strings.Split(s.View().Content, "\n")

	if len(lines) < 2 || !strings.HasPrefix(lines[1], "[x] error:") {
		t.Fatalf("line under the title must be the dedicated error strip, got: %q", lines[1])
	}
	stripLines := 0
	for _, l := range lines[1:] {
		if strings.Contains(l, "[x]") && (strings.Contains(l, "error:") || strings.Contains(l, "pack")) {
			stripLines++
		}
		if stripLines == 2 {
			break
		}
	}
	joined := strings.Join(lines[1:3], " ")
	if !strings.Contains(joined, "field length: 3 should be fixed: 4") {
		t.Errorf("the wrapped strip must reach the error's tail, got: %q", joined)
	}

	// A run with no failed step shows no strip (geometry unchanged).
	st2 := scenListState()
	st2.SelectedSteps = []StepRow{
		{Index: 1, Name: "Network Sign On", MTI: "0800", Status: StepPass, Note: "extract 37", Latency: time.Millisecond},
	}
	passBody := scenPage(t, st2, 120, 32).View().Content
	if strings.Contains(passBody, "error:") {
		t.Errorf("passing run must not render the error strip:\n%s", passBody)
	}
}

func TestScenariosPassNoteSubLine(t *testing.T) {
	t.Parallel()

	st := scenListState()
	st.SelectedSteps = []StepRow{{
		Index: 2, Name: "Purchase Auth", MTI: "0200", RC: "00",
		Note:   "extract AuthId=482913 · validate 39=00",
		Status: StepPass, Latency: 3 * time.Millisecond,
	}}
	s := scenPage(t, st, 120, 32)
	body := s.View().Content

	if !strings.Contains(body, "extract AuthId=482913") || !strings.Contains(body, "validate 39=00") {
		t.Errorf("pass sub-line missing:\n%s", body)
	}
	if !strings.Contains(body, "RC 00") || !strings.Contains(body, "3ms") {
		t.Errorf("pass row must show RC and time:\n%s", body)
	}
}

func TestScenariosEnterYieldsRunMsg(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)
	_, _ = s.Update(scenPress(tea.KeyDown))

	_, cmd := s.Update(scenPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on a scenario must yield a cmd")
	}
	msg, ok := cmd().(ScenarioRunMsg)
	if !ok || msg.ID != "Card update flow" {
		t.Fatalf("enter msg = %#v, want ScenarioRunMsg{Card update flow}", cmd())
	}
}

func TestScenariosEnterEmptyNoCmd(t *testing.T) {
	t.Parallel()

	s := scenPage(t, ScenariosState{}, 120, 32)
	_, cmd := s.Update(scenPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("enter with no scenario ran %v", cmd())
	}
}

func TestScenariosExportAndPopMsgs(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)

	_, cmd := s.Update(press('e'))
	if _, ok := cmd().(ScenarioExportMsg); cmd == nil || !ok {
		t.Fatalf("e msg = %#v, want ScenarioExportMsg", cmd)
	}

	_, cmd = s.Update(scenPress(tea.KeyEscape))
	if _, ok := cmd().(ScenarioPopMsg); cmd == nil || !ok {
		t.Fatalf("esc msg = %#v, want ScenarioPopMsg", cmd)
	}
}

func TestScenariosFilterModeTypesReservedKeys(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)
	_, _ = s.Update(press('/'))

	for _, r := range "q:1" {
		_, cmd := s.Update(press(r))
		if cmd != nil {
			t.Fatalf("typing %q ran a cmd (keyboard claim leaked)", r)
		}
	}
	if f, _ := s.Filter(); f != "q:1" {
		t.Fatalf("filter text = %q, want q:1", f)
	}
}

func TestScenariosEmptyState(t *testing.T) {
	t.Parallel()

	s := scenPage(t, ScenariosState{}, 120, 32)
	body := s.View().Content

	if !strings.Contains(body, "no scenarios") || !strings.Contains(body, "load via") {
		t.Errorf("empty state must name the palette entry:\n%s", body)
	}
	if !strings.Contains(body, "SCENARIOS (0)") {
		t.Errorf("header must count scenarios:\n%s", body)
	}
}

func TestScenariosStatusLineAndExportBanner(t *testing.T) {
	t.Parallel()

	st := scenListState()
	st.StatusLine = "report → scenario-report.json"
	s := scenPage(t, st, 120, 32)
	if body := s.View().Content; !strings.Contains(body, "report -> scenario-report.json") { // ascii degradation
		t.Errorf("status line missing:\n%s", body)
	}

	st.Summary = "3/3 passed · 7ms total"
	s.SetState(st)
	body := s.View().Content
	if !strings.Contains(body, "3/3 passed - 7ms total") || !strings.Contains(body, "report ->") {
		t.Errorf("banner and status line must share the banner row:\n%s", body)
	}
}

// TestScenariosResponsiveStacking pins the §F breakpoint contract:
// side-by-side at ≥100 cols (STEPS title on the title row's line below),
// steps pane below the list below it (90/70).
func TestScenariosResponsiveStacking(t *testing.T) {
	t.Parallel()

	st := scenListState()
	st.SelectedSteps = []StepRow{{Index: 1, Name: "Sign On Step", MTI: "0800", Status: StepPass}}

	cases := []struct {
		w       int
		stacked bool
	}{
		{120, false},
		{90, true},
		{70, true},
	}

	for _, c := range cases {
		s := scenPage(t, st, c.w, 32)
		lines := strings.Split(strings.TrimRight(s.View().Content, "\n"), "\n")

		stepsLine := -1
		listLine := -1

		for i, l := range lines {
			if strings.Contains(l, "STEPS") && stepsLine < 0 {
				stepsLine = i
			}
			if strings.Contains(l, "Sign On Step") && listLine < 0 {
				listLine = i
			}
		}
		if stepsLine < 0 || listLine < 0 {
			t.Fatalf("width %d: STEPS=%d step=%d:\n%s", c.w, stepsLine, listLine, strings.Join(lines, "\n"))
		}
		if c.stacked && stepsLine >= listLine {
			t.Errorf("width %d: steps pane must sit below the list (STEPS@%d step@%d)", c.w, stepsLine, listLine)
		}
		if !c.stacked && stepsLine > listLine {
			t.Errorf("width %d: panes must be side by side (STEPS@%d step@%d)", c.w, stepsLine, listLine)
		}
	}
}

// TestScenariosPanesAligned pins UAT round 9 finding F-9e: at split
// widths BOTH §F panes are titled Sections sharing ONE title row with
// their top and bottom borders flush, and the two pane widths plus the
// gap sum exactly to the content width (the old border-only list pane
// staggered the boxes by a row and the -2 width maths left a 4-cell
// trailing gap). At stacked widths the panes each take the full content
// width and STEPS starts on its own title row below the list.
func TestScenariosPanesAligned(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		w, h    int
		stacked bool
	}{
		{"w120", 120, 32, false},
		{"w200", 200, 40, false},
		{"w80", 80, 32, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := scenListState()
			st.SelectedSteps = []StepRow{
				{Index: 1, Name: "Sign On Step", MTI: "0800", RC: "00", Status: StepPass, Latency: time.Millisecond},
			}
			s := scenPage(t, st, c.w, c.h)
			body := s.View().Content

			contentW, _ := frame.ContentSize(c.w, c.h)
			lines := strings.Split(strings.TrimRight(body, "\n"), "\n")

			// Both panes record a section Rect: the list pane AND the
			// steps pane (the guard hit-tests both).
			if len(s.sections) != 2 {
				t.Fatalf("recorded %d section rects, want 2 (list + steps):\n%s",
					len(s.sections), body)
			}
			list, steps := s.sections[0], s.sections[1]

			if c.stacked {
				if list.W != contentW || steps.W != contentW {
					t.Errorf("stacked panes want the %d-cell content width: list %v steps %v",
						contentW, list, steps)
				}
				if steps.Y != list.Y+list.H {
					t.Errorf("stacked STEPS must start on its own row below the list: list %v steps %v",
						list, steps)
				}

				return
			}

			if list.Y != steps.Y || list.H != steps.H {
				t.Errorf("panes not on one grid row: list %v steps %v", list, steps)
			}
			if list.W+scenSectionGap+steps.W != contentW {
				t.Errorf("pane widths %d+%d+%d do not sum to the %d-cell content width",
					list.W, scenSectionGap, steps.W, contentW)
			}

			// One shared title row carries both pane titles, and the
			// rows below/above carry BOTH boxes' borders on the SAME
			// rows: top-left, top-right of each box, flush.
			needInk := func(row int, cols []int, why string) {
				t.Helper()
				if row < 0 || row >= len(lines) {
					t.Fatalf("%s: line %d past the %d-line body", why, row, len(lines))
				}

				cs := cells(lines[row])
				for _, col := range cols {
					if col >= len(cs) || cs[col] == ' ' || cs[col] == 0 {
						t.Errorf("%s: cell %d on line %d is blank: %q",
							why, col, row, ansi.Strip(lines[row]))
					}
				}
			}
			needInk(list.Y, []int{list.X, steps.X}, "the shared title row needs both pane titles")
			needInk(list.Y+1, []int{0, list.W - 1, steps.X, contentW - 1},
				"the top border row needs both boxes' edges on one row")
			needInk(list.Y+list.H-1, []int{0, list.W - 1, steps.X, contentW - 1},
				"the bottom border row needs both boxes' edges flush")
		})
	}
}

func TestScenariosHintsRunExportPrimary(t *testing.T) {
	t.Parallel()

	s := NewScenarios(asciiTheme(t))

	var run, export bool

	for _, h := range s.Hints() {
		switch h.Key {
		case "enter":
			run = h.Primary
		case "e":
			export = h.Primary
		}
	}
	if !run || !export {
		t.Fatalf("enter/e must be primary hints: %+v", s.Hints())
	}
}

func TestScenariosHeaderReportPath(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenListState(), 120, 32)
	if body := s.View().Content; !strings.Contains(body, "report: scenario-report.json") {
		t.Errorf("header must show the export destination:\n%s", body)
	}

	s2 := scenPage(t, ScenariosState{Scenarios: []ScenarioRow{{ID: "a", Name: "a"}}}, 120, 32)
	if body := s2.View().Content; strings.Contains(body, "report: scenario") {
		t.Errorf("unset ReportPath must render the dash, not a guess:\n%s", body)
	}
}
