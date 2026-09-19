// analyze_match_test.go pins the matching wizard page: the conds keymap
// yields the root-bound messages, the value editor commits what was typed,
// and the group-by pane opens/closes locally while its toggles reach the
// root.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

func matchState() AnalyzeState {
	st := analyzeFixtureState()
	st.Goal = AnalyzeGoalMockRoutes
	st.Step = StepMatching
	st.Conds = []AnalyzeCond{
		{Side: "req", Field: "0", When: "equals", Value: "0200"},
		{Side: "req", Field: "3", When: "equals", Value: "000000"},
	}
	st.Variances = []AnalyzeGroupOption{
		{Field: "4", Side: "req", Vary: "2 distinct values"},
		{Field: "39", Side: "resp", Vary: "2 distinct values"},
	}
	st.MatchLine = "~42 pairs match · 2 route(s)"

	return st
}

func TestAnalyzeMatchingKeys(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, matchState(), 120, 32)

	cases := []struct {
		key  rune
		want any
	}{
		{'a', AnalyzeCondAddMsg{}},
		{'d', AnalyzeCondDeleteMsg{Index: 0}},
		{' ', AnalyzeCondWhenMsg{Index: 0}},
		{'s', AnalyzeCondSideMsg{Index: 0}},
	}
	for _, c := range cases {
		got := cmdMsg(t, keyMsg(t, a, c.key))
		if got != c.want {
			t.Errorf("%q -> %#v, want %#v", c.key, got, c.want)
		}
	}
}

// keyMsg presses a key and fails when the page returns no command.
func keyMsg(t *testing.T, a *Analyze, r rune) tea.Cmd {
	t.Helper()

	_, cmd := a.Update(press(r))
	if cmd == nil {
		t.Fatalf("key %q returned no cmd", r)
	}

	return cmd
}

func TestAnalyzeMatchingValueEditor(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, matchState(), 120, 32)

	// 'e' on a row with a Field edits its VALUE.
	_, _ = a.Update(press('e'))
	if !a.ClaimsKeyboard() {
		t.Fatal("the value editor must claim the keyboard")
	}
	for _, c := range "999" {
		_, _ = a.Update(press(c))
	}
	_, ecmd := a.Update(special(tea.KeyEnter))
	if ecmd == nil {
		t.Fatal("enter in the value editor returned no cmd")
	}
	got := cmdMsg(t, ecmd)
	if want := (AnalyzeCondSetValueMsg{Index: 0, Value: "999"}); got != want {
		t.Errorf("value editor commit = %#v, want %#v", got, want)
	}
	if a.ClaimsKeyboard() {
		t.Error("commit must close the editor")
	}

	// A row with no Field edits FIELD first (esc-cancelled here).
	st := matchState()
	st.Conds = append(st.Conds, AnalyzeCond{Side: "req", When: "equals"})
	b := analyzePage(t, st, 120, 32)
	_, _ = b.Update(press('j'))
	_, _ = b.Update(press('j')) // cursor onto the blank row (clamped ok)
	_, _ = b.Update(press('e'))
	for _, c := range "55.1" {
		_, _ = b.Update(press(c))
	}
	_, cmd := b.Update(special(tea.KeyEscape)) // esc cancels without a message
	_ = cmd
	if b.ClaimsKeyboard() {
		t.Error("esc must close the editor")
	}
}

func TestAnalyzeMatchingGroupPane(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, matchState(), 120, 32)

	// g opens the group-by pane over the conds table.
	_, _ = a.Update(press('g'))
	body := ansi.Strip(a.View().Content)
	if !strings.Contains(body, "GROUP BY") || !strings.Contains(body, "39") {
		t.Errorf("group pane body lacks the variance list:\n%s", body)
	}
	if !a.ClaimsKeyboard() {
		t.Error("the open group pane must claim the keyboard")
	}

	// space toggles the cursor's field toward the root.
	got := cmdMsg(t, keyMsg(t, a, ' '))
	if want := (AnalyzeGroupToggleMsg{Field: "4", Side: "req"}); got != want {
		t.Errorf("group toggle = %#v, want %#v", got, want)
	}

	// esc closes the pane locally (no message; conds table is back).
	_, cmd := a.Update(special(tea.KeyEscape))
	if cmd != nil {
		t.Errorf("group pane esc leaked a message: %T", cmd)
	}
	body = ansi.Strip(a.View().Content)
	if strings.Contains(body, "GROUP BY") {
		t.Errorf("pane stayed open:\n%s", body)
	}
}

func TestAnalyzeMatchingBody(t *testing.T) {
	t.Parallel()

	st := matchState()
	st.Conds = append(st.Conds, AnalyzeCond{Side: "resp", Field: "39", When: "equals", Value: "05"})
	st.MatchWarn = "card/track data will be matched - captured PANs are anonymized"
	a := analyzePage(t, st, 120, 32)
	body := ansi.Strip(a.View().Content)

	for _, want := range []string{"MATCHING", "0200", "000000", "resp", "~42 pairs match", "39", "anonymized"} {
		if !strings.Contains(body, want) {
			t.Errorf("matching body lacks %q:\n%s", want, body)
		}
	}

	// Enter on the matching step advances (AnalyzeNextMsg).
	_, ecmd := a.Update(special(tea.KeyEnter))
	if ecmd == nil {
		t.Fatal("enter on the matching step returned no cmd")
	}
	if got := cmdMsg(t, ecmd); got != (AnalyzeNextMsg{}) {
		t.Errorf("enter = %#v, want AnalyzeNextMsg", got)
	}
}
