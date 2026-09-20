package tui

import (
	"slices"
	"strings"
	"testing"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// TestAnalyzeItemRowsPreviewNumericOrder: the generated-item
// picker's per-item preview must render ISO8583 fields in numeric ascending
// order (0,2,11) — exactly what the file will contain — not Go's default
// map key order (0,11,2).
func TestAnalyzeItemRowsPreviewNumericOrder(t *testing.T) {
	t.Parallel()

	out := &app.AnalyzeOutput{Mode: app.AnalyzeModeTx}
	out.AttachGeneratedItems([]config.Item{{
		Type:   config.TypeTransaction,
		Name:   "Captured Flow 0400",
		Fields: []byte(`{"0":"0400","11":"auto","2":"411111"}`),
	}})

	rows := analyzeItemRows(out)
	if len(rows) != 1 {
		t.Fatalf("roster rows = %d, want 1", len(rows))
	}
	p := rows[0].Preview
	i0 := strings.Index(p, `"0":`)
	i2 := strings.Index(p, `"2":`)
	i11 := strings.Index(p, `"11":`)
	if i0 >= i2 || i2 >= i11 {
		t.Errorf("picker preview field order is not numeric ascending:\n%s", p)
	}
}

// TestAnalyzeItemRowsScenarioLinksAndResponseCode: the picker rows are
// wired for the complete-scenario pick (UAT): a transaction links the
// routes whose match its template satisfies, a scaffolded reversal pairs
// with the request it reverses, and route rows carry the response code
// they answer with in the roster's RC column.
func TestAnalyzeItemRowsScenarioLinksAndResponseCode(t *testing.T) {
	t.Parallel()

	out := &app.AnalyzeOutput{Mode: "scenario", ScenarioName: "Captured"}
	out.AttachGeneratedItems([]config.Item{
		{Type: config.TypeTransaction, Name: "Tx 0100 DE3=000000 #1",
			Fields: []byte(`{"0":"0100","3":"000000","11":"000001"}`)},
		{Type: config.TypeTransaction, Name: "Reversal for 0100 DE3=000000 #1",
			Fields: []byte(`{"0":"0400","3":"000000","11":"000002"}`)},
		{Type: config.TypeMockRoute, Name: "Mock Route #0001 0110 DE3=000000 RC=51",
			MatchFields:    map[string]any{"0": "0100", "3": "000000"},
			ResponseFields: map[string]any{"39": "51"}},
		{Type: config.TypeMockRoute, Name: "Mock Reversal Route #0001 0400 DE3=000000",
			MatchFields:    map[string]any{"0": "0400", "3": "000000"},
			ResponseFields: map[string]any{"39": "00"}},
		{Type: config.TypeScenario, Name: "Captured", Steps: []byte(`[]`)},
	})

	rows := analyzeItemRows(out)
	byName := map[string]pages.AnalyzeItemRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	tx := byName["Tx 0100 DE3=000000 #1"]
	rev := byName["Reversal for 0100 DE3=000000 #1"]
	route := byName["Mock Route #0001 0110 DE3=000000 RC=51"]
	revRoute := byName["Mock Reversal Route #0001 0400 DE3=000000"]

	if route.RC != "51" || revRoute.RC != "00" {
		t.Errorf("route RC = %q / %q, want 51 / 00", route.RC, revRoute.RC)
	}
	if tx.RC != "" {
		t.Errorf("a transaction row carries no response code, got %q", tx.RC)
	}
	if !slices.Contains(rev.Links, tx.Key) || !slices.Contains(tx.Links, rev.Key) {
		t.Errorf("the reversal template must pair with the request it reverses: %v / %v", tx.Links, rev.Links)
	}
	if !slices.Contains(route.Links, tx.Key) || !slices.Contains(tx.Links, route.Key) {
		t.Errorf("the response route must link the transaction it answers: %v / %v", tx.Links, route.Links)
	}
	if !slices.Contains(revRoute.Links, rev.Key) {
		t.Errorf("the reversal route must link the reversal template: %v", revRoute.Links)
	}
	if slices.Contains(revRoute.Links, tx.Key) {
		t.Error("a 0400-match route must not link the 0100 request template")
	}
	// Every piece pulls the scenario item (its steps scope to the
	// written transactions at write time); the scenario pulls nothing.
	scen := byName["Captured"]
	if len(scen.Links) != 0 {
		t.Errorf("the scenario item must pull no pieces, got %v", scen.Links)
	}
	for name, row := range map[string]pages.AnalyzeItemRow{"tx": tx, "reversal": rev, "route": route, "reversal route": revRoute} {
		if !slices.Contains(row.Links, scen.Key) {
			t.Errorf("%s must pull the scenario item on select, links: %v", name, row.Links)
		}
	}
}
