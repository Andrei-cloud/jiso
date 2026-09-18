// scenario_fallback_test.go pins the run/preview spec predicate: which
// steps of a scenario would resolve their spec through the collection's
// global spec — a step naming no transaction template, or one whose
// template declares no spec of its own. The answer is the transactions
// truth and never depends on what the global holds (a set global is an
// explicit choice only where the caller reads the config); every step
// declaring a spec answers empty even under a nil global.
package transactions

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/moov-io/iso8583"
)

// scenarioFallbackFixtureJSON: Echo declares no spec, Signed declares one,
// Aliased declares via the spec_file alias, and four scenarios cover the
// matrix (all declared, mixed, no template, alias).
const scenarioFallbackFixtureJSON = `[
 {"type":"transaction","name":"Echo","description":"e","fields":{"0":"0800"}},
 {"type":"transaction","name":"Signed","description":"s","fields":{"0":"0800"},"spec":"%s"},
 {"type":"transaction","name":"Aliased","description":"a","fields":{"0":"0800"},"spec_file":"other.json"},
 {"type":"scenario","name":"All Declared","description":"1","steps":[
   {"name":"s1","use_transaction_id":"Signed"}]},
 {"type":"scenario","name":"Mixed","description":"2","steps":[
   {"name":"s1","use_transaction_id":"Signed"},
   {"name":"s2","use_transaction_id":"Echo"}]},
 {"type":"scenario","name":"No Template","description":"3","steps":[
   {"name":"s1","fields":{"0":"0800"}}]},
 {"type":"scenario","name":"Alias Declared","description":"4","steps":[
   {"name":"s1","use_transaction_id":"Aliased"}]}
]`

// newFallbackFixture writes the matrix file and builds a collection over
// it with the global spec set or empty.
func newFallbackFixture(t *testing.T, globalSet bool) *TransactionCollection {
	t.Helper()

	t.Setenv("JISO_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "fallback.json")
	body := fmt.Sprintf(scenarioFallbackFixtureJSON, filepath.Join("..", "..", "specs", "flex.json"))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var global *iso8583.MessageSpec
	if globalSet {
		global = iso8583.Spec87
	}
	tc, err := NewTransactionCollection(path, global)
	if err != nil {
		t.Fatalf("build collection: %v", err)
	}

	return tc
}

func TestScenarioFallbackStepsMatrix(t *testing.T) {
	tests := []struct {
		name      string
		scenario  string
		globalSet bool
		want      []int
	}{
		{"all steps declared under an empty global resolve nothing via fallback", "All Declared", false, nil},
		{"all steps declared under a set global", "All Declared", true, nil},
		{"mixed falls back on the specless step", "Mixed", false, []int{2}},
		{"the fallback set is the same under a set global", "Mixed", true, []int{2}},
		{"a step naming no template falls back", "No Template", true, []int{1}},
		{"a spec_file declaration counts as declared", "Alias Declared", false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			coll := newFallbackFixture(t, tc.globalSet)
			got, err := coll.ScenarioFallbackSteps(tc.scenario)
			if err != nil {
				t.Fatalf("ScenarioFallbackSteps: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("fallback steps = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScenarioFallbackStepsUnknownScenario(t *testing.T) {
	coll := newFallbackFixture(t, false)
	if _, err := coll.ScenarioFallbackSteps("nope"); err == nil {
		t.Fatal("an unknown scenario must be an honest error")
	}
}
