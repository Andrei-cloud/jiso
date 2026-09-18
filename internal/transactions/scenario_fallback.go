// scenario_fallback.go answers which steps of a scenario would resolve
// their spec through the collection's global spec instead of a spec of
// their own. It is the transactions-side half of the TUI's spec prompt:
// the caller pairs it with whether the global is an explicit user choice
// (config) or the engine default. The result never depends on what the
// global holds — declaring is a property of the steps.
package transactions

// ScenarioFallbackSteps returns the 1-based indexes of the named
// scenario's steps that fall back to the collection's global spec: a step
// naming no transaction template, or one whose template declares no spec
// of its own (a spec_file alias counts as declared; a template the
// collection does not have declares nothing).
func (tc *TransactionCollection) ScenarioFallbackSteps(name string) ([]int, error) {
	scenario, err := tc.GetScenario(name)
	if err != nil {
		return nil, err
	}

	var idx []int
	for i, step := range scenario.Steps {
		if step.UseTransactionID == "" {
			idx = append(idx, i+1)
			continue
		}
		t, err := tc.findTransaction(step.UseTransactionID)
		if err != nil || t.Spec == "" {
			idx = append(idx, i+1)
		}
	}

	return idx, nil
}
