// root_scenario_detail_test.go pins the §F step message-preview load:
// Enter-on-step arms one async load, the run-step payload comes from the
// retained report, a never-run step previews its template composition, a
// same-step re-request after Esc re-arms, an in-flight request ignores
// duplicates, and an error folds an honest note — never a fabricated message.
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/moov-io/iso8583"

	"jiso/internal/transactions"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
)

// scenPackFor packs a minimal MTI (+ optional field 39) message with the
// fixture app's loaded spec.
func scenPackFor(t *testing.T, m *RootModel, mti, rc string) string {
	t.Helper()

	spec := m.app.Service().GetSpec()
	if spec == nil {
		t.Fatal("fixture: the app must load the spec")
	}
	msg := iso8583.NewMessage(spec)
	msg.MTI(mti)
	if rc != "" {
		if err := msg.Field(39, rc); err != nil {
			t.Fatalf("field 39: %v", err)
		}
	}
	packed, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack %s: %v", mti, err)
	}

	return string(packed)
}

// completeScenarioRun drives a completed "E2E Purchase" run whose report carries the captured payloads.
func completeScenarioRun(t *testing.T, m *RootModel, steps []transactions.StepResult) {
	t.Helper()

	col := &scenCollector{msgs: make(chan tea.Msg, 32)}
	m.SetScenarioSender(func(msg tea.Msg) { col.msgs <- msg })
	m.runScenario = fakeScenarioEngine(nil,
		&transactions.TestReport{ScenarioName: "E2E Purchase", Success: true, Steps: steps}, nil)
	_, _ = m.Update(pages.ScenarioRunMsg{ID: "E2E Purchase"})
	for _, msg := range col.nextAll(t) {
		_, _ = m.Update(msg)
	}
}

// Enter on a step of a completed run arms one async load; the arm clears
// the cached preview as its own pushed state, ignores requests while in
// flight, and the fold lands the real captured payloads.
func TestRootScenarioStepDetailLoadsRunPayload(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	req := scenPackFor(t, m, "0800", "")
	resp := scenPackFor(t, m, "0810", "00")
	completeScenarioRun(t, m, []transactions.StepResult{
		{StepName: "Sign On Step", Success: true, RequestPayload: req, ResponsePayload: resp},
		{StepName: "Purchase Step", Success: true},
	})

	_, cmd := m.Update(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "E2E Purchase"})
	if cmd == nil {
		t.Fatal("enter on a step must arm the detail load")
	}
	if m.scenarioDetail.preview != nil {
		t.Fatal("the arm must clear the cached preview so the clear reaches the page as its own push")
	}
	if m.scenarios.StepPreviewOpen() {
		t.Fatal("the overlay opens when the payload arrives, not optimistically on arm")
	}

	_, cmd2 := m.Update(pages.ScenarioStepDetailMsg{StepIndex: 2, ScenarioID: "E2E Purchase"})
	if cmd2 != nil {
		t.Fatal("a detail request while one is in flight must be ignored (no double-load)")
	}

	loaded := mustCmd[scenarioStepDetailLoadedMsg](t, cmd)
	if loaded.err != nil {
		t.Fatalf("load = %v", loaded.err)
	}
	if loaded.request == nil || loaded.response == nil {
		t.Fatalf("loaded = %+v, want both captured payloads", loaded)
	}
	if _, cmd := m.Update(loaded); cmd != nil {
		t.Fatalf("fold ran %v, want nil", cmd())
	}

	p := m.scenarioDetail.preview
	if p == nil || p.StepIndex != 1 || p.ScenarioID != "E2E Purchase" || p.Loading {
		t.Fatalf("preview = %+v", p)
	}
	if p.Request == nil || p.Request.HEX == "" || !strings.Contains(p.Request.Describe, "0800") {
		t.Errorf("request section = %+v, want the captured 0800 payload", p.Request)
	}
	if p.Response == nil || !strings.Contains(p.Response.Describe, "0810") ||
		!strings.Contains(p.Response.Describe, "Response Code") {
		t.Errorf("response section = %+v, want the captured 0810 payload", p.Response)
	}
	if !m.scenarios.StepPreviewOpen() {
		t.Fatal("the fold must arm the overlay")
	}
	if p.Composed {
		t.Error("a captured run payload must not be marked composed (UAT round 9 F1)")
	}
	content := m.View().Content
	for _, want := range []string{"STEP 1", "REQUEST HEX", "RESPONSE HEX"} {
		if !strings.Contains(content, want) {
			t.Errorf("final frame lacks %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "request composed from template") {
		t.Errorf("a captured payload must not carry the composed label:\n%s", content)
	}
}

// after Esc, re-requesting the SAME step re-opens the overlay — only works
// because the arm's nil clear reaches the page as its own pushed state.
func TestRootScenarioStepDetailReArmsAfterEsc(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	req := scenPackFor(t, m, "0800", "")
	resp := scenPackFor(t, m, "0810", "00")
	completeScenarioRun(t, m, []transactions.StepResult{
		{StepName: "Sign On Step", Success: true, RequestPayload: req, ResponsePayload: resp},
	})

	_, cmd := m.Update(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "E2E Purchase"})
	_, _ = m.Update(mustCmd[scenarioStepDetailLoadedMsg](t, cmd))
	if !m.scenarios.StepPreviewOpen() {
		t.Fatal("fixture: the fold armed the overlay")
	}

	_, _ = m.Update(special(tea.KeyEscape)) // the overlay eats the first esc
	if m.scenarios.StepPreviewOpen() {
		t.Fatal("esc must close the overlay")
	}

	_, cmd = m.Update(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "E2E Purchase"})
	if cmd == nil {
		t.Fatal("the same-step re-request after esc must arm a fresh load")
	}
	if m.scenarioDetail.preview != nil {
		t.Fatal("the re-arm must clear the preview on its own tick")
	}
	_, _ = m.Update(mustCmd[scenarioStepDetailLoadedMsg](t, cmd))
	if !m.scenarios.StepPreviewOpen() {
		t.Fatal("a same-step re-request after esc must re-arm the overlay")
	}
}

// a never-run step previews its template's honest raw composition — never
// a response, never a fabricated payload; the overlay labels it "not sent yet".
func TestRootScenarioStepDetailPendingComposesRequest(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	_, _ = m.Update(palette.GoToPageMsg{ID: "scenarios"})

	_, cmd := m.Update(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "Decline matrix"})
	if cmd == nil {
		t.Fatal("a never-run step must still arm the compose leg")
	}
	loaded := mustCmd[scenarioStepDetailLoadedMsg](t, cmd)
	if loaded.err != nil {
		t.Fatalf("compose leg = %v", loaded.err)
	}
	if loaded.request == nil {
		t.Fatal("the pending step must preview its template composition")
	}
	if loaded.response != nil {
		t.Fatal("a never-run step has no response — nothing may be fabricated")
	}
	_, _ = m.Update(loaded)

	p := m.scenarioDetail.preview
	if p == nil || p.Request == nil || p.Response != nil || p.Note != "" {
		t.Fatalf("preview = %+v", p)
	}
	if !strings.Contains(p.Request.Describe, "0200") { // the Purchase template MTI
		t.Errorf("composed request = %q", p.Request.Describe)
	}
	if !p.Composed {
		t.Error("a never-run composition must fold Composed:true so the overlay labels it")
	}
	content := m.View().Content
	if !strings.Contains(content, "no response - run the scenario") {
		t.Errorf("the honest no-response section is missing:\n%s", content)
	}
	if !strings.Contains(content, "request composed from template - not sent yet") {
		t.Errorf("the pending preview must label the composed request as not sent:\n%s", content)
	}
}

// a failing load folds an honest note into the preview and clears the in-flight guard.
func TestRootScenarioStepDetailErrorFoldsNote(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	_, _ = m.Update(palette.GoToPageMsg{ID: "scenarios"})

	_, cmd := m.Update(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "nope"})
	if cmd == nil {
		t.Fatal("the detail arm must fire the load even for an unknown scenario")
	}
	loaded := mustCmd[scenarioStepDetailLoadedMsg](t, cmd)
	if loaded.err == nil {
		t.Fatal("an unknown scenario must be an honest error")
	}
	_, _ = m.Update(loaded)

	p := m.scenarioDetail.preview
	if p == nil || p.Note == "" {
		t.Fatalf("the error must fold an honest note into the preview: %+v", p)
	}
	if p.Request != nil || p.Response != nil {
		t.Fatal("the error fold must not fabricate messages")
	}
	if m.scenarioDetail.wait {
		t.Fatal("the wait flag must clear on the error fold")
	}
	if strings.Contains(m.View().Content, "REQUEST HEX") {
		t.Errorf("the error preview must not fake message sections:\n%s", m.View().Content)
	}
}
