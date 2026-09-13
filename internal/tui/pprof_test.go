package tui

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

// TestProfileBindsLoopbackOnly: $JISO_PROFILE=1 with an ephemeral port —
// the bound listener address must start with "127." (never 0.0.0.0), the
// pprof index must answer, and Stop must be wired for program exit.
func TestProfileBindsLoopbackOnly(t *testing.T) {
	t.Setenv(profileEnv, "1")
	t.Setenv(profilePortEnv, "0")

	stop, addr := maybeStartProfile(nil)
	if stop == nil || addr == "" {
		t.Fatal("JISO_PROFILE=1 did not start the pprof server")
	}

	defer stop()

	if !strings.HasPrefix(addr, "127.") {
		t.Errorf("pprof bound %q, want a 127.* loopback address", addr)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}

	if host != "127.0.0.1" {
		t.Errorf("bind host = %q, want 127.0.0.1", host)
	}

	if port == "0" {
		t.Error("JISO_PROFILE_PORT=0 must resolve to an ephemeral port, stayed 0")
	}

	resp, err := http.Get("http://" + addr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("GET /debug/pprof/ on %s: %v", addr, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /debug/pprof/ status = %d, want 200", resp.StatusCode)
	}
}

// TestProfileAddrIsLoopbackConstantAndPortResolves: the host is a compile
// constant (import-scan equivalent: no env can widen the bind), and the
// port layer defaults to 6065 for unset/blank/unparsable/out-of-range.
func TestProfileAddrIsLoopbackConstantAndPortResolves(t *testing.T) {
	if pprofHost != "127.0.0.1" {
		t.Fatalf("pprofHost = %q, must stay 127.0.0.1", pprofHost)
	}

	cases := []struct {
		port string
		want string
	}{
		{"", "127.0.0.1:6065"},
		{"7777", "127.0.0.1:7777"},
		{"not-a-port", "127.0.0.1:6065"},
		{"70000", "127.0.0.1:6065"},
	}

	for _, tc := range cases {
		t.Setenv(profilePortEnv, tc.port)

		if got := pprofAddr(); got != tc.want {
			t.Errorf("pprofAddr with JISO_PROFILE_PORT=%q = %q, want %q", tc.port, got, tc.want)
		}
	}
}

// TestProfileOffNoListener: unset $JISO_PROFILE starts nothing.
func TestProfileOffNoListener(t *testing.T) {
	t.Setenv(profileEnv, "")
	t.Setenv(profilePortEnv, "")

	stop, addr := maybeStartProfile(nil)
	if stop != nil || addr != "" {
		t.Fatalf("profile off still started: stop=%v addr=%q", stop != nil, addr)
	}
}

// TestProfileLogsLifecycleWhenDebugOn: the pprof milestones land in the
// same lifecycle log as the rest of the milestones.
func TestProfileLogsLifecycleWhenDebugOn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(debugEnv, "1")
	t.Setenv("JISO_STATE_DIR", dir)
	t.Setenv(profileEnv, "1")
	t.Setenv(profilePortEnv, "0")

	dbg := newDebugLogger()
	if dbg == nil {
		t.Fatal("debug logger off with JISO_DEBUG=1")
	}

	stop, addr := maybeStartProfile(dbg)
	if stop == nil {
		t.Fatal("pprof did not start with JISO_PROFILE=1")
	}

	stop()

	dbg.close()

	lines := readDebugLog(t, dir)
	wantDebugLine(t, lines, "profile start addr=127.0.0.1:")
	wantDebugLine(t, lines, "profile stop addr=127.0.0.1:")
	if !strings.Contains(strings.Join(lines, "\n"), "addr="+addr) {
		t.Errorf("profile start line does not name the bound addr %q:\n%s", addr, strings.Join(lines, "\n"))
	}
}
