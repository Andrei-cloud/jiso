// server_golden_test.go pins the §G page at the baseline
// 120x32 for the three canonical states — running stats, stopped
// start-form hint, and the stop-confirm overlay composed over the
// running body (the root's widgets.ConfirmDialog composition) — in
// both glyph/colour modes (; same harness as the §A/§F goldens).
package pages

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/widgets"
)

func TestServerGoldens(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		prof colorprofile.Profile
	}{
		{"truecolor", colorprofile.TrueColor},
		{"ascii", colorprofile.ASCII},
	}

	for _, p := range profiles {
		t.Run("server_running_"+p.name, func(t *testing.T) {
			th := testTheme(t, p.prof)
			page := NewServer(th)
			page.SetState(serverRunningState())
			_, _ = page.Update(windowSize(120, 32))
			checkGolden(t, "server_running_"+p.name, page.View().Content)
		})

		t.Run("server_stopped_"+p.name, func(t *testing.T) {
			th := testTheme(t, p.prof)
			page := NewServer(th)
			page.SetState(serverStoppedState())
			_, _ = page.Update(windowSize(120, 32))
			checkGolden(t, "server_stopped_"+p.name, page.View().Content)
		})

		t.Run("server_log_"+p.name, func(t *testing.T) {
			th := testTheme(t, p.prof)
			page := NewServer(th)
			st := serverRunningState()
			st.Log = serverLogFixture()
			page.SetState(st)
			_, _ = page.Update(windowSize(120, 32))
			checkGolden(t, "server_log_"+p.name, page.View().Content)
		})

		t.Run("server_log150_"+p.name, func(t *testing.T) {
			th := testTheme(t, p.prof)
			page := NewServer(th)
			st := serverRunningState()
			st.Log = serverLogFixture()
			page.SetState(st)
			_, _ = page.Update(windowSize(160, 40))
			checkGolden(t, "server_log150_"+p.name, page.View().Content)
		})

		t.Run("server_log80_"+p.name, func(t *testing.T) {
			th := testTheme(t, p.prof)
			page := NewServer(th)
			st := serverRunningState()
			st.Log = serverLogFixture()
			page.SetState(st)
			_, _ = page.Update(windowSize(80, 32))
			checkGolden(t, "server_log80_"+p.name, page.View().Content)
		})

		t.Run("server_detail_"+p.name, func(t *testing.T) {
			th := testTheme(t, p.prof)
			page := NewServer(th)
			page.SetState(serverRunningState())
			_, _ = page.Update(windowSize(120, 32))
			// The keyboard truth of opening the detail: r focuses the
			// routes pane, enter opens the cursor's row (the first).
			_, _ = page.Update(press('r'))
			_, _ = page.Update(specialCode(tea.KeyEnter))
			checkGolden(t, "server_detail_"+p.name, page.View().Content)
		})

		t.Run("server_confirm_"+p.name, func(t *testing.T) {
			th := testTheme(t, p.prof)
			page := NewServer(th)
			page.SetState(serverRunningState())
			_, _ = page.Update(windowSize(120, 32))
			// The root's stop-confirm composition: the question line over
			// the page body, pending by construction (the decision keys
			// live in the root's footer strip, outside this render).
			confirm := widgets.NewConfirmDialog(th,
				"stop mock server :9999 with 3 live connection(s)?")
			checkGolden(t, "server_confirm_"+p.name,
				confirm.View()+"\n"+page.View().Content)
		})
	}
}

// serverLogFixture is the raw root-stamped output lines (the shapes
// internal/server writes) the SERVER LOG pane compacts for
// display.
func serverLogFixture() []string {
	return []string{
		"09:17:03 [SERVER] 🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)",
		"09:17:04 [SERVER] 🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)",
		"09:18:11 [SERVER] ⚠️ Fallback (No Route Match) for MTI 0200 -> Responding 0210 (RC: 12)",
		"09:18:12 [SERVER] 🔴 Matched Route 'Drop' for MTI 0800 -> Dropping connection",
		"09:18:13 [SERVER] ❌ Error unpacking request payload: short read",
	}
}
