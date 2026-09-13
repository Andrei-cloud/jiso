package tui

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
)

// Loopback-only pprof side channel (TUI-409): $JISO_PROFILE truthy starts a
// net/http/pprof server for the lifetime of the program; the bind host is
// the constant below, never 0.0.0.0 and never configurable — only the port
// is ($JISO_PROFILE_PORT, default 6065). Tests assert listener
// addr.String() starts with "127.".

const (
	pprofHost     = "127.0.0.1"
	pprofPortFull = "6065"
)

// pprofAddr resolves the bind address: loopback host + $JISO_PROFILE_PORT
// when it parses as a TCP port (0 = ephemeral, for tests), else 6065.
func pprofAddr() string {
	if p, err := strconv.Atoi(strings.TrimSpace(os.Getenv(profilePortEnv))); err == nil && p >= 0 && p <= 65535 {
		return net.JoinHostPort(pprofHost, strconv.Itoa(p))
	}

	return net.JoinHostPort(pprofHost, pprofPortFull)
}

// profileServer is a running pprof listener; stop with Stop (run wires it
// to program exit).
type profileServer struct {
	srv *http.Server
	ln  net.Listener
	dbg *debugLogger
}

// maybeStartProfile starts the pprof server when $JISO_PROFILE is truthy
// and reports its bound address. Off, or a bind failure, yields (nil, ""):
// a broken side channel never sinks the TUI, and the failure (if debug is
// on) is in the lifecycle log.
func maybeStartProfile(dbg *debugLogger) (stop func(), addr string) {
	if !envTruthy(os.Getenv(profileEnv)) {
		return nil, ""
	}

	//nolint:noctx // binds a numeric wildcard address, so there is no name to
	// resolve and nothing for a context to cancel; the caller is the TUI process
	// itself, which wants the side channel up or a logged failure, never a cancel.
	ln, err := net.Listen("tcp", pprofAddr())
	if err != nil {
		dbg.logf("profile start error=%v", err)

		return nil, ""
	}

	// Dedicated ServeMux: importing net/http/pprof registers on
	// DefaultServeMux, which the TUI process must not expose.
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	p := &profileServer{srv: srv, ln: ln, dbg: dbg}
	dbg.logf("profile start addr=%s", ln.Addr())

	return p.Stop, ln.Addr().String()
}

// Stop closes the listener and pending connections; safe once.
func (p *profileServer) Stop() {
	_ = p.srv.Close()

	p.dbg.logf("profile stop addr=%s", p.ln.Addr())
}
