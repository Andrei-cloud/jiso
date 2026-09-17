package pages

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

func TestCompactServerLog(t *testing.T) {
	t.Parallel()

	th := testTheme(t, colorprofile.TrueColor)

	cases := []struct{ raw, want string }{
		{
			"09:17:03 [SERVER] 🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)",
			"09:17:03 🟢 Echo · 0800→0810 · RC 00",
		},
		{
			"09:18:11 [SERVER] ⚠️ Fallback (No Route Match) for MTI 0200 -> Responding 0210 (RC: 12)",
			"09:18:11 ⚠️ Fallback · 0200→0210 · RC 12",
		},
		{
			"09:19:02 [SERVER] 🔴 Matched Route 'Drop' for MTI 0800 -> Dropping connection",
			"09:19:02 🔴 Drop · 0800→ drop_conn",
		},
		{
			"09:20:00 [SERVER] ❌ Error unpacking request payload: short read",
			"09:20:00 ❌ Error unpacking request payload: short read",
		},
		{
			// Unrecognized shapes pass through verbatim.
			"09:21:00 something else entirely",
			"09:21:00 something else entirely",
		},
	}
	for _, c := range cases {
		if got := CompactServerLog(th, c.raw); got != c.want {
			t.Errorf("CompactServerLog:\n got %q\nwant %q", got, c.want)
		}
	}
}

// TestCompactServerLogAscii pins the 7-bit fallback for the ascii
// profile (goldens and the ascii contract).
func TestCompactServerLogAscii(t *testing.T) {
	t.Parallel()

	th := testTheme(t, colorprofile.ASCII)

	raw := "09:17:03 [SERVER] 🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)"
	got := CompactServerLog(th, raw)
	if strings.ContainsAny(got, "🟢·→") {
		t.Errorf("ascii compaction leaked non-7bit: %q", got)
	}
	for _, want := range []string{"ok", "Echo", "0800->0810", "RC 00", "09:17:03"} {
		if !strings.Contains(got, want) {
			t.Errorf("ascii compaction lacks %q: %q", want, got)
		}
	}
}

// TestServerLogScrollWindow pins the §4 log scroll keys: newest at the
// bottom by default, k scrolls toward older lines, G/end resumes
// following.
func TestServerLogScrollWindow(t *testing.T) {
	t.Parallel()

	st := serverRunningState()
	for i := range 40 {
		st.Log = append(st.Log, "09:17:03 [SERVER] 🟢 Matched Route 'Echo"+strconv.Itoa(i)+
			"' for MTI 0800 -> Responding 0810 (RC: 00)")
	}
	s := serverPage(t, st, 160, 40)

	body := strings.Join(bodyLinesOf(t, s), "\n")
	if !strings.Contains(body, "Echo39") {
		t.Fatal("newest line must be visible when following")
	}

	for range 8 {
		_, _ = s.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	}
	body = strings.Join(bodyLinesOf(t, s), "\n")
	if strings.Contains(body, "Echo39") {
		t.Error("k must scroll away from the newest line")
	}
	if !strings.Contains(body, "Echo31") {
		t.Errorf("scrolled window must show older lines (Echo31):\n%s", body)
	}

	_, _ = s.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	body = strings.Join(bodyLinesOf(t, s), "\n")
	if !strings.Contains(body, "Echo39") {
		t.Error("G must resume following the newest line")
	}
}
