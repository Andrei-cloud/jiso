package pages

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// analyze_items_test.go pins the generated-item picker overlay (UAT
// round 6): a run re-presents it, space/a toggle the local inclusion
// set, Enter applies the deselection, Esc discards, and a fresh
// ItemsID (new run) re-arms it.

// analyzeItemsFixture is the picker roster for the tests.
func analyzeItemsFixture() []AnalyzeItemRow {
	return []AnalyzeItemRow{
		{
			Key: "transaction|Captured Flow 0200_0", Name: "Captured Flow 0200_0", Kind: "transaction",
			Included: true, Preview: "{\n  \"type\": \"transaction\",\n  \"name\": \"Captured Flow 0200_0\"\n}",
		},
		{
			Key: "dataset|pool", Name: "pool", Kind: "dataset",
			Included: true, Preview: "{\n  \"name\": \"pool\"\n}",
		},
		{
			Key: "mock_route|route-0200-00", Name: "route-0200-00", Kind: "mock_route",
			Included: true, Preview: "{\n  \"name\": \"route-0200-00\"\n}",
		},
	}
}

func analyzeDoneWithItems() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.Items = analyzeItemsFixture()
	st.ItemsID = 1

	return st
}

// TestAnalyzeItemsPickerOpensAndApplies: a run result
// re-presents the picker (claims the keyboard, lists every item and the
// cursor row's file form); space deselects, Enter applies the
// deselection, and the picker closes.
func TestAnalyzeItemsPickerOpensAndApplies(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeDoneWithItems(), 120, 32)
	if !a.ClaimsKeyboard() {
		t.Fatal("the picker must claim the keyboard while open")
	}
	body := ansi.Strip(a.View().Content)
	for _, want := range []string{"ITEMS  3 of 3", "Captured Flow 0200_0", "PREVIEW", "route-0200-00"} {
		if !strings.Contains(body, want) {
			t.Errorf("picker body lacks %q:\n%s", want, body)
		}
	}

	_, cmd := a.Update(ch(' ')) // deselect the cursor row (local)
	if cmd != nil {
		t.Fatalf("space must not emit, got %v", cmd)
	}
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeItemsApplyMsg)
	if !ok {
		t.Fatalf("enter yielded %T, want AnalyzeItemsApplyMsg", cmd)
	}
	if len(msg.Excluded) != 1 || msg.Excluded[0] != "transaction|Captured Flow 0200_0" {
		t.Fatalf("excluded = %v, want the deselected transaction key", msg.Excluded)
	}
	if a.ClaimsKeyboard() {
		t.Fatal("apply must close the picker")
	}
}

// TestAnalyzeItemsPickerEscAppliesAndXReopens: Esc APPLIES the
// selection (no longer silently discards it), so backing out to the run step
// and pressing w writes exactly the picked set; [x] reopens the picker; the
// 'a' key toggles all/none.
func TestAnalyzeItemsPickerEscAppliesAndXReopens(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeDoneWithItems(), 120, 32)
	_, _ = a.Update(ch(' '))                   // deselect row 0 locally
	_, cmd := a.Update(special(tea.KeyEscape)) // close, APPLYING the selection
	if a.ClaimsKeyboard() {
		t.Fatal("esc must close the picker")
	}
	if msg, ok := cmdMsg(t, cmd).(AnalyzeItemsApplyMsg); !ok ||
		len(msg.Excluded) != 1 || msg.Excluded[0] != "transaction|Captured Flow 0200_0" {
		t.Fatalf("esc must apply the deselection, got %+v", cmd)
	}

	// a: deselect all; Enter excludes every key.
	_, _ = a.Update(ch('x'))
	if !a.ClaimsKeyboard() {
		t.Fatal("[x] must reopen the picker")
	}
	_, _ = a.Update(ch('a'))
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg, ok := cmdMsg(t, cmd).(AnalyzeItemsApplyMsg); !ok || len(msg.Excluded) != 3 {
		t.Fatalf("a + enter must exclude all three keys, got %+v", cmd)
	}
}

// TestAnalyzeItemPickerCouplesDataset: a transaction and the
// dataset it draws from share a group, so deselecting the transaction
// deselects its dataset too (and vice versa) — the write carries a dataset
// only with its transaction.
func TestAnalyzeItemPickerCouplesDataset(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.ItemsID = 1
	st.Items = []AnalyzeItemRow{
		{Key: "transaction|Captured Flow 0200_0", Name: "Captured Flow 0200_0", Kind: "transaction", Group: "dataset_0200_0", Included: true},
		{Key: "dataset|dataset_0200_0", Name: "dataset_0200_0", Kind: "dataset", Group: "dataset_0200_0", Included: true},
		{Key: "mock_route|route-0200-00", Name: "route-0200-00", Kind: "mock_route", Included: true},
	}
	a := analyzePage(t, st, 140, 32)

	// Cursor leads at the transaction (row 0); space deselects it and its
	// dataset together, leaving the unrelated route included.
	_, _ = a.Update(ch(' '))
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeItemsApplyMsg)
	if !ok {
		t.Fatalf("enter yielded %T, want AnalyzeItemsApplyMsg", cmd)
	}
	if !slices.Contains(msg.Excluded, "transaction|Captured Flow 0200_0") ||
		!slices.Contains(msg.Excluded, "dataset|dataset_0200_0") {
		t.Errorf("deselecting the transaction must deselect its dataset too: %v", msg.Excluded)
	}
	if slices.Contains(msg.Excluded, "mock_route|route-0200-00") {
		t.Errorf("the unrelated route must stay included: %v", msg.Excluded)
	}
}

// TestAnalyzeItemsPickerReopensOnNewItemsID: a NEW run
// (ItemsID bump) re-presents the picker; re-pushes of the same ID
// leave the operator's close in place.
func TestAnalyzeItemsPickerReopensOnNewItemsID(t *testing.T) {
	t.Parallel()

	st := analyzeDoneWithItems()
	a := analyzePage(t, st, 120, 32)
	_, _ = a.Update(special(tea.KeyEscape))
	if a.ClaimsKeyboard() {
		t.Fatal("esc closed the picker")
	}
	a.SetState(st) // same ItemsID: stays closed
	if a.ClaimsKeyboard() {
		t.Fatal("re-push of the same ItemsID must not re-open the picker")
	}
	st.ItemsID = 2 // a fresh run
	a.SetState(st)
	if !a.ClaimsKeyboard() {
		t.Fatal("a new ItemsID must re-present the picker")
	}
}

// TestAnalyzeItemsPickerColumnsAlign QA: the roster's NAME
// and KIND columns must line up on every row AND with the header, no
// matter how long each item name is (names are padded to a fixed width).
func TestAnalyzeItemsPickerColumnsAlign(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeDoneWithItems(), 120, 32)
	roster := ansi.Strip(a.itemsRoster(46, 12))
	lines := strings.Split(roster, "\n")
	if len(lines) < 4 {
		t.Fatalf("roster too short:\n%s", roster)
	}
	nameCol := strings.Index(lines[0], "NAME")
	kindCol := strings.Index(lines[0], "KIND")
	if nameCol < 0 || kindCol < 0 {
		t.Fatalf("header lacks NAME/KIND:\n%s", roster)
	}
	for _, it := range analyzeItemsFixture() {
		row := ""
		for _, l := range lines[1:] {
			if strings.Contains(l, it.Name) {
				row = l

				break
			}
		}
		if row == "" {
			t.Fatalf("no roster row for %q:\n%s", it.Name, roster)
		}
		if c := strings.Index(row, it.Name); c != nameCol {
			t.Errorf("name %q starts at col %d, want the header col %d:\n%s", it.Name, c, nameCol, row)
		}
		if c := strings.Index(row, it.Kind); c != kindCol {
			t.Errorf("kind %q (%s) starts at col %d, want the header col %d:\n%s", it.Kind, it.Name, c, kindCol, row)
		}
	}
}

// --- the generated-item preview scrolls ----------------------------------

// tallPreview builds an n-line file form with a uniquely-named row per
// line (line-00 .. line-99, zero-padded so no name prefixes another).
func tallPreview(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line-" + strconv.Itoa(i/10%10) + strconv.Itoa(i%10)
	}

	return strings.Join(lines, "\n")
}

// analyzeItemsTallState is a picker roster whose previews overflow the
// pane at 120x32, so the preview window is observable.
func analyzeItemsTallState() AnalyzeState {
	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.ItemsID = 1
	st.Items = []AnalyzeItemRow{
		{Key: "transaction|tall", Name: "tall", Kind: "transaction", Included: true, Preview: tallPreview(100)},
		{Key: "mock_route|wide", Name: "wide", Kind: "mock_route", Included: true, Preview: tallPreview(60)},
	}

	return st
}

// A tall preview in a short pane: ScrollPreview moves the window, clamping
// at both ends; scroll keys reach the preview only while it is focused.
func TestPreviewScrollsWhenOverflowing(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeItemsTallState(), 120, 32)
	paneH := a.previewWindow()
	contentH := a.previewContentHeight()
	maxOff := contentH - paneH
	if maxOff <= 0 {
		t.Fatalf("fixture must overflow the pane: %d content rows, %d visible", contentH, paneH)
	}

	// At the top the window starts at the first line, not the last.
	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "line-00") || strings.Contains(body, "line-99") {
		t.Fatalf("top window wrong (want line-00 visible, line-99 hidden):\n%s", body)
	}

	// +1 moves the window down one row: the first line scrolls out.
	a.ScrollPreview(1)
	if a.previewOff != 1 {
		t.Fatalf("ScrollPreview(+1): previewOff = %d, want 1", a.previewOff)
	}
	if body := ansi.Strip(a.View().Content); strings.Contains(body, "line-00") {
		t.Fatalf("scrolled window still shows the first line:\n%s", body)
	}

	// Clamps at the bottom, and the bottom window shows the last line.
	a.ScrollPreview(maxOff)
	if a.previewOff != maxOff {
		t.Fatalf("ScrollPreview: previewOff = %d, want the bottom clamp %d", a.previewOff, maxOff)
	}
	a.ScrollPreview(4)
	if a.previewOff != maxOff {
		t.Fatalf("ScrollPreview past the bottom must clamp at %d, got %d", maxOff, a.previewOff)
	}
	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "line-99") {
		t.Fatalf("bottom window must show the last line:\n%s", body)
	}

	// Clamps at the top.
	a.ScrollPreview(-4 * maxOff)
	if a.previewOff != 0 {
		t.Fatalf("ScrollPreview past the top must clamp at 0, got %d", a.previewOff)
	}

	// [tab] focuses the preview; j/k and the arrows scroll it, leaving
	// the roster cursor alone.
	_, _ = a.Update(special(tea.KeyTab))
	if !a.previewFocused {
		t.Fatal("[tab] must focus the preview sub-pane")
	}
	_, _ = a.Update(ch('j'))
	if a.previewOff != 1 || a.itemCursor != 0 {
		t.Fatalf("j over the preview: off %d cursor %d, want 1/0", a.previewOff, a.itemCursor)
	}
	_, _ = a.Update(special(tea.KeyDown))
	if a.previewOff != 2 || a.itemCursor != 0 {
		t.Fatalf("down over the preview: off %d cursor %d, want 2/0", a.previewOff, a.itemCursor)
	}
	_, _ = a.Update(special(tea.KeyPgDown))
	if a.previewOff != 2+paneH {
		t.Fatalf("pgdn must page the preview by one window: off %d, want %d", a.previewOff, 2+paneH)
	}
	_, _ = a.Update(special(tea.KeyPgUp))
	if a.previewOff != 2 {
		t.Fatalf("pgup must page the preview back: off %d, want 2", a.previewOff)
	}
	_, _ = a.Update(ch('k'))
	if a.previewOff != 1 {
		t.Fatalf("k over the preview: off %d, want 1", a.previewOff)
	}
	_, _ = a.Update(special(tea.KeyUp))
	if a.previewOff != 0 {
		t.Fatalf("up at the top must clamp: off %d, want 0", a.previewOff)
	}
	_, _ = a.Update(special(tea.KeyUp))
	if a.previewOff != 0 {
		t.Fatalf("up past the top must clamp: off %d, want 0", a.previewOff)
	}

	// [shift+tab] returns focus: cursor keys move the list again.
	_, _ = a.Update(modKey(tea.KeyTab, tea.ModShift))
	if a.previewFocused {
		t.Fatal("[shift+tab] must return the focus to the roster")
	}

	// Selecting a different item restarts its preview at the top.
	a.ScrollPreview(3)
	_, _ = a.Update(ch('j'))
	if a.itemCursor != 1 {
		t.Fatalf("j over the roster must move the item cursor, got %d", a.itemCursor)
	}
	if a.previewOff != 0 {
		t.Fatalf("a new item must restart its preview at the top, got off %d", a.previewOff)
	}

	// Closing (Esc applies) and reopening with [x] starts fresh.
	_, cmd := a.Update(special(tea.KeyEscape))
	if _, ok := cmdMsg(t, cmd).(AnalyzeItemsApplyMsg); !ok {
		t.Fatalf("esc must still apply the selection, got %v", cmd)
	}
	_, _ = a.Update(ch('x'))
	if a.previewOff != 0 || a.previewFocused {
		t.Fatalf("reopening must reset the preview sub-pane: off %d focused %v", a.previewOff, a.previewFocused)
	}
}

// scenarioLinkFixture is a scaffolded scenario roster: a purchase and its
// dataset, the route answering it, the purchase's reversal and the reversal's
// own route - rows linked as the root links them for a real scenario run.
func scenarioLinkFixture() []AnalyzeItemRow {
	txK := "transaction|Tx 0100 DE3=000000 #1"
	dsK := "dataset|dataset_captured"
	rtK := "mock_route|Mock Route #0001 0110 DE3=000000"
	rvK := "transaction|Reversal for 0100 DE3=000000 #1"
	rrK := "mock_route|Mock Reversal Route #0001 0400 DE3=000000"

	return []AnalyzeItemRow{
		{Key: txK, Name: "Tx 0100 DE3=000000 #1", Kind: "transaction", Group: "dataset_captured",
			Included: true, Preview: `{}`, Links: []string{rtK, rvK}},
		{Key: dsK, Name: "dataset_captured", Kind: "dataset", Group: "dataset_captured",
			Included: true, Preview: `{}`},
		{Key: rtK, Name: "Mock Route #0001 0110 DE3=000000", Kind: "mock_route", RC: "51",
			Included: true, Preview: `{}`, Links: []string{txK}},
		{Key: rvK, Name: "Reversal for 0100 DE3=000000 #1", Kind: "transaction",
			Included: true, Preview: `{}`, Links: []string{txK, rrK}},
		{Key: rrK, Name: "Mock Reversal Route #0001 0400 DE3=000000", Kind: "mock_route", RC: "00",
			Included: true, Preview: `{}`, Links: []string{rvK}},
	}
}

// TestAnalyzeItemsIncludeClosureCompletesScenario (UAT): picking ANY piece
// of the scenario selects the complete replayable flow - the response route
// alone pulls in the purchase (with its dataset), the purchase pulls its
// reversal, and the reversal pulls its own route.
func TestAnalyzeItemsIncludeClosureCompletesScenario(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.ItemsID = 1
	st.Items = scenarioLinkFixture()
	a := analyzePage(t, st, 140, 32)

	// Start from none: a deselects everything when all are included.
	_, _ = a.Update(ch('a'))

	// Pick just the response route (row 2): the closure completes the flow.
	_, _ = a.Update(ch('j'))
	_, _ = a.Update(ch('j'))
	_, _ = a.Update(ch(' '))

	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeItemsApplyMsg)
	if !ok {
		t.Fatalf("enter yielded %T, want AnalyzeItemsApplyMsg", cmd)
	}
	if len(msg.Excluded) != 0 {
		t.Errorf("picking one route must complete the scenario, still excluded: %v", msg.Excluded)
	}
}

// TestAnalyzeItemsDeselectCascadeIsLocal: deselecting a route drops that row
// alone - the transactions it answered stay (they are valid items on their
// own), and no linked item is dragged away from another kept selection.
func TestAnalyzeItemsDeselectCascadeIsLocal(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.ItemsID = 1
	st.Items = scenarioLinkFixture()
	a := analyzePage(t, st, 140, 32)

	_, _ = a.Update(ch('j'))
	_, _ = a.Update(ch('j'))
	_, _ = a.Update(ch(' '))

	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeItemsApplyMsg)
	if !ok {
		t.Fatalf("enter yielded %T, want AnalyzeItemsApplyMsg", cmd)
	}
	if len(msg.Excluded) != 1 || msg.Excluded[0] != "mock_route|Mock Route #0001 0110 DE3=000000" {
		t.Errorf("deselecting the route must drop only it, excluded: %v", msg.Excluded)
	}
}

// TestAnalyzeItemsRosterShowsResponseCode: the roster's RC column carries
// each route's answer (the response code it replays) so the operator can
// steer by outcome; non-route rows leave it blank.
func TestAnalyzeItemsRosterShowsResponseCode(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusDone
	st.ItemsID = 1
	st.Items = scenarioLinkFixture()
	a := analyzePage(t, st, 140, 32)

	view := ansi.Strip(a.View().Content)
	if !strings.Contains(view, "RC") {
		t.Errorf("the roster must show the RC column header:\n%s", view)
	}
	if !strings.Contains(view, "51") || !strings.Contains(view, "00") {
		t.Errorf("route rows must show the response codes they answer with:\n%s", view)
	}
}
