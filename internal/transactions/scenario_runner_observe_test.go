package transactions

import (
	"os"
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observeFixture writes a two-step scenario (Sign On → Purchase) and
// returns a collection loaded from it. svc is nil, so every step fails
// fast with "connection is offline" — the deterministic offline path the
// observer tests need.
func observeFixture(t *testing.T) *TransactionCollection {
	t.Helper()

	const data = `[
		{"type":"transaction","name":"Sign On","description":"sign on","fields":{"0":"0800"}},
		{"type":"transaction","name":"Purchase","description":"purchase","fields":{"0":"0200"}},
		{"type":"scenario","name":"Two Steps","description":"two","dataset_name":"",
		 "steps":[
			{"name":"Sign On Step","use_transaction_id":"Sign On","extract":{"Session":"38"}},
			{"name":"Purchase Step","use_transaction_id":"Purchase"}
		 ]}
	]`

	path := t.TempDir() + "/tx.json"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	tc, err := NewTransactionCollection(path, iso8583.Spec87)
	require.NoError(t, err)

	return tc
}

// TestRunScenarioObserverOrder: the hook sees started/done pairs in step
// order with 1-based indexes, done events carry the finished result, and
// the report itself is unaffected.
func TestRunScenarioObserverOrder(t *testing.T) {
	t.Parallel()

	tc := observeFixture(t)
	runner := NewScenarioRunner(nil, tc)

	type ev struct {
		index   int
		name    string
		started bool
		hasRes  bool
	}

	var events []ev

	runner.Observe = func(p StepProgress) {
		events = append(events, ev{p.Index, p.StepName, p.Started, p.Result != nil})
	}

	// Results ride the done events and match the final report.
	var done []*StepResult

	runner.Observe = func(p StepProgress) {
		if p.Started {
			events = append(events, ev{p.Index, p.StepName, true, false})
		} else {
			events = append(events, ev{p.Index, p.StepName, false, p.Result != nil})
			done = append(done, p.Result)
		}
	}

	report, err := runner.RunScenario("Two Steps")
	require.NoError(t, err)

	want := []ev{
		{1, "Sign On Step", true, false},
		{1, "Sign On Step", false, true},
		{2, "Purchase Step", true, false},
		{2, "Purchase Step", false, true},
	}
	assert.Equal(t, want, events)
	require.Len(t, done, 2)
	assert.Equal(t, report.Steps[0], *done[0])
	assert.Equal(t, report.Steps[1], *done[1])
	assert.False(t, report.Success)
}

// TestRunScenarioObserverNilUnaffected: no hook = the legacy path
// (the CLI contract): no panic, same report shape.
func TestRunScenarioObserverNilUnaffected(t *testing.T) {
	t.Parallel()

	tc := observeFixture(t)
	runner := NewScenarioRunner(nil, tc)

	report, err := runner.RunScenario("Two Steps")
	require.NoError(t, err)
	require.Len(t, report.Steps, 2)
	assert.Equal(t, "connection is offline", report.Steps[0].Error)
	assert.False(t, report.Success)
}

// TestRunScenarioObserverExtractDiff: extract values are reported only on
// the step that newly wrote them — a step that wrote nothing (or only
// repeated a prior value) yields nil Extracted. The offline path never
// extracts, so the events stay nil honestly.
func TestRunScenarioObserverExtractDiff(t *testing.T) {
	t.Parallel()

	runner := NewScenarioRunner(nil, nil)
	runner.sessionState = map[string]string{}

	seen := map[string]map[string]string{}

	runner.Observe = func(p StepProgress) {
		if !p.Started {
			seen[p.StepName] = p.Extracted
		}
	}

	before := runner.snapshotSession()
	runner.sessionState["AuthId"] = "482913"
	seen["manual-new"] = runner.extractSince(before)

	before2 := runner.snapshotSession()
	seen["manual-same"] = runner.extractSince(before2)

	runner.sessionState["AuthId"] = "999"
	seen["manual-changed"] = runner.extractSince(before2)

	assert.Equal(t, map[string]string{"AuthId": "482913"}, seen["manual-new"])
	assert.Nil(t, seen["manual-same"])
	assert.Equal(t, map[string]string{"AuthId": "999"}, seen["manual-changed"])
}

// TestRunScenarioObserverUnknownScenario: a missing scenario returns the
// load error and fires no events (nothing ever started).
func TestRunScenarioObserverUnknownScenario(t *testing.T) {
	t.Parallel()

	tc := observeFixture(t)
	runner := NewScenarioRunner(nil, tc)

	fired := 0
	runner.Observe = func(StepProgress) { fired++ }

	_, err := runner.RunScenario("Nope")
	assert.Error(t, err)
	assert.Equal(t, 0, fired)
}
