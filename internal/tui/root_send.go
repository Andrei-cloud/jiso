// root_send.go owns the §D live operation: the page stays a
// presentation-only consumer of SendState snapshots while root walks the
// Connect ▸ Send ▸ Receive ▸ Parse ▸ Validate stages in a goroutine and
// feeds every transition back as a root-internal SendStageMsg through the
// program send-func — the same injectable-sender pattern the event bridge
// uses, so no real program is required in tests.
//
// Project rule pinned here: NO auto-retry, ever. A failed stage is final;
// the state stays failed until the user re-sends with Enter (which yields
// the same TxSendMsg path as the §B page). Only one op is in flight at a
// time: a TxSendMsg while one runs is ignored.
package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"

	"jiso/internal/tui/widgets"
)

// sendTickInterval is the in-flight elapsed refresh cadence (~4Hz). The
// page never ticks itself: root reschedules this cmd while the op runs and
// stops once Done, so the timer freezes with the final duration.
const sendTickInterval = 250 * time.Millisecond

// SendStageMsg reports one stage transition from the send goroutine back
// into Update (root-internal; the pages fence keeps it out of pages).
// Stage indexes pages.SendStageNames; OK is the stage verdict; Err
// explains a failure (context.DeadlineExceeded marks the timeout). The
// unexported payloads ride the transition they belong to: ex on the
// Receive-success stage (the raw exchange to parse), parsed on the
// Parse-success stage (rows/RC/correlation for the state).
type SendStageMsg struct {
	Stage int
	OK    bool
	Err   error

	ex     *liveExchange
	parsed *parsedExchange
}

// sendElapsedMsg is the ~4Hz timer tick root re-arms while an op runs; it
// re-stamps the live Elapsed via the injectable clock. gen is the
// generation token of the run the chain was armed for: a tick whose gen
// no longer matches m.sendRun.gen is an orphan of a superseded run and
// is dropped WITHOUT re-arming (single-flight chains, E5-A5-4).
type sendElapsedMsg struct {
	gen uint64
}

// errNoSenderWired closes a live op immediately when the program
// send-func seam is nil (library use without Run): the goroutine's
// emits would drop and wedge the page in-flight forever, so the start
// path applies this terminal failure synchronously instead (E5-A5-7).
var errNoSenderWired = errors.New("no sender wired")

// sendRun is the live-op bookkeeping: the machine truth plus the
// injectable-clock stamps for the (frozen) elapsed timer. nil m.sendRun
// means no op has ever run. gen is the run's generation token (see
// sendElapsedMsg).
type sendRun struct {
	state pages.SendState
	start time.Time
	end   time.Time
	gen   uint64
}

// SetSendSender overrides how send messages reach the program. Run wires
// (*tea.Program).Send; tests inject a collector and re-enter Update by
// hand (the SetEventSender idiom), proving the goroutine→msg→state
// plumbing without a real program.
func (m *RootModel) SetSendSender(send bridge.Sender) { m.sendSender = send }

// responseBudget is the live-send timeout source: the config response
// timeout (flag/env/config-file layered), the exact value App.New passes
// to the service and the CLI send --wait effectively blocks on.
func (m *RootModel) responseBudget() time.Duration {
	if m.app == nil {
		return 0
	}
	if cfg := m.app.Config(); cfg != nil {
		return cfg.GetResponseTimeout()
	}

	return 0
}

// startSend launches (or ignores) a live op for tx id: ignored without an
// app, and ignored while one is in flight (no queue, no retry — the
// project rule). It pushes the §D page at the current size, arms the
// goroutine, and returns the first elapsed tick (stamped with the new
// run's generation). Without a wired sender the run closes synchronously
// with a terminal failure instead of arming a goroutine nobody can hear.
func (m *RootModel) startSend(id string) tea.Cmd {
	started, cmd := m.beginSend(id)
	if started && m.Current().ID() != pages.SendPageID {
		m.Push(m.send)
		_, _ = m.send.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		m.syncSend()
	}

	return cmd
}

// startSendDetached runs the same walk without the §D page jump (UAT
// round 5: a dashboard send keeps the operator on the dashboard, the
// LAST SEND tile carries the outcome).
func (m *RootModel) startSendDetached(id string) tea.Cmd {
	_, cmd := m.beginSend(id)

	return cmd
}

// beginSend prepares and arms the run: ignored without an app, and
// ignored while one is in flight (no queue, no retry — the project
// rule). It never touches the page stack; the callers decide (startSend
// pushes §D, startSendDetached keeps the operator put). It returns the
// first elapsed tick (stamped with the new run's generation); without
// a wired sender the run closes synchronously with a terminal failure
// instead of arming a goroutine nobody can hear.
func (m *RootModel) beginSend(id string) (started bool, cmd tea.Cmd) {
	if m.app == nil || id == "" {
		m.debug.logf("tx send id=%s (no app wired)", id)

		return false, nil
	}
	if m.sendRun != nil && !m.sendRun.state.Done {
		m.debug.logf("tx send id=%s ignored in-flight", id)

		return false, nil
	}

	st := pages.SendState{TxID: id, Target: m.targetDisplay(), Budget: m.responseBudget(), Attempt: 1}
	if info, err := m.app.Transactions().Info(id); err == nil && info.Name != "" {
		st.TxName = info.Name
	} else {
		st.TxName = id
	}
	m.sendGen++
	m.sendRun = &sendRun{state: st, start: m.now(), gen: m.sendGen}
	m.syncSend()

	// No program seam (library use without Run): nothing could report
	// back, so close the run with the terminal failure NOW — arming the
	// goroutine would drop every emit and wedge §D in-flight forever.
	sender := m.sendSender
	if sender == nil {
		m.debug.logf("tx send id=%s closed: %v", id, errNoSenderWired)
		_, _ = m.applySendStage(SendStageMsg{Stage: 0, Err: errNoSenderWired})
		m.syncSend()

		return true, nil
	}

	connect, leg := m.liveConnect, m.liveSend
	if connect == nil {
		connect = m.appConnect
	}
	if leg == nil {
		leg = m.appSend
	}

	parent := context.Background()
	if m.eventCtx != nil {
		parent = m.eventCtx
	}
	ctx, cancel := context.WithTimeout(parent, max(st.Budget, time.Millisecond))

	go m.walkSend(ctx, cancel, sender, connect, leg, id)

	return true, m.sendTickCmd()
}

// walkSend is the stage goroutine: every transition goes to the program
// send-func; the walk stops at the first failed stage (nothing after a
// failed stage ever fires, and nothing retries).
func (m *RootModel) walkSend(
	ctx context.Context,
	cancel context.CancelFunc,
	sender bridge.Sender,
	connect func(context.Context) error,
	leg func(context.Context, string) (*liveExchange, error),
	txName string,
) {
	defer cancel()

	emit := func(msg tea.Msg) {
		if sender != nil {
			sender(msg)
		}
	}

	if err := connect(ctx); err != nil {
		emit(SendStageMsg{Stage: 0, Err: err})

		return
	}
	emit(SendStageMsg{Stage: 0, OK: true})

	ex, err := leg(ctx, txName)
	if ex == nil || !ex.Wrote {
		m.debug.logf("send stage1 failed tx=%q err=%v", txName, err)
		emit(SendStageMsg{Stage: 1, Err: err})

		return
	}
	emit(SendStageMsg{Stage: 1, OK: true})

	if err != nil {
		emit(SendStageMsg{Stage: 2, Err: err})

		return
	}
	emit(SendStageMsg{Stage: 2, OK: true, ex: ex})

	parsed, perr := parseExchange(ex)
	if perr != nil {
		emit(SendStageMsg{Stage: 3, Err: perr})

		return
	}
	emit(SendStageMsg{Stage: 3, OK: true, parsed: parsed})

	// Validate: the same app.ValidateMessage gate the CLI send path runs,
	// surfaced as the explicit final stage the design shows.
	verr := app.ValidateMessage(ex.Request)
	emit(SendStageMsg{Stage: 4, OK: verr == nil, Err: verr})
}

// applySendStage folds one transition into the machine truth: resolved
// stages append to StageOK, parsed payloads land on the state, and a
// failed stage (or the final Validate) closes the run — freezing the
// elapsed timer via the injectable clock.
func (m *RootModel) applySendStage(msg SendStageMsg) (tea.Model, tea.Cmd) {
	r := m.sendRun
	if r == nil {
		return m, nil // straggler after a pop/reset
	}
	st := &r.state

	st.Stage = msg.Stage
	st.StageOK = append(st.StageOK, msg.OK)

	if msg.parsed != nil {
		st.Request = msg.parsed.request
		st.Response = msg.parsed.response
		st.RequestHex = msg.parsed.requestHex
		st.ResponseHex = msg.parsed.responseHex
		st.RC, st.RCLabel, st.RCok = msg.parsed.rc, msg.parsed.rcLabel, msg.parsed.rcOK
		st.CorrelationOK = msg.parsed.correlationOK
	}
	if msg.Stage == 4 {
		st.Validated = msg.OK
	}

	if !msg.OK || msg.Stage == 4 {
		st.Done = true
		if errors.Is(msg.Err, context.DeadlineExceeded) {
			st.TimedOut = true
		}
		r.end = m.now()
		st.Elapsed = r.end.Sub(r.start)
		m.lastSend = st
		// The §A LAST SEND card's time column: the
		// injectable-clock completion stamp, frozen with the run.
		m.lastSendAt = r.end
		m.pushSendHistory(*st, r.end)
		// The failed stage's whole cause rides the error screen (UAT
		// findings 4/8): the page's stage strip is one line, the screen
		// makes the cause readable and scrollable over the frozen page.
		if !msg.OK && msg.Err != nil {
			m.openErrorModal(sendStageFailureTitle(msg.Stage), msg.Err)
		}
	}

	return m, nil
}

// sendStageFailureTitle names a failed stage for the error screen's
// title, in the screen's "cannot ..." voice; stage names come from the
// same five the segmented indicator shows.
func sendStageFailureTitle(stage int) string {
	switch stage {
	case 0:
		return "cannot connect to server"
	case 1:
		return "cannot send transaction"
	case 2:
		return "no response received"
	case 3:
		return "cannot parse response"
	default:
		return "message validation failed"
	}
}

// sendHistoryMax bounds the session send-history ring.
const sendHistoryMax = 50

// pushSendHistory appends the completed run to the bounded ring (oldest
// dropped); the send-history page renders it newest-at-the-bottom.
func (m *RootModel) pushSendHistory(st pages.SendState, at time.Time) {
	m.sends = append(m.sends, pages.SendHistoryEntry{State: st, At: at})
	if len(m.sends) > sendHistoryMax {
		m.sends = m.sends[len(m.sends)-sendHistoryMax:]
	}
}

// openSendHistory pushes the send-history overlay; an
// empty ring toasts instead of opening a page with nothing to show.
func (m *RootModel) openSendHistory() (tea.Model, tea.Cmd) {
	if len(m.sends) == 0 {
		m.pushToast("no send history yet", widgets.ToastInfo)

		return m, nil
	}
	if m.Current().ID() == pages.SendHistoryPageID {
		return m, nil
	}
	m.sendHistory.SetEntries(m.sends)
	m.Push(m.sendHistory)
	_, _ = m.sendHistory.Update(m.innerWS())
	m.debug.logf("send history open entries=%d", len(m.sends))

	return m, nil
}

// sendHistoryDetail freezes §D on a picked history entry (the §D h
// toggle is the detail ↔ hex view).
func (m *RootModel) sendHistoryDetail(msg pages.SendHistoryPickMsg) (tea.Model, tea.Cmd) {
	if msg.Index < 0 || msg.Index >= len(m.sends) {
		return m, nil
	}
	m.Push(m.send)
	_, _ = m.send.Update(m.innerWS())
	m.send.SetState(m.sends[msg.Index].State)

	return m, nil
}

// tickSendElapsed re-stamps the live elapsed timer and re-arms the tick
// while the op runs; once Done no tick is scheduled (the timer stops
// refreshing — tested with a fake clock). A tick carrying a superseded
// generation is an orphan of a replaced run: it is dropped and never
// re-armed, so chains cannot multiply (E5-A5-4).
func (m *RootModel) tickSendElapsed(msg sendElapsedMsg) (tea.Model, tea.Cmd) {
	if m.sendRun == nil || msg.gen != m.sendRun.gen || m.sendRun.state.Done {
		return m, nil
	}

	return m, m.sendTickCmd()
}

// sendTickCmd is the ~4Hz in-flight refresh timer, stamped with the
// current run's generation so a superseded chain self-retires.
func (m *RootModel) sendTickCmd() tea.Cmd {
	if m.sendRun == nil {
		return nil
	}
	gen := m.sendRun.gen

	return tea.Tick(sendTickInterval, func(time.Time) tea.Msg { return sendElapsedMsg{gen: gen} })
}

// syncSend pushes the machine truth into the §D page (Update-wrapper
// placement mirrors syncDashboard/syncTransactions): while in flight the
// elapsed value is re-stamped from the injectable clock; after Done the
// frozen value is pushed unchanged.
func (m *RootModel) syncSend() {
	if m.send == nil || m.sendRun == nil {
		return
	}
	r := m.sendRun
	if !r.state.Done {
		r.state.Elapsed = m.now().Sub(r.start)
	}
	m.send.SetState(r.state)
}

// viewLastSend re-opens §D on the last completed run without starting a
// new op; Enter there resends through the page's normal resend binding.
// Without a completed run it toasts instead of showing an empty screen.
func (m *RootModel) viewLastSend() (tea.Model, tea.Cmd) {
	if m.lastSend == nil {
		m.pushToast("no previous send yet", widgets.ToastInfo)

		return m, nil
	}
	if m.Current().ID() == pages.SendPageID {
		return m, nil
	}
	m.Push(m.send)
	_, _ = m.send.Update(m.innerWS())
	m.send.SetState(*m.lastSend)
	m.debug.logf("last send view tx=%s", m.lastSend.TxID)

	return m, nil
}

// targetDisplay is the §D target label (host:port; dash when unset).
func (m *RootModel) targetDisplay() string {
	if m.app == nil {
		return ""
	}
	cfg := m.app.Config()
	if cfg == nil || cfg.GetHost() == "" || cfg.GetPort() == "" {
		return ""
	}

	return cfg.GetHost() + ":" + cfg.GetPort()
}

// sendTargetReady reports whether a send has everything the §D walk dials
// through: the connection truth the chip shows (latest bus event, app
// snapshot fallback) plus a host:port to dial. A missing element belongs
// to the send wizard's connect-first walk, never to a dial over an
// incomplete address.
func (m *RootModel) sendTargetReady() bool {
	if !m.connectionLive() {
		return false
	}
	cfg := m.configOrNil()

	return cfg != nil && cfg.GetHost() != "" && cfg.GetPort() != ""
}
