// root_send_history_test.go pins what a mid-flight failure must leave
// behind (UAT observation #3): a run that died in Receive still describes
// its composed request, so the §D panes and the send-history detail
// render the field tree instead of empty lines.
package tui

import (
	"context"
	"testing"
	"time"

	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
)

// TestSendHistoryTimedOutCarriesRequest: the timeout closes the run, the
// ring entry carries the request tree, and opening the detail from the
// history shows request rows with an honest empty response.
func TestSendHistoryTimedOutCarriesRequest(t *testing.T) {
	r := newSendTestRoot(t)
	config.GetConfig().SetResponseTimeout(80 * time.Millisecond)
	r.m.liveConnect = func(context.Context) error { return nil }
	r.m.liveSend = func(ctx context.Context, _ string) (*liveExchange, error) {
		<-ctx.Done()

		return &liveExchange{Request: cannedRequest(t, r.spec()), Wrote: true}, ctx.Err()
	}

	_, _ = r.m.Update(pages.TxSendMsg{ID: "Purchase"})
	for i := 0; i < 3; i++ {
		sm := r.nextStage(t)
		stopped := sm.Stage == 2 && !sm.OK
		r.pump(t, sm)
		if stopped {
			break
		}
	}
	r.advance(3200 * time.Millisecond)

	if len(r.m.sends) != 1 {
		t.Fatalf("ring entries = %d, want 1", len(r.m.sends))
	}
	if len(r.m.sends[0].State.Request) == 0 {
		t.Fatal("the ring entry must carry the composed request's field tree")
	}

	_, _ = r.m.Update(pages.SendPopMsg{})
	_, _ = r.m.Update(palette.SendHistoryMsg{})
	_, _ = r.m.Update(pages.SendHistoryPickMsg{Index: 0})

	st := r.m.send.State()
	if !st.TimedOut {
		t.Errorf("frozen §D state should carry the timeout, got %#v", st)
	}
	if len(st.Request) == 0 {
		t.Error("history detail must render the request field tree")
	}
	if len(st.Response) != 0 {
		t.Error("history detail must not fabricate response rows")
	}
}
