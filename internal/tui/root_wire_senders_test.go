// root_wire_senders_test.go is the B6 regression: run() installs
// program.Send on EVERY live-operation seam through wireSenders. The
// scenario leg was once left unwired, so walkScenario's emits went
// nowhere, §F wedged at "running" forever, and the single-flight gate
// blocked all later runs (every test injected a collector, so the seam
// itself was untested). This test drives the production seam directly:
// wireSenders with a collector, then a real walkScenario — the step and
// done messages must land in the collector.
package tui

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/transactions"
)

func TestWireSendersWiresAllThreeLiveLegs(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	var mu sync.Mutex

	var got []tea.Msg

	send := func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, msg)
	}

	wireSenders(m, send)

	if m.sendSender == nil || m.connectSender == nil || m.scenarioSender == nil {
		t.Fatalf("wireSenders left a seam nil: send=%v connect=%v scenario=%v",
			m.sendSender != nil, m.connectSender != nil, m.scenarioSender != nil)
	}

	engine := func(name string, observe func(transactions.StepProgress)) (*transactions.TestReport, error) {
		observe(transactions.StepProgress{Index: 1, Started: true})

		return &transactions.TestReport{ScenarioName: name}, nil
	}
	m.walkScenario(m.scenarioSender, engine, "demo")

	mu.Lock()
	defer mu.Unlock()

	var sawStep, sawDone bool

	for _, msg := range got {
		switch msg.(type) {
		case scenarioStepMsg:
			sawStep = true
		case scenarioDoneMsg:
			sawDone = true
		}
	}

	if !sawStep || !sawDone {
		t.Fatalf("scenario emits lost through the run seam: step=%v done=%v (%d msgs)", sawStep, sawDone, len(got))
	}
}
