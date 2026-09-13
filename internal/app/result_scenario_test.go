package app

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"jiso/internal/transactions"
)

func scenarioFixtureTime() time.Time {
	return time.Date(2026, 2, 3, 10, 30, 0, 0, time.UTC)
}

func scenarioFixtures() map[string]*transactions.TestReport {
	start := scenarioFixtureTime()

	return map[string]*transactions.TestReport{
		"success": {
			ScenarioName: "Purchase flow",
			Description:  "auth then capture",
			Success:      true,
			StartTime:    start,
			EndTime:      start.Add(250 * time.Millisecond),
			DurationMs:   250,
			Steps: []transactions.StepResult{
				{StepName: "Purchase", Success: true, LatencyMs: 120, RequestPayload: "req-1", ResponsePayload: "resp-1"},
				{StepName: "Capture", Success: true, LatencyMs: 130},
			},
		},
		"failure": {
			ScenarioName: "Declined flow",
			Success:      false,
			StartTime:    start,
			EndTime:      start.Add(300 * time.Millisecond),
			DurationMs:   300,
			Steps: []transactions.StepResult{
				{StepName: "Purchase", Success: true, LatencyMs: 100},
				{
					StepName:  "Reversal",
					Success:   false,
					LatencyMs: 200,
					Error:     "network send failed: timeout",
					ValidationErrors: []transactions.ValidationError{
						{Field: "39", Expected: "00", Actual: "05", Message: "expected approval"},
					},
				},
			},
		},
	}
}

func TestNewScenarioReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		report      *transactions.TestReport
		wantNil     bool
		wantSuccess bool
		wantTotal   int
		wantPassed  int
		wantFailed  int
		wantLastErr string
	}{
		{
			name:      "nil report yields nil",
			report:    nil,
			wantNil:   true,
			wantTotal: 0,
		},
		{
			name:        "all steps pass",
			report:      scenarioFixtures()["success"],
			wantSuccess: true,
			wantTotal:   2,
			wantPassed:  2,
		},
		{
			name:        "failed step counted",
			report:      scenarioFixtures()["failure"],
			wantSuccess: false,
			wantTotal:   2,
			wantPassed:  1,
			wantFailed:  1,
			wantLastErr: "network send failed: timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewScenarioReport(tt.report)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("NewScenarioReport(nil) = %+v, want nil", got)
				}

				return
			}

			if got.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v", got.Success, tt.wantSuccess)
			}
			if got.TotalSteps != tt.wantTotal || got.PassedSteps != tt.wantPassed || got.FailedSteps != tt.wantFailed {
				t.Errorf("totals = %d/%d/%d, want %d/%d/%d",
					got.TotalSteps, got.PassedSteps, got.FailedSteps, tt.wantTotal, tt.wantPassed, tt.wantFailed)
			}
			if got.Duration != 300*time.Millisecond && tt.name == "failed step counted" {
				t.Errorf("Duration = %v, want 300ms", got.Duration)
			}
			if tt.wantLastErr != "" && got.Steps[1].Error != tt.wantLastErr {
				t.Errorf("last step Error = %q, want %q", got.Steps[1].Error, tt.wantLastErr)
			}
			for i, step := range got.Steps {
				if step.Index != i+1 {
					t.Errorf("step %d Index = %d, want %d", i, step.Index, i+1)
				}
				wantStatus := "passed"
				if !step.Success {
					wantStatus = "failed"
				}
				if step.Status != wantStatus {
					t.Errorf("step %d Status = %q, want %q", i, step.Status, wantStatus)
				}
			}
		})
	}
}

func TestScenarioReportJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		report   *ScenarioReport
		wantKeys []string
	}{
		{
			name:     "success shape",
			report:   NewScenarioReport(scenarioFixtures()["success"]),
			wantKeys: []string{"scenario_name", "start_time", "duration", "passed_steps", "steps"},
		},
		{
			name:     "failure shape with validation errors",
			report:   NewScenarioReport(scenarioFixtures()["failure"]),
			wantKeys: []string{"scenario_name", "failed_steps", "steps", "validation_errors"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.report, tt.wantKeys)
		})
	}
}

// roundTrip marshals v, unmarshals into a fresh zero copy of the same
// type, re-marshals, and asserts byte-identical JSON plus the presence of
// expected snake_case keys.
func roundTrip(t *testing.T, v any, wantKeys []string) {
	t.Helper()

	first, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	back := newSameType(v)
	if err := json.Unmarshal(first, back); err != nil {
		t.Fatalf("unmarshal failed: %v\njson: %s", err, first)
	}

	second, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-marshal failed: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("JSON round-trip differs:\nfirst:  %s\nsecond: %s", first, second)
	}

	var generic map[string]any
	if err := json.Unmarshal(first, &generic); err != nil {
		t.Fatalf("generic unmarshal failed: %v", err)
	}

	flat := flattenKeys(generic)
	for _, key := range wantKeys {
		if !flat[key] {
			t.Errorf("missing json key %q in %s", key, first)
		}
	}
}

// newSameType returns a pointer to a zero value of the pointee type of v.
func newSameType(v any) any {
	return reflect.New(reflect.TypeOf(v).Elem()).Interface()
}

func flattenKeys(m map[string]any) map[string]bool {
	keys := map[string]bool{}

	var walkValue func(any)
	walkValue = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			for k, nested := range value {
				keys[k] = true
				walkValue(nested)
			}
		case []any:
			for _, item := range value {
				walkValue(item)
			}
		}
	}
	walkValue(m)

	return keys
}
