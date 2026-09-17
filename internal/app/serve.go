// serve.go is the App-level façade over the embedded mock server. It
// resolves spec/routes/header exactly like the cobra `serve start` shim:
// spec from the given path with a silent fallback to the default spec,
// routes via ResolveRoutes (PAR-309 precedence), TLS from the config's
// enabled TLS block. The TUI never imports internal/command, so this
// in-process accessor is its serve path; ServeSnapshot reads the live
// engine tracker directly, never a snapshot file.
package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/server"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// serveStartDefaults mirrors the cobra `serve start` flag defaults.
const (
	serveDefaultPort   = "9999"
	serveDefaultHeader = "binary2"
)

// ServeFallbackRoute is the RouteCounts key carrying the catch-all
// fallback hits (server.FallbackRouteName, re-exported so frontends never
// import internal/server).
const ServeFallbackRoute = server.FallbackRouteName

// ErrAlreadyRunning is the sentinel behind ServeStart when a mock server is
// already listening. The concrete error keeps the legacy "mock server is
// already running on port <p>" text verbatim.
var ErrAlreadyRunning = errors.New("mock server already running")

// ErrServeBind is the sentinel behind a ServeStart listener-bind failure
// (the engine's Start error). The concrete error preserves the engine text.
var ErrServeBind = errors.New("mock server bind failed")

// serveAlreadyRunningError carries the legacy already-running text while
// linking to ErrAlreadyRunning for errors.Is.
type serveAlreadyRunningError struct {
	port string
}

func (e *serveAlreadyRunningError) Error() string {
	return fmt.Sprintf("mock server is already running on port %s", e.port)
}

func (e *serveAlreadyRunningError) Unwrap() error { return ErrAlreadyRunning }

// serveBindError preserves the engine's bind error text verbatim while
// matching ErrServeBind (errors.Is) and the underlying error (Unwrap).
type serveBindError struct {
	err error
}

func (e *serveBindError) Error() string { return e.err.Error() }

func (e *serveBindError) Unwrap() error { return e.err }

func (e *serveBindError) Is(target error) bool { return target == ErrServeBind }

// ServeStart starts the embedded mock server in-process. Empty port /
// header fall back to the CLI defaults (9999 / binary2); specPath loads
// with the CLI's silent default-spec fallback; routes resolve through
// ResolveRoutes (PAR-309): a tx-file load failure yields zero routes
// silently, while an explicit routesFile that cannot be read or parsed
// returns a *ConfigError naming the path BEFORE any listener opens. A
// running server is an error, never a restart.
//
// serveMu is held across the closed-check, the already-running-check and
// the listener bind, so a concurrent App.Close cannot interleave and
// leave an orphaned listener.
func (a *App) ServeStart(port, headerType, specPath, txPath, routesFile string) error {
	a.serveMu.Lock()
	defer a.serveMu.Unlock()

	if a.isClosed() {
		return ErrClosed
	}
	if a.srv != nil && a.srv.IsRunning() {
		return &serveAlreadyRunningError{port: a.srv.GetPort()}
	}

	port = strings.TrimSpace(port)
	if port == "" {
		port = serveDefaultPort
	}
	headerType = strings.TrimSpace(headerType)
	if headerType == "" {
		headerType = serveDefaultHeader
	}

	var spec *iso8583.MessageSpec
	if sp := strings.TrimSpace(specPath); sp != "" {
		if s, err := utils.CreateSpecFromFile(sp); err == nil {
			spec = s
		}
	}
	if spec == nil {
		spec = utils.GetDefaultSpec()
	}

	routes, _, err := ResolveRoutes(routesFile, txPath, spec)
	if err != nil {
		return err
	}

	srv := server.NewServer(spec, routes, headerType)
	if tlsFileCfg := a.cfg.GetTLSConfig(); tlsFileCfg != nil && tlsFileCfg.Enabled {
		cryptoTLS, err := tlsFileCfg.BuildServerTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build server TLS configuration: %w", err)
		}
		srv.SetTLSConfig(cryptoTLS)
	}

	if err := srv.Start(port); err != nil {
		return &serveBindError{err: err}
	}
	a.srv = srv
	a.serveRoutes = routes

	return nil
}

// ServeStop stops the embedded mock server; stopping a stopped (or never
// started) server is a no-op, never an error, and nothing ever restarts
// it automatically.
func (a *App) ServeStop() error {
	a.serveMu.Lock()
	defer a.serveMu.Unlock()

	if a.srv == nil || !a.srv.IsRunning() {
		return nil
	}

	return a.srv.Stop()
}

// ServeRunning reports whether the embedded mock server is listening.
func (a *App) ServeRunning() bool {
	a.serveMu.Lock()
	defer a.serveMu.Unlock()

	return a.srv != nil && a.srv.IsRunning()
}

// ServeSnapshot is the in-process stats accessor: the same ServerStats
// view `serve stats` renders, read straight from the live engine tracker.
// After a stop the tracker keeps the final totals until the next start,
// so callers may freeze on this value.
func (a *App) ServeSnapshot() *ServerStats {
	a.serveMu.Lock()
	defer a.serveMu.Unlock()

	if a.srv == nil {
		return &ServerStats{Running: false}
	}

	return NewServerStatsFromServerStats(
		a.srv.GetStats(), a.srv.GetPort(), a.srv.GetHeaderType(),
		a.srv.IsRunning(), a.srv.ActiveConnections(),
	)
}

// ServeRoutes returns the active mock routes: the ones the (last) started
// server was built with, else the tx file's mock_routes — the same list
// `serve routes` lists.
func (a *App) ServeRoutes() []config.MockRouteConfig {
	a.serveMu.Lock()
	routes := a.serveRoutes
	a.serveMu.Unlock()

	if len(routes) > 0 {
		return routes
	}
	if tc, ok := a.tc.(*transactions.TransactionCollection); ok {
		return tc.GetMockRoutes()
	}

	return nil
}
