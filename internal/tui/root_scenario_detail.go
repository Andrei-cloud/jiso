// root_scenario_detail.go owns the §F step message-preview load (UAT round 9
// F-9e c, task 9.8b): the page emits pages.ScenarioStepDetailMsg on Enter-on-
// step and root loads the step's request/response OFF the UI thread, mirroring
// the §I async-review pattern (handleSessionsReview/applySessionsReview): the
// handler guards the in-flight leg, clears the cached preview FIRST so the
// clear reaches the page as its own pushed state (9.8a Minor 3: the page's
// stepPreviewShownID must see nil→payload across the two Update ticks, never
// payload→payload, or a same-step re-request after Esc never re-arms the
// overlay), bumps the seq, and fires a func() tea.Msg; the apply folds the
// result into ScenariosState.Preview on a LATER tick under the SAME identity
// (scenario + step index), so the fold is the overlay's arm — like §I's
// review, the overlay opens when the result arrives, never optimistically
// (the Loading marker stays reserved for genuinely slow load sources; this
// load reads retained in-process state, so no Loading:true frame is ever
// pushed — the reconciliation of 9.8a's sketched "Loading first" note).
//
// Payload surfacing (the derivation gap this closes): a completed run keeps
// the engine's captured packed payloads in m.scenarioRun.report /
// m.scenarioLastReport (StepResult.RequestPayload/ResponsePayload, raw wire
// bytes; the per-step ScenariosState derivation keeps only Status/Latency/RC/
// Note), so a run step reconstructs from the retained report with the app's
// loaded spec — the same sections `jiso db tx` prints (utils.HexDump +
// DoNotFilterFields describe, per db.Reconstruct's raw-HEX path). A step that
// never ran previews the honest raw composition of its declared template
// (TransactionCollection.ComposeRaw — no dataset row drawn and no sequence
// number consumed, the engine's own base path); the response only ever comes
// from a real capture, so the overlay names the missing reply instead of
// inventing one. Errors fold an honest Note into the preview; no message is
// ever fabricated.
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

// scenarioDetailState is the §F step message-preview wiring (UAT round 9
// F-9e c, task 9.8b): preview is the payload feeding ScenariosState.Preview
// (nil = none — the arm clears it FIRST so the clear reaches the page as its
// own pushed state; 9.8a Minor 3: a same-step re-request after Esc must see
// nil→payload, never payload→payload, or the overlay never re-opens); wait
// marks an in-flight load (a new request while one is in flight is ignored);
// seq is the load's lifecycle token (new arms and new runs bump it so
// stragglers turn stale instead of folding over newer truth — the §I
// sessionsReviewWait/sessionsSeq pattern).
type scenarioDetailState struct {
	preview *pages.ScenarioStepPreview
	wait    bool
	seq     uint64
}

// scenarioStepDetailLoadedMsg reports the step-preview load from the tea.Cmd
// goroutine; seq marks the load generation (a stale seq is ignored — the
// sessionsReviewLoadedMsg lifecycle).
type scenarioStepDetailLoadedMsg struct {
	seq        uint64
	scenarioID string
	stepIndex  int
	request    *pages.TxReviewMessage
	response   *pages.TxReviewMessage
	err        error
}

// handleScenarioStepDetail arms one step-preview load: ignored while one is
// in flight (single detail leg — the §I review rule), preview cleared first
// (the clear is THIS tick's syncPages push; the payload arrives on a later
// tick through the loaded msg), seq bumped, then the load runs off the UI
// thread. Everything the goroutine reads is snapshotted here (the §I src
// pattern): the retained reports, the collection, the loaded spec.
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
		}
	}
}

// applyScenarioStepDetail folds the loaded messages into the preview on the
// later tick. The wait flag clears BEFORE the stale check (the uniform §I
// apply pattern). The fold carries the arm's identity (scenario + step
// index) so the overlay arms from the pushed Preview and a same-identity
// re-push would only refresh the body; an error folds as an honest Note with
// no messages at all.
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
	}
	if msg.err != nil {
		p.Note = msg.err.Error()
	}
	m.scenarioDetail.preview = p

	return m, nil
}

// scenarioStepMessages is the outcome of one step-preview load: the loaded
// request/response sections (either may be nil = nothing captured) and the
// honest failure note. Grouped as a struct so the load's same-typed results
// never read confusingly at the call site.
type scenarioStepMessages struct {
	request  *pages.TxReviewMessage
	response *pages.TxReviewMessage
	err      error
}

// loadScenarioStepMessages loads one step's request/response for the preview
// from the retained truth (it never re-runs anything): the engine-captured
// packed payloads from the retained report when the scenario has completed,
// else the honest raw composition of the never-run step's declared template.
// A step of a completed run that captured no payload (a step that failed
// before packing) yields nil sections — the page's honest empty hint, never
// a placeholder.
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
		request: &pages.TxReviewMessage{HEX: utils.HexDump(packed), Describe: describeMessage(msg)},
	}
}

// scenarioReviewMessage reconstructs one captured packed payload into the §I
// review shape (the same sections db.Reconstruct's raw-HEX path prints:
// utils.HexDump of the bytes plus the DoNotFilterFields describe). A
// successful unpack is NOT labeled "raw hex fallback" — a scenario payload IS
// the wire capture, hex is its native form — while an unpack failure keeps the
// bytes and names the reason (RawFallback + ParseError), so the operator sees
// the bytes and the cause instead of an empty message. "" payload is "nothing
// captured": nil.
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

// describeMessage renders the DoNotFilterFields describe output (the same
// filter set §I's reconstruction uses — the review overlay shows the whole
// message the operator captured), trailing newline trimmed; a describe
// failure keeps the partial output and names the rest.
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
