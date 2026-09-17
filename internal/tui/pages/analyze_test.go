// analyze_test.go covers the §J wizard page units: rail, spec/header
// selection, run-step inline keys, step deltas, Esc, width. Capture step:
// analyze_capture_test.go; state machine and async legs: root_analyze_test.go.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/frame"
)

// analyzeFixtureState is the wireframe §J data (fixed display strings:
// no clock, no real paths).
func analyzeFixtureState() AnalyzeState {
	st := AnalyzeState{
		Step:        StepRun,
		Status:      AnalyzeStatusDone,
		Goal:        AnalyzeGoalTransactions,
		SpecPath:    "./spec.json",
		Header:      "binary2",
		CapturePath: "/captures/capture.pcap",
		Parsed:      412,
		Unparsable:  3,
	}
	st.CaptureItems = []WizardItem{
		{Label: "night.pcap", Path: "/captures/night.pcap"},
		{Label: "capture.pcap", Path: "/captures/capture.pcap", Current: true},
	}
	st.SpecItems = []WizardItem{
		{Label: "spec.json", Path: "./spec.json", Current: true},
		{Label: "other.json", Path: "./other.json"},
	}
	st.Goals = []AnalyzeRadio{
		{Key: "t", Label: "transactions + datasets", Selected: true},
		{Key: "r", Label: "mock routes"},
		{Key: "s", Label: "scenario flow"},
	}
	st.Headers = []AnalyzeHeaderItem{
		{Header: "binary2", Selected: true},
		{Header: "ascii4"},
		{Header: "naps"},
		{Header: "binary4"},
		{Header: "bcd2"},
		{Header: "visa"},
	}
	st.Flows = []AnalyzeFlowRow{
		{Port: 8080, Direction: "dst", Msgs: 205, MTIs: "0200(180) 0800(25)", Signon: true, Selectable: true, Selected: true},
		{Port: 8080, Direction: "src", Msgs: 184, MTIs: "0210(178) 0810(6)"},
		{Port: 9999, Direction: "dst", Msgs: 23, MTIs: "0200(23)", Selectable: true},
		{Port: 9999, Direction: "src", Msgs: 23, MTIs: "0210(23)"},
	}
	st.Masking = []AnalyzeRadio{
		{Key: "m", Label: "mask sensitive fields", Selected: true},
		{Key: "r", Label: "keep raw (unsecure)"},
	}

	return st
}

// analyzePage builds an ascii page at the given size with state pushed.
func analyzePage(t *testing.T, st AnalyzeState, w, h int) *Analyze {
	t.Helper()
	a := NewAnalyze(asciiTheme(t))
	a.SetState(st)
	_, _ = a.Update(windowSize(w, h))

	return a
}

// cmdMsg runs the command returned by Update (nil-safe).
func cmdMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}

	return cmd()
}

func TestAnalyzeRailLabelsAllSteps(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	a := analyzePage(t, st, 120, 32)
	body := ansi.Strip(a.View().Content)

	for _, want := range []string{"PCAP ANALYZE", "1 capture", "2 spec", "3 header", "4 run"} {
		if !strings.Contains(body, want) {
			t.Errorf("rail lacks %q:\n%s", want, body)
		}
	}
}

func TestAnalyzeSpecStepCommitsSelection(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepSpec
	a := analyzePage(t, st, 120, 32)

	if a.ClaimsKeyboard() {
		t.Fatal("a fresh spec step is navigate mode: globals (q, digits, ?) work until typing starts")
	}
	if a.ListCursor() != 0 {
		t.Errorf("cursor = %d, want the current spec at 0", a.ListCursor())
	}
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeCommitSpecMsg)
	if !ok || msg.Value != "./spec.json" {
		t.Errorf("Enter -> %T %+v, want CommitSpec ./spec.json", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}

	// A typed path overrides the list; typing claims the keyboard (edit mode).
	st = analyzeFixtureState()
	st.Step = StepSpec
	a = analyzePage(t, st, 120, 32)
	_, _ = a.Update(ch('.')) // the first byte reaches the page unclaimed
	if !a.ClaimsKeyboard() {
		t.Fatal("typing a path must claim the keyboard (paths contain q/digits)")
	}
	for _, r := range "/my spec.json" {
		_, _ = a.Update(ch(r))
	}
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg, ok := cmdMsg(t, cmd).(AnalyzeCommitSpecMsg); !ok || msg.Value != "./my spec.json" {
		t.Errorf("typed spec -> %T %+v, want ./my spec.json", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}
}

// Two-mode browse gate: navigate-mode f browses with IsSpec:true; typing
// enters edit mode where f types literally; Esc returns to navigate.
func TestAnalyzeSpecBrowseTracksEditMode(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepSpec
	a := analyzePage(t, st, 120, 32)

	// Navigate mode: f browses and names the spec step.
	if a.Editing() {
		t.Fatal("a fresh spec step must be navigate mode")
	}
	_, cmd := a.Update(ch('f'))
	msg, ok := cmdMsg(t, cmd).(AnalyzeBrowseMsg)
	if !ok {
		t.Fatalf("navigate-mode f -> %T, want AnalyzeBrowseMsg", cmdMsg(t, cmd))
	}
	if !msg.IsSpec {
		t.Fatal("spec-step browse must carry IsSpec: true, or root reopens the capture picker")
	}

	// Typing enters edit mode (and types itself).
	_, _ = a.Update(ch('n'))
	if !a.Editing() {
		t.Fatal("typing must enter edit mode")
	}

	// Edit mode: f types literally into the draft; no second browse.
	_, cmd = a.Update(ch('f'))
	if got := cmdMsg(t, cmd); got != nil {
		t.Fatalf("f while editing must not browse, got %#v", got)
	}
	if d, _ := a.Draft(); d != "nf" {
		t.Fatalf("draft = %q, want the typed suffix \"nf\"", d)
	}

	// Esc clears the draft back to navigate mode, where f browses again.
	_, _ = a.Update(special(tea.KeyEscape))
	if a.Editing() {
		t.Fatal("esc must leave edit mode")
	}
	_, cmd = a.Update(ch('f'))
	msg, ok = cmdMsg(t, cmd).(AnalyzeBrowseMsg)
	if !ok || !msg.IsSpec {
		t.Fatalf("f after leaving edit mode -> %#v, want AnalyzeBrowseMsg{IsSpec: true}", cmdMsg(t, cmd))
	}
}

func TestAnalyzeHeaderListSpaceSelects(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepHeader
	a := analyzePage(t, st, 120, 32)

	// The current framing leads the list (snapshot order).
	body := ansi.Strip(a.View().Content)
	iCur, iAlt := strings.Index(body, "binary2"), strings.Index(body, "ascii4")
	if iCur < 0 || iAlt < 0 || iCur > iAlt {
		t.Errorf("current header must lead the list:\n%s", body)
	}

	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := a.Update(ch(' '))
	if msg, ok := cmdMsg(t, cmd).(AnalyzeChooseHeaderMsg); !ok || msg.Header != "ascii4" {
		t.Errorf("space -> %T %+v, want ChooseHeader ascii4", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := cmdMsg(t, cmd).(AnalyzeNextMsg); !ok {
		t.Errorf("Enter -> %T, want AnalyzeNextMsg", cmdMsg(t, cmd))
	}
}

func TestAnalyzeRunInlineRowsAndKeys(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle
	a := analyzePage(t, st, 120, 32)

	body := ansi.Strip(a.View().Content)
	for _, want := range []string{"capture.pcap", "spec.json", "binary2", "transactions + datasets", "mock routes", "scenario flow", "mask PAN/track", ":8080", "0200(180) 0800(25)"} {
		if !strings.Contains(body, want) {
			t.Errorf("run step lacks %q:\n%s", want, body)
		}
	}

	cases := []struct {
		key  rune
		want any
	}{
		{'t', AnalyzeChooseGoalMsg{Goal: AnalyzeGoalTransactions}},
		{'r', AnalyzeChooseGoalMsg{Goal: AnalyzeGoalMockRoutes}},
		{'s', AnalyzeChooseGoalMsg{Goal: AnalyzeGoalScenario}},
		{'m', AnalyzeChooseMaskMsg{Raw: true}},
		{'w', AnalyzeWriteMsg{}},
	}
	for _, c := range cases {
		_, cmd := a.Update(ch(c.key))
		got := cmdMsg(t, cmd)
		if got != c.want { // same-type comparable messages
			t.Errorf("%c -> %T %+v, want %+v", c.key, got, got, c.want)
		}
	}
}

// TestAnalyzeOutputEditor: UAT round 5 — [o] on the run step opens the
// output-path editor (seeded with the effective path), typing edits it,
// Enter commits AnalyzeOutCommitMsg, Esc cancels without a message; the
// effective path renders on the run step's output row.
func TestAnalyzeOutputEditor(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle
	st.OutputPath = "transactions/transaction.json"
	a := analyzePage(t, st, 120, 32)

	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "transactions/transaction.json") {
		t.Errorf("output row must show the effective path:\n%s", body)
	}

	_, _ = a.Update(ch('o'))
	_, cmd := a.Update(ch('x'))
	if cmd != nil {
		t.Fatal("typing in the output editor must not emit messages")
	}
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if cmd != nil {
		t.Fatal("backspace in the output editor must not emit messages")
	}
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got, ok := cmdMsg(t, cmd).(AnalyzeOutCommitMsg)
	if !ok {
		t.Fatalf("enter must commit AnalyzeOutCommitMsg, got %#v", cmdMsg(t, cmd))
	}
	if got.Path != "transactions/transaction.json" {
		t.Errorf("committed path = %q, want the seeded path (typed char removed by backspace)", got.Path)
	}

	// Esc cancels without a message.
	_, _ = a.Update(ch('o'))
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatalf("esc must cancel the editor silently, got %v", cmd())
	}
}

// Out-editor browse gate: f in a freshly opened [o] editor hands the
// location to the shared picker; after an edit the gate closes.
func TestAnalyzeOutEditorBrowseGate(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle
	st.OutputPath = "transactions/transaction.json"
	a := analyzePage(t, st, 120, 32)

	_, _ = a.Update(ch('o'))
	_, cmd := a.Update(ch('f'))
	got, ok := cmdMsg(t, cmd).(AnalyzeOutBrowseMsg)
	if !ok {
		t.Fatalf("f in the fresh editor -> %T, want AnalyzeOutBrowseMsg", cmdMsg(t, cmd))
	}
	if got.Draft != "transactions/transaction.json" {
		t.Errorf("browse draft = %q, want the seeded path", got.Draft)
	}

	// After an edit the gate closes: f types, Enter commits the draft.
	_, _ = a.Update(ch('o'))
	if _, cmd := a.Update(ch('x')); cmd != nil {
		t.Fatal("typing in the fresh editor must not emit messages")
	}
	if _, cmd := a.Update(ch('f')); cmd != nil {
		t.Fatalf("f after an edit must type, not browse: %v", cmd())
	}
	_, cmd = a.Update(special(tea.KeyEnter))
	committed, ok := cmdMsg(t, cmd).(AnalyzeOutCommitMsg)
	if !ok {
		t.Fatalf("enter must commit AnalyzeOutCommitMsg, got %#v", cmdMsg(t, cmd))
	}
	if want := "transactions/transaction.jsonxf"; committed.Path != want {
		t.Errorf("committed path = %q, want %q", committed.Path, want)
	}
}

func TestAnalyzeRunFlowFilterAndEnter(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle
	a := analyzePage(t, st, 120, 32)

	if a.ClaimsKeyboard() {
		t.Fatal("the run step must not claim the keyboard until / opens")
	}
	_, _ = a.Update(ch('/'))
	if !a.ClaimsKeyboard() {
		t.Fatal("/ must open the flow filter and claim the keyboard")
	}
	for _, r := range "9999" {
		_, _ = a.Update(ch(r))
	}
	body := ansi.Strip(a.View().Content)
	if !strings.Contains(body, ":9999") || strings.Contains(body, ":8080") {
		t.Errorf("filter must narrow the flows block:\n%s", body)
	}
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg, ok := cmdMsg(t, cmd).(AnalyzeRunMsg); !ok || msg.Filter != "9999" {
		t.Errorf("Enter -> %T %+v, want RunMsg 9999", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}

	// Backspace trims the open filter; Esc clears it first.
	a = analyzePage(t, st, 120, 32)
	_, _ = a.Update(ch('/'))
	_, _ = a.Update(ch('9'))
	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	draft, editing := a.Draft()
	if !editing || draft != "" {
		t.Errorf("backspace: draft = %q/%v, want empty editing", draft, editing)
	}
}

func TestAnalyzeRunStatusAndPreview(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.Preview = "dry-run: would write 6 generated ConfigItem(s) to 'transactions/transaction.json'"
	a := analyzePage(t, st, 120, 32)
	body := ansi.Strip(a.View().Content)
	if !strings.Contains(body, "would write 6 generated ConfigItem(s)") {
		t.Errorf("results preview missing:\n%s", body)
	}

	st.Status = AnalyzeStatusRunning
	a = analyzePage(t, st, 120, 32)
	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "running") {
		t.Errorf("running state missing:\n%s", body)
	}
}

func TestAnalyzeEscWalksBackAndAborts(t *testing.T) {
	t.Parallel()

	// Esc with a draft clears it (no step message).
	st := analyzeFixtureState()
	st.Step = StepSpec
	a := analyzePage(t, st, 120, 32)
	_, _ = a.Update(ch('x'))
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Errorf("Esc with a draft -> %T, want nil (clear only)", cmdMsg(t, cmd))
	}

	// Esc beyond the first step walks back through root's jump.
	st.Step = StepHeader
	a = analyzePage(t, st, 120, 32)
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if msg, ok := cmdMsg(t, cmd).(AnalyzeStepDeltaMsg); !ok || msg.Delta != -1 {
		t.Errorf("Esc on step 3 -> %T %+v, want StepDelta -1", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}

	// Esc on the first step aborts (root confirms over in-flight legs).
	st.Step = StepCapture
	a = analyzePage(t, st, 120, 32)
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmdMsg(t, cmd).(AnalyzeAbortMsg); !ok {
		t.Errorf("Esc on step 1 -> %T, want AnalyzeAbortMsg", cmdMsg(t, cmd))
	}
}

func TestAnalyzeStepDeltaKeys(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeFixtureState(), 120, 32)

	cases := []struct {
		msg   tea.Msg
		delta int
	}{
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, 1},
		{tea.KeyPressMsg{Code: tea.KeyPgUp}, -1},
		{tea.KeyPressMsg{Code: tea.KeyTab}, 1},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, -1},
		{PaneFocusMsg{}, 1},
		{PaneFocusMsg{Reverse: true}, -1},
	}
	for i, c := range cases {
		_, cmd := a.Update(c.msg)
		msg, ok := cmdMsg(t, cmd).(AnalyzeStepDeltaMsg)
		if !ok || msg.Delta != c.delta {
			t.Errorf("case %d -> %T %+v, want StepDelta %+d", i, cmdMsg(t, cmd), cmdMsg(t, cmd), c.delta)
		}
	}
}

func TestAnalyzeDraftResetsOnStepChange(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	a := analyzePage(t, st, 120, 32)
	_, _ = a.Update(ch('x'))

	st.Step = StepRun
	st.FlowFilter = "8080"
	a.SetState(st)
	if d, editing := a.Draft(); editing || d != "8080" {
		t.Errorf("after step change draft = %q/%v, want the committed filter seeded", d, editing)
	}
	if a.ClaimsKeyboard() {
		t.Error("the run step must not claim the keyboard on arrival")
	}
}

// TestAnalyzeNoLineExceedsContentWidth: at 160/120/100/80 columns every
// rendered line stays inside the content width on every step (clip,
// never wrap — the frame must never break).
func TestAnalyzeNoLineExceedsContentWidth(t *testing.T) {
	t.Parallel()

	for _, step := range []int{StepCapture, StepSpec, StepHeader, StepRun} {
		for _, w := range []int{160, 120, 100, 80} {
			st := analyzeFixtureState()
			st.Step = step
			st.Preview = "dry-run: would write 6 generated ConfigItem(s) to 'transactions/transaction.json'"
			st.WriteLine = "wrote 6 items to transactions/transaction.json"
			st.Note = "no flows match the filter - clear it or fix it"
			a := analyzePage(t, st, w, 32)
			cw, _ := frame.ContentSize(w, 32)
			for i, line := range strings.Split(a.View().Content, "\n") {
				if lw := lipgloss.Width(line); lw > cw {
					t.Errorf("step %d width %d line %d overflows (%d > %d): %q", step, w, i, lw, cw, line)
				}
			}
		}
	}
}

// ch is the pages-package key builder (press exists for printable
// runes; this mirrors the tui tests' helper name).
func ch(c rune) tea.KeyPressMsg { return press(c) }

// TestAnalyzeFlowCursor: the run step's flow rows are selectable — j/k move
// the cursor over every visible direction row, space toggles the row under
// the cursor and names its (port, direction), a runs all/none, and the view
// distinguishes included (●) from excluded (○) with a ▸ cursor (UAT round 7:
// each direction row is its own unit).
func TestAnalyzeFlowCursor(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle
	a := analyzePage(t, st, 120, 32)

	// Fixture: dst 8080 included, dst 9999 excluded; every row (dst and
	// src) carries its own inclusion marker.
	body := ansi.Strip(a.View().Content)
	if !strings.Contains(body, "> [x] -> dst :8080") || !strings.Contains(body, "[ ] -> dst :9999") {
		t.Fatalf("flow rows lack cursor/inclusion glyphs:\n%s", body)
	}
	if !strings.Contains(body, "[ ] <- src :8080") {
		t.Fatalf("src row lacks its own inclusion marker:\n%s", body)
	}

	_, cmd := a.Update(ch(' '))
	if msg, ok := cmdMsg(t, cmd).(AnalyzeFlowToggleMsg); !ok || msg.Port != 8080 || msg.Dir != "dst" {
		t.Errorf("space -> %T %+v, want toggle 8080/dst", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}

	// The cursor walks every row: down lands on the src row and space
	// there toggles that direction on its own (port, direction).
	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = a.Update(ch(' '))
	if msg, ok := cmdMsg(t, cmd).(AnalyzeFlowToggleMsg); !ok || msg.Port != 8080 || msg.Dir != "src" {
		t.Errorf("down(src)+space -> %T %+v, want toggle 8080/src", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}

	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = a.Update(ch(' '))
	if msg, ok := cmdMsg(t, cmd).(AnalyzeFlowToggleMsg); !ok || msg.Port != 9999 || msg.Dir != "dst" {
		t.Errorf("down down+space -> %T %+v, want toggle 9999/dst", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}

	// And to the last row (src :9999): the cursor reaches every row.
	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = a.Update(ch(' '))
	if msg, ok := cmdMsg(t, cmd).(AnalyzeFlowToggleMsg); !ok || msg.Port != 9999 || msg.Dir != "src" {
		t.Errorf("three downs+space -> %T %+v, want toggle 9999/src (last row)", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}

	_, cmd = a.Update(ch('a'))
	if _, ok := cmdMsg(t, cmd).(AnalyzeFlowToggleAllMsg); !ok {
		t.Errorf("a -> %T, want AnalyzeFlowToggleAllMsg", cmdMsg(t, cmd))
	}

	// A filter that keeps one dst row clamps the cursor to it.
	st2 := analyzeFixtureState()
	st2.Step = StepRun
	st2.Status = AnalyzeStatusIdle
	a2 := analyzePage(t, st2, 120, 32)
	_, _ = a2.Update(ch('/'))
	_, _ = a2.Update(ch('9'))
	_, _ = a2.Update(ch('9'))
	_, _ = a2.Update(ch('9'))
	_, _ = a2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, cmd = a2.Update(ch(' '))
	if msg, ok := cmdMsg(t, cmd).(AnalyzeFlowToggleMsg); !ok || msg.Port != 9999 {
		t.Errorf("filtered space -> %T %+v, want toggle 9999 only", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}
}
