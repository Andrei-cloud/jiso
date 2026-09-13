package pages

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// analyze_items_test.go pins the generated-item picker overlay (UAT
// round 6): a run re-presents it, space/a toggle the local inclusion
// set, Enter applies the deselection, Esc discards, and a fresh
// ItemsID (new run) re-arms it.

// analyzeItemsFixture is the picker roster for the UAT round 6 tests.
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

// TestAnalyzeItemsPickerOpensAndApplies UAT round 6: a run result
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

// TestAnalyzeItemsPickerEscAppliesAndXReopens UAT round 7: Esc APPLIES the
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

// TestAnalyzeItemPickerCouplesDataset UAT round 7: a transaction and the
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

// TestAnalyzeItemsPickerReopensOnNewItemsID UAT round 6: a NEW run
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

// TestAnalyzeItemsPickerColumnsAlign UAT round 6 QA: the roster's NAME
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
