// root_send_error_modal_test.go pins the UAT findings 4/8 send leg: a
// failed stage opens the error screen with the whole cause, not only the
// page's one-line strip.
package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"jiso/internal/tui/pages"
)

// TestSendConnectFailOpensErrorScreen: stage 0 failing opens the error
// screen and the screen carries the dial cause verbatim.
func TestSendConnectFailOpensErrorScreen(t *testing.T) {
	r := newSendTestRoot(t)
	dial := errors.New("dial tcp: refused")
	r.m.liveConnect = func(context.Context) error { return dial }
	r.m.liveSend = func(context.Context, string) (*liveExchange, error) {
		t.Error("send leg ran after a failed connect")

		return nil, nil
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	sm := r.nextStage(t)
	r.pump(t, sm)

	if r.m.errModal == nil {
		t.Fatal("failed connect stage opened no error screen")
	}
	if !strings.Contains(strings.Join(r.m.errModal.Lines(), "\n"), "refused") {
		t.Errorf("screen lacks the cause: %v", r.m.errModal.Lines())
	}
}

// TestSendStageFailureTitles: the screen titles name the failed stage in
// the screen's "cannot ..." voice, one honest title per stage.
func TestSendStageFailureTitles(t *testing.T) {
	t.Parallel()

	want := []string{
		"cannot connect to server",
		"cannot send transaction",
		"no response received",
		"cannot parse response",
		"message validation failed",
	}
	for stage, title := range want {
		if got := sendStageFailureTitle(stage); got != title {
			t.Errorf("stage %d title = %q, want %q", stage, got, title)
		}
	}
}
