// scenarios_preview_test.go pins the §F step cursor and step-preview
// overlay: j/k step nav, Enter-for-preview, Esc-first close, scroll,
// and honest loading/empty/error/composed states.
package pages

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// scenStepsState is the §F sample with a three-step stream under the
// first scenario (distinct names so cursor moves are greppable).
func scenStepsState() ScenariosState {
	st := scenListState()
	st.SelectedSteps = []StepRow{
		{Index: 1, Name: "Network Sign On", MTI: "0800", RC: "00", Status: StepPass},
		{Index: 2, Name: "Purchase Authorization", MTI: "0200", RC: "00", Status: StepPass},
		{Index: 3, Name: "Reversal", MTI: "0420", Status: StepPending},
	}

	return st
}

// scenStepsFocused returns an ascii §F page with the STEPS pane focused.
func scenStepsFocused(t *testing.T, state ScenariosState, w, h int) *Scenarios {
	t.Helper()

	s := scenPage(t, state, w, h)
	_, _ = s.Update(PaneFocusMsg{}) // list -> steps

	return s
}

// With STEPS focused, j/k move the step cursor, never the list cursor.
func TestScenariosStepCursorMovesInStepsPane(t *testing.T) {
	t.Parallel()

	s := scenStepsFocused(t, scenStepsState(), 120, 32)
	before := s.SelectedID()

	_, cmd := s.Update(scenPress(tea.KeyDown))
	if cmd != nil {
		t.Fatalf("step cursor move ran %v, want nil", cmd())
	}
	if s.StepCursor() != 1 {
		t.Fatalf("after down step cursor = %d, want 1", s.StepCursor())
	}
	body := ansi.Strip(s.View().Content)
	if !strings.Contains(body, ">  2  Purchase Authorization") {
		t.Errorf("the cursor row must carry the selector marker:\n%s", body)
	}
	if strings.Contains(body, ">  1  Network Sign On") {
		t.Errorf("the unselected rows must not carry the marker:\n%s", body)
	}

	_, _ = s.Update(press('j')) // one more step
	if s.StepCursor() != 2 {
		t.Fatalf("after j step cursor = %d, want 2", s.StepCursor())
	}
	_, _ = s.Update(press('j')) // clamped at the last row
	if s.StepCursor() != 2 {
		t.Fatalf("clamped j step cursor = %d, want 2", s.StepCursor())
	}
	_, _ = s.Update(press('k'))
	if s.StepCursor() != 1 {
		t.Fatalf("after k step cursor = %d, want 1", s.StepCursor())
	}
	_, _ = s.Update(scenPress(tea.KeyHome))
	if s.StepCursor() != 0 {
		t.Fatalf("after home step cursor = %d, want 0", s.StepCursor())
	}
	_, _ = s.Update(scenPress(tea.KeyEnd))
	if s.StepCursor() != 2 {
		t.Fatalf("after end step cursor = %d, want 2", s.StepCursor())
	}
	if s.SelectedID() != before {
		t.Fatalf("step nav must not move the list cursor: %q -> %q", before, s.SelectedID())
	}

	// The step cursor re-homes when the selected scenario changes.
	_, _ = s.Update(PaneFocusMsg{Reverse: true}) // back to the list pane
	_, _ = s.Update(scenPress(tea.KeyDown))

	st := scenStepsState()
	st.SelectedSteps = []StepRow{{Index: 1, Name: "Sign On Step", MTI: "0800", Status: StepPending}}
	s.SetState(st)
	if s.SelectedID() == "E2E Purchase and Reversal" {
		t.Fatalf("fixture: the list cursor should have moved, got %q", s.SelectedID())
	}
	if s.StepCursor() != 0 {
		t.Fatalf("the step cursor must re-home when the scenario changes: %d", s.StepCursor())
	}
}

// A stream longer than the pane slides the window to keep the cursor visible.
func TestScenariosStepCursorStaysVisible(t *testing.T) {
	t.Parallel()

	st := scenListState()
	for i := 1; i <= 40; i++ {
		st.SelectedSteps = append(st.SelectedSteps, StepRow{
			Index: i, Name: "Step-" + strconv.Itoa(i), MTI: "0200", Status: StepPending,
		})
	}
	s := scenStepsFocused(t, st, 120, 32)

	_, _ = s.Update(scenPress(tea.KeyEnd)) // jump to the last step
	if s.StepCursor() != 39 {
		t.Fatalf("end must clamp to the last step: %d", s.StepCursor())
	}
	body := ansi.Strip(s.View().Content)
	if !strings.Contains(body, "> 40  Step-40") {
		t.Errorf("the window must slide to keep the cursor row visible:\n%s", body)
	}
	if strings.Contains(body, "  Step-1 ") {
		t.Errorf("the slid window must drop the top rows:\n%s", body)
	}
}

// Enter on a step yields ScenarioStepDetailMsg, never ScenarioRunMsg;
// list-pane Enter still runs the scenario.
func TestScenariosEnterOnStepEmitsDetailNotRun(t *testing.T) {
	t.Parallel()

	s := scenStepsFocused(t, scenStepsState(), 120, 32)
	_, _ = s.Update(press('j')) // step cursor onto "Purchase Authorization"

	_, cmd := s.Update(scenPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on a step must yield a cmd")
	}
	msg, ok := cmd().(ScenarioStepDetailMsg)
	if !ok {
		t.Fatalf("enter on a step yielded %T, want ScenarioStepDetailMsg (must not run)", cmd())
	}
	if msg.StepIndex != 2 || msg.ScenarioID != "E2E Purchase and Reversal" {
		t.Fatalf("detail msg = %+v, want step 2 of E2E Purchase and Reversal", msg)
	}

	// Empty step stream: honest no-op.
	empty := scenStepsFocused(t, scenListState(), 120, 32)
	if _, cmd := empty.Update(scenPress(tea.KeyEnter)); cmd != nil {
		t.Fatalf("enter with no steps ran %v, want nil", cmd())
	}

	// List-pane Enter still runs.
	_, _ = s.Update(PaneFocusMsg{Reverse: true})
	_, cmd = s.Update(scenPress(tea.KeyEnter))
	if msg, ok := cmd().(ScenarioRunMsg); !ok || msg.ID != "E2E Purchase and Reversal" {
		t.Fatalf("list-pane enter msg = %#v, want ScenarioRunMsg", cmd())
	}
}

// Overlay arms on a new Preview identity, Esc closes it first, a
// same-identity re-push stays closed, a nil Preview clears it.
func TestScenariosStepPreviewArmsClosesAndReArms(t *testing.T) {
	t.Parallel()

	s := scenPage(t, scenStepsState(), 120, 32)
	if s.StepPreviewOpen() {
		t.Fatal("no Preview pushed: the overlay must stay closed")
	}

	st := scenStepsState()
	st.Preview = &ScenarioStepPreview{StepIndex: 2, ScenarioID: "E2E Purchase and Reversal", Loading: true}
	s.SetState(st)
	if !s.StepPreviewOpen() {
		t.Fatal("a new Preview identity must arm the overlay")
	}
	if body := ansi.Strip(s.View().Content); !strings.Contains(body, ".. loading") {
		t.Errorf("the loading payload must render the loading marker:\n%s", body)
	}

	_, cmd := s.Update(scenPress(tea.KeyEscape))
	if s.StepPreviewOpen() {
		t.Fatal("esc must close the overlay")
	}
	if cmd != nil {
		t.Fatalf("the overlay ate the esc, got %v", cmd())
	}
	if _, cmd := s.Update(scenPress(tea.KeyEscape)); cmd == nil {
		t.Fatal("the second esc must reach the page (ScenarioPopMsg)")
	}

	// Same-identity re-push must not re-open a closed overlay.
	s.SetState(st)
	if s.StepPreviewOpen() {
		t.Fatal("a same-identity re-push must not re-open a closed overlay")
	}

	// A new identity re-arms with the reconstructed sections.
	st2 := scenStepsState()
	st2.Preview = &ScenarioStepPreview{
		StepIndex: 3, ScenarioID: "E2E Purchase and Reversal",
		Request:  &TxReviewMessage{HEX: "0200F2388018", Describe: ` 2  "4242424242424242"`},
		Response: &TxReviewMessage{HEX: "0210F2388018", Describe: `39  "00"`},
	}
	s.SetState(st2)
	if !s.StepPreviewOpen() {
		t.Fatal("a new Preview identity must re-arm the overlay")
	}
	body := ansi.Strip(s.View().Content)
	for _, want := range []string{
		"STEP 3 - E2E Purchase and Reversal", // ascii degrades the "·"
		"REQUEST HEX", "0200F2388018", "REQUEST FIELDS", `4242424242424242`,
		"RESPONSE HEX", `39  "00"`, "j | k scroll | esc close",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("preview body missing %q:\n%s", want, body)
		}
	}

	// A nil Preview closes the overlay.
	st3 := scenStepsState()
	s.SetState(st3)
	if s.StepPreviewOpen() {
		t.Fatal("a nil Preview must close the overlay")
	}
}

// No captured message and no load in flight: honest run hint only.
func TestScenariosStepPreviewEmptyState(t *testing.T) {
	t.Parallel()

	st := scenStepsState()
	st.Preview = &ScenarioStepPreview{StepIndex: 1, ScenarioID: "Decline matrix"}
	s := scenPage(t, st, 120, 32)

	body := ansi.Strip(s.View().Content)
	if !strings.Contains(body, "run the scenario to capture the message") {
		t.Errorf("the empty preview must name the cause:\n%s", body)
	}
	if strings.Contains(body, "REQUEST HEX") {
		t.Errorf("no payload must not fake sections:\n%s", body)
	}
}

// Request-only preview: REQUEST sections plus an honest no-response line.
func TestScenariosStepPreviewNoResponseSection(t *testing.T) {
	t.Parallel()

	st := scenStepsState()
	st.Preview = &ScenarioStepPreview{
		StepIndex: 3, ScenarioID: "Decline matrix",
		Request: &TxReviewMessage{HEX: "00000000  02 00 f0 00 00 00 00 00", Describe: "MTI : 0200"},
	}
	s := scenPage(t, st, 120, 32)

	body := ansi.Strip(s.View().Content)
	for _, want := range []string{
		"REQUEST HEX", "00000000  02 00 f0 00 00 00 00 00", "REQUEST FIELDS", "MTI : 0200",
		"RESPONSE", "no response - run the scenario",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("no-response preview missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "RESPONSE HEX") {
		t.Errorf("a missing response must not fake a hex block:\n%s", body)
	}
}

// A preview folded from a failed load shows the error note verbatim.
func TestScenariosStepPreviewErrorNote(t *testing.T) {
	t.Parallel()

	note := `no scenario named "Decline matrix"`
	st := scenStepsState()
	st.Preview = &ScenarioStepPreview{StepIndex: 9, ScenarioID: "Decline matrix", Note: note}
	s := scenPage(t, st, 120, 32)

	body := ansi.Strip(s.View().Content)
	if !strings.Contains(body, note) {
		t.Errorf("the error preview must show the note verbatim:\n%s", body)
	}
	if strings.Contains(body, scenPreviewEmptyText) {
		t.Errorf("a known failure must not show the generic hint:\n%s", body)
	}
	if strings.Contains(body, "REQUEST HEX") {
		t.Errorf("a failed load must not fake message sections:\n%s", body)
	}
}

// A pending step's composed request is labeled "composed — not sent yet";
// a captured payload (Composed:false) shows no such label.
func TestScenariosStepPreviewComposedLabel(t *testing.T) {
	t.Parallel()

	st := scenStepsState()
	st.Preview = &ScenarioStepPreview{
		StepIndex: 3, ScenarioID: "Decline matrix", Composed: true,
		Request: &TxReviewMessage{HEX: "00000000  02 00 f0 00 00 00 00 00", Describe: "MTI : 0200"},
	}
	body := ansi.Strip(scenPage(t, st, 120, 32).View().Content)
	if !strings.Contains(body, scenPreviewComposedText) {
		t.Fatalf("a composed (pending) request must carry the composed label:\n%s", body)
	}
	labelAt := strings.Index(body, scenPreviewComposedText)
	hexAt := strings.Index(body, "REQUEST HEX")
	if hexAt < 0 || labelAt > hexAt {
		t.Errorf("the label must sit next to the REQUEST title (label@%d, hex title@%d)", labelAt, hexAt)
	}

	// Captured payload (Composed:false) must not carry the label.
	st2 := scenStepsState()
	st2.Preview = &ScenarioStepPreview{
		StepIndex: 2, ScenarioID: "E2E Purchase and Reversal",
		Request:  &TxReviewMessage{HEX: "0200F2388018", Describe: ` 2  "4242424242424242"`},
		Response: &TxReviewMessage{HEX: "0210F2388018", Describe: `39  "00"`},
	}
	if body := ansi.Strip(scenPage(t, st2, 120, 32).View().Content); strings.Contains(body, scenPreviewComposedText) {
		t.Errorf("a captured run payload must not carry the composed label:\n%s", body)
	}
}

// Overlay taller than the window: j scrolls to the clamped extent, k back.
func TestScenariosStepPreviewScrolls(t *testing.T) {
	t.Parallel()

	describe := strings.Repeat("6  \"padded field line\"\n", 40)
	st := scenStepsState()
	st.Preview = &ScenarioStepPreview{
		StepIndex: 2, ScenarioID: "E2E Purchase and Reversal",
		Request: &TxReviewMessage{HEX: "0200F2388018", Describe: strings.TrimRight(describe, "\n")},
	}
	s := scenPage(t, st, 120, 16)
	if !s.StepPreviewOpen() {
		t.Fatal("the pushed Preview must arm the overlay")
	}
	if body := ansi.Strip(s.View().Content); strings.Contains(body, "esc close") {
		t.Fatal("the hint line must start below the fold (otherwise this test proves nothing)")
	}

	ext := s.stepPreviewScrollExtent()
	if ext <= 0 {
		t.Fatalf("fixture must overflow the window, extent = %d", ext)
	}
	for range ext + 5 {
		_, _ = s.Update(press('j'))
	}
	body := ansi.Strip(s.View().Content)
	if !strings.Contains(body, "j | k scroll | esc close") {
		t.Errorf("scrolling must reach the overlay's last line:\n%s", body)
	}
	if s.stepPreviewScroll != ext {
		t.Fatalf("scroll = %d, want the clamped extent %d", s.stepPreviewScroll, ext)
	}

	_, _ = s.Update(press('k'))
	if s.stepPreviewScroll != ext-1 {
		t.Fatalf("k: scroll = %d, want %d", s.stepPreviewScroll, ext-1)
	}
}

// While open, the overlay swallows page triggers; the page never claims
// the router keyboard (Tab keeps reaching the router).
func TestScenariosStepPreviewOwnsKeyboard(t *testing.T) {
	t.Parallel()

	st := scenStepsState()
	st.Preview = &ScenarioStepPreview{StepIndex: 2, ScenarioID: "E2E Purchase and Reversal", Loading: true}
	s := scenPage(t, st, 120, 32)

	if s.ClaimsKeyboard() {
		t.Fatal("the page must not claim the keyboard on overlay-open (the router Tab/PaneFocus contract)")
	}
	paneBefore := s.Pane()

	if _, cmd := s.Update(press('e')); cmd != nil {
		t.Fatalf("export fired under the overlay: %v", cmd())
	}
	if _, cmd := s.Update(press('/')); cmd != nil {
		t.Fatalf("filter trigger fired under the overlay: %v", cmd())
	}
	if f, filtering := s.Filter(); filtering || f != "" {
		t.Fatalf("filter mode armed under the overlay: %q %v", f, filtering)
	}
	_, _ = s.Update(PaneFocusMsg{})
	if s.Pane() != paneBefore {
		t.Fatal("Tab must not move pane focus while the overlay owns the keyboard")
	}
}
