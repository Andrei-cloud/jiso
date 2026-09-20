package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jiso/internal/config"
	"jiso/internal/transactions"
)

// analyzeview_scenario_write_test.go pins the write-time scenario scoping:
// the exported scenario plans the WRITTEN transactions only.
// TestAnalyzeWriteScenarioScopedToSelection (UAT): the exported scenario
// must cover the SELECTED transactions, never the full capture list.
// Subsetting the roster drops the steps whose transaction is not written
// (reversal steps ride their reversal transaction), a scenario whose
// transactions were all deselected drops from the file entirely, and a
// full selection keeps the scaffolded scenario verbatim.
func TestAnalyzeWriteScenarioScopedToSelection(t *testing.T) {
	t.Parallel()

	a := analyzeApp(t)

	newOut := func(t *testing.T) (*AnalyzeOutput, string) {
		t.Helper()
		outPath := filepath.Join(t.TempDir(), "scoped.json")
		steps, err := json.Marshal([]transactions.ScenarioStep{
			{Name: "s1", UseTransactionID: "Tx 0100 DE3=000000 #1"},
			{Name: "r1", UseTransactionID: "Reversal for 0100 DE3=000000 #1"},
			{Name: "s2", UseTransactionID: "Tx 0100 DE3=000000 #2"},
		})
		if err != nil {
			t.Fatalf("steps: %v", err)
		}

		out := &AnalyzeOutput{Mode: AnalyzeModeScenario, OutputFile: outPath}
		out.AttachGeneratedItems([]config.Item{
			{Type: config.TypeTransaction, Name: "Tx 0100 DE3=000000 #1"},
			{Type: config.TypeTransaction, Name: "Reversal for 0100 DE3=000000 #1"},
			{Type: config.TypeTransaction, Name: "Tx 0100 DE3=000000 #2"},
			{Type: config.TypeMockRoute, Name: "Mock Route #0001 0110 DE3=000000"},
			{Type: config.TypeScenario, Name: "Captured", Steps: steps,
				Description: "Scaffolded test scenario containing 3 step(s) extracted from PCAP"},
		})

		return out, outPath
	}

	keyOf := func(out *AnalyzeOutput, name string) string {
		for _, it := range out.GeneratedItems() {
			if it.Name == name {
				return ItemKey(it)
			}
		}
		t.Fatalf("fixture item %q missing", name)

		return ""
	}

	readScenario := func(t *testing.T, path string) (steps []map[string]any, present bool, desc string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var items []map[string]any
		if err := json.Unmarshal(data, &items); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, it := range items {
			if it["type"] == "scenario" {
				raw, _ := json.Marshal(it["steps"])
				if err := json.Unmarshal(raw, &steps); err != nil {
					t.Fatalf("steps: %v", err)
				}
				d, _ := it["description"].(string)

				return steps, true, d
			}
		}

		return nil, false, ""
	}

	// Subset: drop Tx #2 - its step goes, the purchase and reversal steps
	// ride on.
	out, path := newOut(t)
	out.SetExcluded([]string{keyOf(out, "Tx 0100 DE3=000000 #2")})
	if err := a.WriteAnalyze(context.Background(), out); err != nil {
		t.Fatalf("write: %v", err)
	}
	steps, present, desc := readScenario(t, path)
	if !present {
		t.Fatal("the scenario must survive a subset selection")
	}
	if len(steps) != 2 {
		t.Fatalf("scenario steps = %d, want the 2 written transactions' steps", len(steps))
	}
	if !strings.Contains(desc, "scoped to the written selection") {
		t.Errorf("the scenario must name its scoping: %q", desc)
	}

	// No transactions at all: the scenario plans nothing - it drops from
	// the file (an empty scenario is not a usable extract).
	out, path = newOut(t)
	out.SetExcluded([]string{
		keyOf(out, "Tx 0100 DE3=000000 #1"),
		keyOf(out, "Reversal for 0100 DE3=000000 #1"),
		keyOf(out, "Tx 0100 DE3=000000 #2"),
	})
	if err := a.WriteAnalyze(context.Background(), out); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, present, _ := readScenario(t, path); present {
		t.Error("a scenario whose transactions were all deselected must not be written")
	}

	// Full selection: the scaffolded scenario rides verbatim.
	out, path = newOut(t)
	if err := a.WriteAnalyze(context.Background(), out); err != nil {
		t.Fatalf("write: %v", err)
	}
	steps, present, desc = readScenario(t, path)
	if !present || len(steps) != 3 {
		t.Fatalf("full selection: scenario present=%v steps=%d, want 3", present, len(steps))
	}
	if !strings.HasPrefix(desc, "Scaffolded test scenario containing 3 step(s)") {
		t.Errorf("the full-selection scenario must keep its description, got %q", desc)
	}
}
