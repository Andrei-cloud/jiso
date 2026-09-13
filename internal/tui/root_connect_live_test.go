// root_connect_live_test.go covers the §E attempt loop's live behaviour:
// the retry progress line, the final-failure shape (dialog stays open,
// error line, form editable, NO further attempts), Esc-cancellation
// (context observed, no post-cancel attempt), session-only prefill, the
// palette entry point, and the root-side TLS note.
package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
)

func TestConnectRetryProgressLine(t *testing.T) {
	r := newConnectTestRoot(t)
	r.m.connectBackoff = func(int) time.Duration { return 2 * time.Millisecond }

	var calls atomic.Int32
	r.m.dialConnect = func(_ context.Context, _ app.ConnectOptions) error {
		if calls.Add(1) < 3 {
			return errors.New("dial refused")
		}

		return nil
	}

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))

	a1, ok := r.next(t).(ConnectAttemptMsg)
	if !ok || a1.Attempt != 1 || a1.Backoff != 0 {
		t.Fatalf("first attempt msg: %+v", a1)
	}
	r.pump(t, a1)
	if st := r.state(t); st.Progress != "attempt 1/3" || st.Backoff != "" {
		t.Fatalf("progress after a1: %+v", st)
	}

	a2, ok := r.next(t).(ConnectAttemptMsg)
	if !ok || a2.Attempt != 2 || a2.Total != 3 || a2.Backoff <= 0 {
		t.Fatalf("retry attempt msg: %+v", a2)
	}
	r.pump(t, a2)
	st := r.state(t)
	if st.Progress != "attempt 2/3" || !strings.HasPrefix(st.Backoff, "backoff ") {
		t.Fatalf("progress after a2: %+v", st)
	}
	if view := r.m.dlg.View(); !strings.Contains(view, "attempt 2/3") || !strings.Contains(view, "backoff") {
		t.Fatalf("in-flight view lacks attempt/backoff line:\n%s", view)
	}

	_, res := r.drainToResult(t)
	if !res.OK || calls.Load() != 3 {
		t.Fatalf("result %+v calls=%d", res, calls.Load())
	}
	if r.m.dlg != nil {
		t.Fatal("dialog must close after the retry succeeds")
	}
	r.wantQuiet(t)
}

func TestConnectFinalFailureStaysOpenNoAutoReconnect(t *testing.T) {
	r := newConnectTestRoot(t)
	r.m.connectBackoff = func(int) time.Duration { return time.Millisecond }

	var calls atomic.Int32
	boom := errors.New("dial tcp: connect: connection refused")
	r.m.dialConnect = func(_ context.Context, _ app.ConnectOptions) error {
		calls.Add(1)

		return boom
	}

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))
	attempts, res := r.drainToResult(t)

	if len(attempts) != 3 || calls.Load() != 3 {
		t.Fatalf("attempts=%d calls=%d, want 3/3 (config reconnect-attempts)", len(attempts), calls.Load())
	}
	for _, a := range attempts[1:] {
		if a.Backoff <= 0 {
			t.Fatalf("retry %d carried no backoff", a.Attempt)
		}
	}
	if res.OK || !errors.Is(res.Err, boom) {
		t.Fatalf("final result: %+v", res)
	}

	st := r.state(t)
	if st.InFlight || st.Error == "" || !strings.Contains(st.Error, "connection refused") {
		t.Fatalf("failure shape: %+v", st)
	}
	// Form editable again: focus still cycles over enabled fields.
	_, _ = r.m.Update(special(tea.KeyTab))
	if f := st.Field(pages.ConnectFieldMode); f == nil {
		t.Fatal("form fields gone after failure")
	}
	if got := r.m.dlg.Focus(); got != 1 {
		t.Fatalf("tab after failure focused %d, want target", got)
	}
	r.wantQuiet(t) // and stays quiet: NO auto-reconnect
}

func TestConnectEscDuringInflightCancels(t *testing.T) {
	r := newConnectTestRoot(t)

	var calls atomic.Int32
	var once sync.Once
	canceled := make(chan struct{})
	r.m.dialConnect = func(ctx context.Context, _ app.ConnectOptions) error {
		calls.Add(1)
		once.Do(func() { close(canceled) })
		<-ctx.Done()

		return ctx.Err()
	}

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))
	a1, ok := r.next(t).(ConnectAttemptMsg)
	if !ok {
		t.Fatalf("want attempt msg, got %T", a1)
	}
	r.pump(t, a1)
	waitSignal(t, canceled)

	_, _ = r.m.Update(special(tea.KeyEscape)) // Esc during in-flight
	if r.m.dlg != nil {
		t.Fatal("esc must close the dialog and cancel the run")
	}

	_, res := r.drainToResult(t)
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("loop must report cancellation, got %+v", res)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1 (no post-cancel attempt)", calls.Load())
	}
	r.wantQuiet(t)
}

func waitSignal(t *testing.T, ch chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("signal never arrived")
	}
}

func TestConnectSessionPrefillAfterSuccess(t *testing.T) {
	r := newConnectTestRoot(t)
	r.m.dialConnect = func(_ context.Context, _ app.ConnectOptions) error { return nil }

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyTab)) // focus ip
	clearField(t, r)
	for _, c := range "10.0.0.9" {
		_, _ = r.m.Update(ch(c))
	}
	if got := r.state(t).Fields[r.m.dlg.Focus()].Value; got != "10.0.0.9" {
		t.Fatalf("typed ip %q", got)
	}
	_, _ = r.m.Update(special(tea.KeyTab)) // focus port
	clearField(t, r)
	for _, c := range "1234" {
		_, _ = r.m.Update(ch(c))
	}
	_, _ = r.m.Update(special(tea.KeyEnter))
	r.drainToResult(t)

	// Config drifts afterwards; the SESSION values must still prefill.
	// (c is now the disconnect key while a connection is live — proposal
	// 04 — so the reopen goes through the root's own open path.)
	r.m.app.Config().SetHost("9.9.9.9")
	_, _ = r.m.openConnect()
	stPrefill := r.state(t)
	if got := stPrefill.Field(pages.ConnectFieldIP).Value; got != "10.0.0.9" {
		t.Fatalf("prefill after success: %q, want the session value", got)
	}
	if got := stPrefill.Field(pages.ConnectFieldPort).Value; got != "1234" {
		t.Fatalf("prefill port after success: %q, want the session value", got)
	}
	// Session-only: the config file was never touched (no save API ran)
	// and the live config still holds its own host.
	if got := r.m.app.Config().GetHost(); got != "9.9.9.9" {
		t.Fatalf("prefill must not rewrite the config: %q", got)
	}
}

// clearField backspaces the focused field to empty.
func clearField(t *testing.T, r *connectTestRoot) {
	t.Helper()

	for {
		st := r.state(t)
		f := st.Fields[r.m.dlg.Focus()]
		if f.Value == "" {
			return
		}
		_, _ = r.m.Update(special(tea.KeyBackspace))
	}
}

func TestConnectEnterIgnoredWhileInFlight(t *testing.T) {
	r := newConnectTestRoot(t)

	var calls atomic.Int32
	var once sync.Once
	entered := make(chan struct{})
	release := make(chan struct{})
	r.m.dialConnect = func(ctx context.Context, _ app.ConnectOptions) error {
		calls.Add(1)
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))
	waitSignal(t, entered)
	_, _ = r.m.Update(special(tea.KeyEnter)) // second enter: ignored
	if got := calls.Load(); got != 1 {
		t.Fatalf("second enter started another op (calls=%d)", got)
	}
	close(release)
	r.drainToResult(t)
}

func TestConnectStragglerResultAfterEscIgnored(t *testing.T) {
	r := newConnectTestRoot(t)

	var once sync.Once
	entered := make(chan struct{})
	r.m.dialConnect = func(ctx context.Context, _ app.ConnectOptions) error {
		once.Do(func() { close(entered) })
		<-ctx.Done()

		return ctx.Err()
	}

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))
	waitSignal(t, entered)
	_, _ = r.m.Update(special(tea.KeyEscape))

	r.pump(t, ConnectResultMsg{OK: true, Target: "x:1"}) // late success
	if r.m.conn != nil {
		t.Fatal("straggler success must not stamp the connection truth")
	}
	r.next(t) // the loop's own cancellation msg drains without effect
}

func TestConnectAttemptsFollowConfigReconnectAttempts(t *testing.T) {
	r := newConnectTestRoot(t)
	r.m.app.Config().SetReconnectAttempts(1)
	r.m.connectBackoff = func(int) time.Duration { return time.Millisecond }

	var calls atomic.Int32
	r.m.dialConnect = func(_ context.Context, _ app.ConnectOptions) error {
		calls.Add(1)

		return errors.New("nope")
	}

	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))
	attempts, res := r.drainToResult(t)
	if len(attempts) != 1 || attempts[0].Total != 1 || calls.Load() != 1 {
		t.Fatalf("attempts=%d calls=%d, want 1 (config reconnect-attempts=1)", len(attempts), calls.Load())
	}
	if res.OK {
		t.Fatal("dial was pinned to fail")
	}
}

func TestConnectNoAppShowsErrorLine(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(ch('c'))
	if m.dlg == nil {
		t.Fatal("dialog must open without an app (form is config-independent)")
	}
	_, _ = m.Update(special(tea.KeyEnter))
	st := m.dlg.State()
	if st.InFlight || st.Error == "" {
		t.Fatalf("no-app enter: %+v", st)
	}
	if m.dlg == nil {
		t.Fatal("dialog must stay open showing the error")
	}
}

func TestConnectPaletteActionOpensSameDialog(t *testing.T) {
	r := newConnectTestRoot(t)

	_, _ = r.m.Update(ch(':'))
	for _, c := range "connect" {
		_, _ = r.m.Update(ch(c))
	}
	_, cmd := r.m.Update(special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("palette enter submitted nothing")
	}
	msg, ok := cmd().(palette.OpenConnectMsg)
	if !ok {
		t.Fatalf("palette action msg %T, want palette.OpenConnectMsg", cmd())
	}
	before := r.m.Current().ID()
	r.pump(t, msg)
	if r.m.dlg == nil {
		t.Fatal("palette action must open the same overlay")
	}
	wantStack(t, r.m, before)
	_, _ = r.m.Update(special(tea.KeyEscape))
	if r.m.dlg != nil {
		t.Fatal("esc must close the palette-opened overlay too")
	}
}

func TestConnectTLSNoteStampedRootSide(t *testing.T) {
	dir := t.TempDir()
	good := dir + "/tls_config.json"
	if err := os.WriteFile(good, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write tls file: %v", err)
	}

	r := newConnectTestRoot(t)
	r.openHotkey(t)
	for range 5 { // mode → ip → port → header → unsolicited → tls
		_, _ = r.m.Update(special(tea.KeyTab))
	}
	if got := r.m.dlg.State().Fields[r.m.dlg.Focus()].Key; got != pages.ConnectFieldTLS {
		t.Fatalf("focus %q, want tls", got)
	}

	type want struct {
		path string
		note string
		kind pages.NoteKind
	}
	for i, w := range []want{
		{good, "loaded", pages.NotePass},
		{dir + "/missing.json", "missing", pages.NoteFail},
		{"", "", pages.NoteNone},
	} {
		clearField(t, r)
		for _, c := range w.path {
			_, _ = r.m.Update(ch(c))
		}
		st := r.state(t)
		f := st.Field(pages.ConnectFieldTLS)
		if f.Note != w.note || f.NoteKind != w.kind {
			t.Fatalf("case %d (%q): note=%q/%v, want %q/%v", i, w.path, f.Note, f.NoteKind, w.note, w.kind)
		}
	}
}

func TestConnectListenerOptionsMapped(t *testing.T) {
	r := newConnectTestRoot(t)

	st := r.m.buildConnectForm()
	setRadio(&st, pages.ConnectFieldMode, pages.ConnectModeListener)
	applyConnectRules(&st)
	if !st.Field(pages.ConnectFieldPort).Enabled {
		t.Fatal("listener mode port must be enabled before mapping")
	}
	st.Field(pages.ConnectFieldPort).Value = "8888"
	setRadio(&st, pages.ConnectFieldHeader, "visa")
	st.Field(pages.ConnectFieldStation).Value = "001234"
	applyConnectRules(&st)
	if !st.Field(pages.ConnectFieldStation).Enabled {
		t.Fatal("station must be enabled for visa before mapping")
	}
	setRadio(&st, pages.ConnectFieldUnsolicited, "Yes")

	opts := connectOptions(&st, r.m.app.Config())
	if !opts.Listener || opts.ListenPort != "8888" {
		t.Fatalf("listener mapping: %+v", opts)
	}
	if opts.LengthType != "visa" || opts.VisaStationID != "001234" || !opts.ProcessUnsolicited {
		t.Fatalf("header/station/unsolicited mapping: %+v", opts)
	}
	if label := connectTargetLabel(opts); label != "0.0.0.0:8888" {
		t.Fatalf("listener target label %q", label)
	}
}
