// root_scenario_detail.go owns the step message-preview load: the page
// emits ScenarioStepDetailMsg on Enter-on-step; root loads off the UI
// thread, clears the cached preview FIRST (a same-step re-request after
// Esc must see nil→payload, never payload→payload, or the overlay never
// re-arms), bumps the seq, and folds the result in on a later tick under
// the same identity — the fold is the arm, never an optimistic open.
// Run steps preview the engine's captured bytes from the retained report;
// never-run steps the honest ComposeRaw template composition, which still
// advances the persisted STAN counter (a previewed $stan step consumes a
// sequence value the real send will not reuse). Errors fold an honest
// Note; no message is ever fabricated.
package tui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/moov-io/iso8583"

	"jiso/internal/transactions"
	"jiso/internal/tui/pages"
	"jiso/internal/utils"
)

// scenarioDetailState is the step-preview wiring: preview is the payload
// feeding ScenariosState.Preview (cleared first so the clear reaches the
// page as its own pushed state), wait ignores a new request while one is
// in flight, and seq turns straggler loads stale instead of letting them
// fold over newer truth.
type scenarioDetailState struct {
	preview *pages.ScenarioStepPreview
	wait    bool
	seq     uint64
}

// scenarioStepDetailLoadedMsg reports the step-preview load from the load
// goroutine; a stale seq is ignored.
type scenarioStepDetailLoadedMsg struct {
	seq        uint64
	scenarioID string
	stepIndex  int
	request    *pages.TxReviewMessage
	response   *pages.TxReviewMessage
	err        error
	// composed echoes the load's honest marker: the request is a fresh
	// template composition of a never-run step, not a capture.
	composed bool
}

// handleScenarioStepDetail arms one load: ignored while one is in flight,
// preview cleared first (this tick's push; the payload arrives later via
// the loaded msg), seq bumped, then the load runs off the UI thread.
// Everything the goroutine reads (reports, collection, spec) is snapshotted here.
func (m *RootModel) handleScenarioStepDetail(msg pages.ScenarioStepDetailMsg) (tea.Model, tea.Cmd) {
	if m.scenarioDetail.wait {
		m.debug.logf("scenario step detail id=%s step=%d ignored in-flight", msg.ScenarioID, msg.StepIndex)

		return m, nil
	}
	m.scenarioDetail.wait = true
	m.scenarioDetail.preview = nil
	m.scenarioDetail.seq++
	seq := m.scenarioDetail.seq

	var report *transactions.TestReport
	if r := m.scenarioRun; r != nil && r.done && r.id == msg.ScenarioID {
		report = r.report
	}
	if report == nil && m.scenarioLastReport != nil && m.scenarioLastReport.ScenarioName == msg.ScenarioID {
		report = m.scenarioLastReport
	}
	tc := m.scenarioCollection()

	var spec *iso8583.MessageSpec
	if m.app != nil {
		if svc := m.app.Service(); svc != nil {
			spec = svc.GetSpec()
		}
	}
	id, idx := msg.ScenarioID, msg.StepIndex

	return m, func() tea.Msg {
		res := loadScenarioStepMessages(tc, report, spec, id, idx)

		return scenarioStepDetailLoadedMsg{
			seq: seq, scenarioID: id, stepIndex: idx,
			request: res.request, response: res.response, err: res.err,
			composed: res.composed,
		}
	}
}

// applyScenarioStepDetail folds the loaded messages into the preview on
// the later tick. The wait flag clears BEFORE the stale check; the fold
// carries the arm's identity (scenario + step index), so the overlay arms
// from the pushed Preview. An error folds as an honest Note with no messages.
func (m *RootModel) applyScenarioStepDetail(msg scenarioStepDetailLoadedMsg) (tea.Model, tea.Cmd) {
	m.scenarioDetail.wait = false
	if msg.seq != m.scenarioDetail.seq {
		return m, nil // straggler after a newer arm or a fresh run
	}
	p := &pages.ScenarioStepPreview{
		StepIndex:  msg.stepIndex,
		ScenarioID: msg.scenarioID,
		Request:    msg.request,
		Response:   msg.response,
		Composed:   msg.composed,
	}
	if msg.err != nil {
		p.Note = msg.err.Error()
		// The overlay would show an error-only body: the modal makes the
		// whole error readable; the inline note stays for re-reading
		// after the screen closes.
		m.openErrorModal("cannot preview step message", msg.err)
	}
	m.scenarioDetail.preview = p

	return m, nil
}

// scenarioStepMessages is the outcome of one step-preview load: the
// loaded sections (either may be nil = nothing captured) and the honest
// failure note.
type scenarioStepMessages struct {
	request  *pages.TxReviewMessage
	response *pages.TxReviewMessage
	err      error
	// composed marks a request produced by the never-run step's ComposeRaw
	// preview path (honest composition, no capture).
	composed bool
}

// loadScenarioStepMessages loads one step's request/response for the
// preview from retained truth (it never re-runs anything): the captured
// payloads from the retained report, else the step template's honest
// ComposeRaw. A step that captured no payload yields nil sections — the
// page's honest empty hint, never a placeholder.
func loadScenarioStepMessages(
	tc *transactions.TransactionCollection,
	report *transactions.TestReport,
	spec *iso8583.MessageSpec,
	id string,
	idx int,
) scenarioStepMessages {
	if idx < 1 {
		return scenarioStepMessages{err: fmt.Errorf("scenario %q has no step %d", id, idx)}
	}
	if report != nil {
		if idx > len(report.Steps) {
			return scenarioStepMessages{err: fmt.Errorf("scenario %q has no step %d", id, idx)}
		}
		st := report.Steps[idx-1]

		return scenarioStepMessages{
			request:  scenarioReviewMessage(st.RequestPayload, spec),
			response: scenarioReviewMessage(st.ResponsePayload, spec),
		}
	}
	if tc == nil {
		return scenarioStepMessages{err: errors.New(errNoAppWired)}
	}
	scenario, err := tc.GetScenario(id)
	if err != nil {
		return scenarioStepMessages{err: err}
	}
	if idx > len(scenario.Steps) {
		return scenarioStepMessages{err: fmt.Errorf("scenario %q has no step %d", id, idx)}
	}
	step := scenario.Steps[idx-1]
	if step.UseTransactionID == "" {
		return scenarioStepMessages{err: fmt.Errorf("step %d of scenario %q declares no transaction template", idx, id)}
	}
	msg, err := tc.ComposeRaw(step.UseTransactionID)
	if err != nil {
		return scenarioStepMessages{err: err}
	}
	packed, err := msg.Pack()
	if err != nil {
		return scenarioStepMessages{err: fmt.Errorf("template %q does not pack with the loaded spec: %w", step.UseTransactionID, err)}
	}

	return scenarioStepMessages{
		request:  &pages.TxReviewMessage{HEX: utils.HexDump(packed), Describe: describeMessage(msg)},
		composed: true,
	}
}

// scenarioReviewMessage reconstructs one captured payload into a hex dump
// plus describe sections. A scenario payload IS the wire capture, so a
// successful unpack is not labeled "raw hex fallback"; an unpack failure
// keeps the bytes and names the cause. "" payload is nothing captured: nil.
func scenarioReviewMessage(payload string, spec *iso8583.MessageSpec) *pages.TxReviewMessage {
	if payload == "" {
		return nil
	}
	raw := []byte(payload)
	hexDump := utils.HexDump(raw)
	if spec == nil {
		return &pages.TxReviewMessage{
			HEX:         hexDump,
			Describe:    "(no spec loaded - message fields not decoded)",
			RawFallback: true,
			ParseError:  "no spec loaded",
		}
	}
	msg := iso8583.NewMessage(spec)
	if err := msg.Unpack(raw); err != nil {
		return &pages.TxReviewMessage{
			HEX:         hexDump,
			Describe:    fmt.Sprintf("(RAW HEX fallback message - could not unpack with spec: %v)", err),
			RawFallback: true,
			ParseError:  err.Error(),
		}
	}

	return &pages.TxReviewMessage{HEX: hexDump, Describe: describeMessage(msg)}
}

// describeMessage renders the DoNotFilterFields describe output (the whole
// captured message); a describe failure keeps the partial output.
func describeMessage(msg *iso8583.Message) string {
	var buf bytes.Buffer
	if err := utils.Describe(msg, &buf, iso8583.DoNotFilterFields()...); err != nil {
		out := strings.TrimRight(buf.String(), "\n")
		if out == "" {
			return fmt.Sprintf("(describe failed: %v)", err)
		}

		return out
	}

	return strings.TrimRight(buf.String(), "\n")
}
