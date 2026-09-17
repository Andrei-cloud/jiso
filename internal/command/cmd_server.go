package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/moov-io/iso8583"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/server"
	"jiso/internal/transactions"
)

// ServerCommand manages the embedded ISO8583 mock server from REPL or CLI
type ServerCommand struct {
	srv    *server.Server
	spec   *iso8583.MessageSpec
	routes []config.MockRouteConfig
	tc     transactions.Repository
	// statsStop halts the side-channel snapshot ticker and
	// removes the state/snapshot files on a clean stop.
	statsStop func() error
}

// PrintStats prints the mock server counters, or says the server is stopped.
// The second case is stated out loud rather than left as an absence: this runs
// after a foreground serve, where the operator needs to know whether the server
// never started or ran and served nothing.
func (sc *ServerCommand) PrintStats() {
	if sc.srv == nil || !sc.srv.IsRunning() {
		fmt.Println("Mock server is currently stopped")
		return
	}
	stats := sc.srv.GetStats()
	stats.PrintSummary(sc.srv.GetPort(), sc.srv.GetHeaderType(), sc.srv.ActiveConnections())
}

// DirectServerOptions configures the foreground `jiso serve start` run
// Zero values keep the legacy behavior: block until
// SIGINT/SIGTERM, print the human stats summary, stop cleanly.
type DirectServerOptions struct {
	Port       string
	HeaderType string
	// ReportPath receives the final ServerStats JSON (atomic rename-per-
	// write) after a clean signal stop; empty disables the report.
	ReportPath string
	// JSONStdout keeps stdout empty while the server runs and prints the
	// final ServerStats JSON to Stdout on a clean stop (all logs go to
	// Stderr); the blocking nature is unchanged.
	JSONStdout bool
	// Stdout/Stderr default to the process streams when nil.
	Stdout io.Writer
	Stderr io.Writer
}

// RunDirectServer blocks in direct CLI mode until SIGINT/SIGTERM. A clean
// signal stop stops the server (the side-channel files are removed
// by StopServer), optionally dumps the final stats to ReportPath and — under
// JSONStdout — to stdout, and returns nil so the process exits 0: for a
// foreground server under systemd, being stopped by a signal is success,
// deliberately unlike one-shot commands whose interrupted work exits 130.
func (sc *ServerCommand) RunDirectServer(opts DirectServerOptions) error {
	stdout, stderr := opts.Stdout, opts.Stderr
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	if err := sc.StartServer(opts.Port, opts.HeaderType); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stderr, "Press Ctrl+C to stop the mock server.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh
	signal.Stop(sigCh)

	_, _ = fmt.Fprintf(stderr, "\nreceived %v, stopping mock server...\n", sig)

	// Resolve the real bound port while the listener is alive: an
	// ephemeral "0" start must still be identifiable in the final stats.
	boundPort, portErr := sc.srv.BoundPort()
	if portErr != nil {
		boundPort = sc.srv.GetPort()
	}

	if !opts.JSONStdout {
		sc.PrintStats()
	}

	if err := sc.StopServer(); err != nil {
		return err
	}

	// The traffic tracker keeps its totals after Stop (only Start resets),
	// so the final view is captured post-stop with running=false.
	final := app.NewServerStatsFromServerStats(
		sc.srv.GetStats(), boundPort, sc.srv.GetHeaderType(), false, sc.srv.ActiveConnections(),
	)
	final.PID = os.Getpid()

	if opts.ReportPath != "" {
		if err := app.WriteJSONAtomic(opts.ReportPath, final); err != nil {
			return fmt.Errorf("writing serve stats report: %w", err)
		}

		_, _ = fmt.Fprintf(stderr, "Serve stats report: %s\n", opts.ReportPath)
	}

	if opts.JSONStdout {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")

		if err := enc.Encode(final); err != nil {
			return fmt.Errorf("writing final serve stats to stdout: %w", err)
		}
	}

	return nil
}

// NewServerCommand creates a new ServerCommand instance.
func NewServerCommand(spec *iso8583.MessageSpec, routes []config.MockRouteConfig, tc transactions.Repository) *ServerCommand {
	return &ServerCommand{
		spec:   spec,
		routes: routes,
		tc:     tc,
	}
}

// StartServer starts the embedded mock server on the requested port with chosen header format.
func (sc *ServerCommand) StartServer(port, headerType string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		port = "9999"
	}
	headerType = strings.TrimSpace(headerType)
	if headerType == "" {
		headerType = "binary2"
	}

	if sc.srv != nil && sc.srv.IsRunning() {
		return fmt.Errorf("mock server is already running on port %s", sc.srv.GetPort())
	}

	sc.srv = server.NewServer(sc.spec, sc.routes, headerType)
	if tlsFileCfg := config.GetConfig().GetTLSConfig(); tlsFileCfg != nil && tlsFileCfg.Enabled {
		cryptoTLS, err := tlsFileCfg.BuildServerTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build server TLS configuration: %w", err)
		}
		sc.srv.SetTLSConfig(cryptoTLS)
		_, _ = fmt.Fprintf(os.Stderr, "   ✓ TLS/mTLS server security enabled (ServerName: %s)\n", tlsFileCfg.ServerName)
	}

	if err := sc.srv.Start(port); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "Embedded ISO8583 Mock Server started on port %s (Header: %s) 🟢\n", port, headerType)

	// side-channel: publish the state file and start the stats
	// snapshot refresh so `jiso serve stats` can query this server from
	// another process. A failure here degrades to "no state file" (serve
	// stats will report not-running) but never blocks the server itself.
	cfg := config.GetConfig()
	statePath, stop, err := app.StartServeSideChannel(sc.srv, cfg.GetHost(), cfg.GetDbPath(), sc.routes, app.ServeStatsRefreshInterval)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: serve state file not written: %v\n", err)
	} else {
		sc.statsStop = stop
		_, _ = fmt.Fprintf(os.Stderr, "Serve state file: %s\n", statePath)
	}

	return nil
}

// StopServer stops the embedded mock server.
func (sc *ServerCommand) StopServer() error {
	if sc.srv == nil || !sc.srv.IsRunning() {
		_, _ = fmt.Fprintln(os.Stderr, "Mock server is not running")
		sc.stopSideChannel()

		return nil
	}

	port := sc.srv.GetPort()
	if err := sc.srv.Stop(); err != nil {
		return fmt.Errorf("failed to stop mock server: %w", err)
	}

	// Clean stop retires the side-channel files, so `serve stats`
	// immediately reports "no running server" instead of a stale PID.
	sc.stopSideChannel()

	_, _ = fmt.Fprintf(os.Stderr, "Embedded ISO8583 Mock Server on port %s stopped 🔴\n", port)

	return nil
}

// stopSideChannel halts the snapshot ticker and removes the state files;
// idempotent and safe when the side-channel never started.
func (sc *ServerCommand) stopSideChannel() {
	if sc.statsStop != nil {
		if err := sc.statsStop(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: serve state cleanup failed: %v\n", err)
		}
		sc.statsStop = nil
	}
}
