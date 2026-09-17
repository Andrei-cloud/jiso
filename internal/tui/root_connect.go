// root_connect.go owns the §E live operation: the dialog is a modal
// overlay (the page stack is never touched, Esc returns to the SAME page)
// and Enter launches the attempt loop as a goroutine reporting back via
// the program send-func. The config has no retry-interval knob, so the
// dialog backs off a fixed 1.5s; there is NO auto-reconnect after the
// final failure — the dialog stays open, error line shown, form editable.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/config"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// defaultConnectAttempts is the attempt-count fallback when no config is
// wired (config validation itself clamps reconnect-attempts to ≥1,
// defaulting to 3).
const defaultConnectAttempts = 3

// defaultConnectBackoff is the fixed wait before each retry attempt: the
// config exposes no retry-interval knob (only connect/response timeouts),
// so the dialog uses the wireframe §E displayed value (backoff 1.5s).
const defaultConnectBackoff = 1500 * time.Millisecond

// ConnectAttemptMsg stamps one attempt start (and the backoff waited
// before it) from the connect goroutine into Update; the dialog's
// in-flight line shows "attempt n/total" + "backoff <d>".
type ConnectAttemptMsg struct {
	Attempt int
	Total   int
	Backoff time.Duration
}

// ConnectResultMsg is the terminal verdict of the attempt loop: OK with
// the connected Target, Err for a final failure (stays open + error line),
// or context.Canceled (Esc; closes silently, no further attempts).
type ConnectResultMsg struct {
	OK     bool
	Err    error
	Target string
}

// connectRun is the in-flight truth; nil m.connectRun means idle.
type connectRun struct {
	cancel  context.CancelFunc
	total   int
	attempt int
	// lengthType is the header framing this attempt asks the app to
	// dial with; on success it becomes the root-tracked effective
	// header the card/chip show.
	lengthType string
}

// connectKeyEsc / connectKeyEnter are the dialog-owned chords the router
// intercepts before the dialog sees any key.
var (
	connectKeyEsc   = key.NewBinding(key.WithKeys(theme.KeyEsc))
	connectKeyEnter = key.NewBinding(key.WithKeys(theme.KeyEnter))
)

// SetConnectSender overrides how connect-loop messages reach the program.
// Run wires (*tea.Program).Send; tests inject a collector and re-enter
// Update by hand (the SetSendSender idiom).
func (m *RootModel) SetConnectSender(send bridge.Sender) { m.connectSender = send }

// openConnect opens (or re-focuses) the §E overlay: the page stack is
// untouched, so Esc — and every page hotkey afterwards — lands back on the
// SAME page. Entry points: the global "c" hotkey, the palette action, and
// the dashboard quick action (all emit/lead here).
func (m *RootModel) openConnect() (tea.Model, tea.Cmd) {
	if m.dlg != nil {
		return m, nil
	}
	th := m.theme
	if th == nil {
		th = theme.Default()
	}
	d := pages.NewConnectDialog(th)
	st := m.buildConnectForm()
	applyConnectRules(&st)
	m.stampTLSNote(&st)
	d.SetState(st)
	if m.width > 0 {
		_, _ = d.Update(m.innerWS())
	}
	m.dlg = d
	m.debug.logf("connect dialog open")

	return m, nil
}

// updateConnectDialog routes one key while the overlay owns the keyboard:
// Esc closes (canceling an in-flight run), but while a field is being
// typed into the first Esc leaves the FIELD instead; Enter starts the
// loop (ignored mid-flight); every other key edits the form and re-stamps.
func (m *RootModel) updateConnectDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The header picker overlay owns Enter/Esc while open:
	// pick/close, never connect/cancel.
	if m.dlg != nil && m.dlg.PickerOpen() {
		_, _ = m.dlg.Update(msg)
		m.syncConnect()

		return m, nil
	}

	switch {
	case key.Matches(msg, connectKeyEsc):
		if m.connectRun == nil && m.dlg.Editing() {
			_, _ = m.dlg.Update(msg) // esc leaves the field before the screen

			return m, nil
		}
		if m.connectRun != nil {
			m.debug.logf("connect canceled attempt=%d/%d", m.connectRun.attempt, m.connectRun.total)
			m.connectRun.cancel()
			m.connectRun = nil
		}
		m.dlg = nil
		m.debug.logf("connect dialog close")

		return m, nil

	case key.Matches(msg, connectKeyEnter):
		if m.connectRun != nil {
			return m, nil
		}
		// Enter on the collapsed picker row opens the overlay
		// instead of starting the action.
		if m.dlg.FocusedIsPicker() {
			m.dlg.OpenPicker()

			return m, nil
		}

		return m.startConnect()

	default:
		_, _ = m.dlg.Update(msg)
		m.syncConnect()

		return m, nil
	}
}

// startConnect snapshots the form into app.ConnectOptions and arms the
// attempt goroutine, flipping the dialog into its in-flight (progress-line
// only) shape. Without an app the honest failure line renders instead.
func (m *RootModel) startConnect() (tea.Model, tea.Cmd) {
	return m.armConnectAttempt(m.dlg.State(), m.dlg)
}

// armConnectAttempt is the shared attempt loop behind both hosts: host is
// the *pages.ConnectDialog that renders the progress line — the standalone
// §E overlay or the send wizard's step-0 form. m.connectHost remembers it
// so both hosts share one goroutine.
func (m *RootModel) armConnectAttempt(st pages.ConnectFormState, host *pages.ConnectDialog) (tea.Model, tea.Cmd) {
	if m.app == nil {
		st.Error = errNoAppWired
		host.SetState(st)

		return m, nil
	}

	if msg := validateConnectPort(&st); msg != "" {
		st.Error = msg
		host.SetState(st)

		return m, nil
	}

	opts := connectOptions(&st, m.app.Config())
	attempts := m.connectAttempts()

	// No program seam (library use without Run): the attempt goroutine's
	// emits would drop and wedge the dialog in-flight forever, so render
	// the terminal failure line now and keep the form editable.
	sender := m.connectSender
	if sender == nil {
		st.Error = errNoSenderWired.Error()
		host.SetState(st)
		m.debug.logf("connect start closed: %v", errNoSenderWired)

		return m, nil
	}

	parent := context.Background()
	if m.eventCtx != nil {
		parent = m.eventCtx
	}
	ctx, cancel := context.WithCancel(parent)
	m.connectRun = &connectRun{cancel: cancel, total: attempts, lengthType: opts.LengthType}
	m.connectHost = host

	st.InFlight = true
	st.Error, st.Progress, st.Backoff = "", "", ""
	host.SetState(st)

	dial := m.dialConnect
	if dial == nil {
		dial = m.appConnectWithOptions
	}
	m.debug.logf("connect start attempts=%d", attempts)
	go m.walkConnect(ctx, cancel, sender, dial, opts, attempts)

	return m, nil
}

// connectHostDlg is the dialog the in-flight stamps belong to: the armed
// host (wizard step 0) or the standalone §E overlay.
func (m *RootModel) connectHostDlg() *pages.ConnectDialog {
	if m.connectHost != nil {
		return m.connectHost
	}

	return m.dlg
}

// walkConnect is the attempt goroutine: dial once per attempt, waiting
// defaultConnectBackoff (injectable) between them; the loop checks the
// context before EVERY attempt, so Esc truly stops further attempts. The
// final failure is emitted once and never re-armed (no auto-reconnect).
func (m *RootModel) walkConnect(
	ctx context.Context,
	cancel context.CancelFunc,
	sender bridge.Sender,
	dial func(context.Context, app.ConnectOptions) error,
	opts app.ConnectOptions,
	attempts int,
) {
	defer cancel()

	emit := func(msg tea.Msg) {
		if sender != nil {
			sender(msg)
		}
	}

	var lastErr error
	for n := 1; n <= attempts; n++ {
		if n > 1 {
			backoff := m.connectBackoffDur(n)
			emit(ConnectAttemptMsg{Attempt: n, Total: attempts, Backoff: backoff})

			select {
			case <-ctx.Done():
				emit(ConnectResultMsg{Err: context.Canceled})

				return
			case <-time.After(backoff):
			}
		} else {
			emit(ConnectAttemptMsg{Attempt: 1, Total: attempts})
		}

		err := dial(ctx, opts)
		if err == nil {
			emit(ConnectResultMsg{OK: true, Target: connectTargetLabel(opts)})

			return
		}
		if ctx.Err() != nil {
			emit(ConnectResultMsg{Err: context.Canceled})

			return
		}
		lastErr = err
	}
	emit(ConnectResultMsg{Err: lastErr})
}

// applyConnectAttempt stamps the in-flight progress line (root-stamped
// attempt counter + backoff text); stragglers after a close are ignored.
func (m *RootModel) applyConnectAttempt(msg ConnectAttemptMsg) (tea.Model, tea.Cmd) {
	host := m.connectHostDlg()
	if host == nil || m.connectRun == nil {
		return m, nil
	}
	st := host.State()
	if !st.InFlight {
		return m, nil
	}
	m.connectRun.attempt = msg.Attempt
	st.Progress = fmt.Sprintf("attempt %d/%d", msg.Attempt, msg.Total)
	st.Backoff = ""
	if msg.Backoff > 0 {
		st.Backoff = "backoff " + msg.Backoff.String()
	}
	host.SetState(st)

	return m, nil
}

// applyConnectResult closes the run: cancel closes the overlay silently,
// success stamps the connection truth (chip + dashboard card update
// through the existing sync paths) and remembers the form values for
// session prefill, failure keeps the dialog open with the error line and
// the form editable — with no further attempt ever armed.
func (m *RootModel) applyConnectResult(msg ConnectResultMsg) (tea.Model, tea.Cmd) {
	if m.connectRun == nil {
		return m, nil // straggler after an Esc
	}
	run := m.connectRun
	m.connectRun = nil
	host := m.connectHostDlg()
	m.connectHost = nil
	if host == nil {
		return m, nil
	}
	standalone := host == m.dlg

	switch {
	case errors.Is(msg.Err, context.Canceled):
		if standalone {
			m.dlg = nil
		} else {
			st := host.State()
			st.InFlight = false
			st.Progress, st.Backoff, st.Error = "", "", ""
			host.SetState(st)
		}
		m.debug.logf("connect dialog close (canceled)")

	case msg.OK:
		now := m.now()
		ev := events.ConnectionEvent{State: events.StateConnected, Detail: msg.Target}
		m.conn, m.connSince = &ev, &now
		// Stamp the framing the live link actually speaks (mirrors
		// the app's own resolution) so the card/chip never lie.
		m.connHeader = effectiveLengthType(run, m.configOrNil())
		s := host.State()
		m.connectSession = &s
		m.rememberLastConn(&s)
		if standalone {
			m.dlg = nil
		} else if m.wizard != nil {
			m.wizard.OnConnected(msg.Target)
		}
		m.debug.logf("connect ok target=%s", msg.Target)

	default:
		st := host.State()
		st.InFlight = false
		st.Progress, st.Backoff = "", ""
		// msg.Err can be nil on a malformed failure msg (OK=false, no
		// cause): render the generic failure line, never dereference.
		if msg.Err != nil {
			st.Error = msg.Err.Error()
			m.debug.logf("connect failed: %v", msg.Err)
		} else {
			st.Error = "connection failed"
			m.debug.logf("connect failed")
		}
		host.SetState(st)
	}

	return m, nil
}

// connectAttempts is the retry-count source: config reconnect-attempts —
// the same value the REPL connect path feeds into the connection manager.
func (m *RootModel) connectAttempts() int {
	if cfg := m.configOrNil(); cfg != nil {
		if n := cfg.GetReconnectAttempts(); n > 0 {
			return n
		}
	}

	return defaultConnectAttempts
}

// connectBackoffDur resolves the wait before attempt n (injectable for
// fast tests; default: the wireframe's fixed 1.5s).
func (m *RootModel) connectBackoffDur(n int) time.Duration {
	if m.connectBackoff != nil {
		return m.connectBackoff(n)
	}

	return defaultConnectBackoff
}

// connectOptions maps the form snapshot to per-attempt app overrides:
// listener mode carries the bind port, caller mode splits target into
// host/port (port falling back to the config when the user typed only a
// host), and the header/station/unsolicited/TLS values ride verbatim.
func connectOptions(st *pages.ConnectFormState, cfg *config.Config) app.ConnectOptions {
	opts := app.ConnectOptions{
		LengthType:         connectFormValue(st, pages.ConnectFieldHeader),
		VisaStationID:      strings.TrimSpace(connectFormValue(st, pages.ConnectFieldStation)),
		ProcessUnsolicited: strings.EqualFold(connectFormValue(st, pages.ConnectFieldUnsolicited), "Yes"),
		TLSConfigPath:      strings.TrimSpace(connectFormValue(st, pages.ConnectFieldTLS)),
	}
	port := strings.TrimSpace(connectFormValue(st, pages.ConnectFieldPort))
	if port == "" && cfg != nil {
		port = cfg.GetPort()
	}
	if connectFormValue(st, pages.ConnectFieldMode) == pages.ConnectModeListener {
		opts.Listener = true
		opts.ListenPort = port

		return opts
	}
	opts.Host = strings.TrimSpace(connectFormValue(st, pages.ConnectFieldIP))
	opts.Port = port

	return opts
}

// validateConnectPort checks the shared port field before dialing/listening:
// one Port field for both modes must be 1-65535. An empty value is legal
// (the config fallback fills it); anything else yields the inline error line.
func validateConnectPort(st *pages.ConnectFormState) string {
	v := strings.TrimSpace(connectFormValue(st, pages.ConnectFieldPort))
	if v == "" {
		return ""
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return "port must be 1-65535"
	}

	return ""
}

// connectTargetLabel is the Connected-event detail for the root-stamped
// success (mirrors App.ConnectWithOptions' published address: the listen
// address for listener mode, host:port for a caller dial).
func connectTargetLabel(opts app.ConnectOptions) string {
	if opts.Listener {
		if opts.ListenPort != "" {
			return "0.0.0.0:" + opts.ListenPort
		}

		return "0.0.0.0"
	}
	if opts.Host == "" {
		return ""
	}
	if opts.Port == "" {
		return opts.Host
	}

	return opts.Host + ":" + opts.Port
}

// appConnectWithOptions is the production dial: App.ConnectWithOptions
// (per-attempt overrides + ConnectionEvent publishing on the bus). The
// dial is synchronous and does not observe ctx today; the parameter
// stays so caller cancellations are passed at the right place.
func (m *RootModel) appConnectWithOptions(_ context.Context, opts app.ConnectOptions) error {
	return m.app.ConnectWithOptions(opts)
}

// effectiveLengthType resolves the header framing a connect attempt
// uses: the per-attempt override, then the configured header, then the
// app fallback -- mirroring app.ConnectWithOptions so the display and
// the dial agree by construction, not by coincidence.
func effectiveLengthType(run *connectRun, cfg *config.Config) string {
	if run != nil {
		if v := strings.TrimSpace(run.lengthType); v != "" {
			return v
		}
	}

	if cfg != nil {
		if v := strings.TrimSpace(cfg.GetHeader()); v != "" {
			return v
		}
	}

	return app.DefaultLengthType
}
