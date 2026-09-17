// connect_options.go is the extension: a per-attempt connect entry
// point for the §E dialog. App.Connect/ConnectWith keep their historical
// behaviour (configured target/header, matcher untouched); ConnectWithOptions
// additionally accepts caller target, listener mode + bind port, visa
// station ID, mock-routes unsolicited handling, and a TLS config path,
// mirroring the REPL connect command's answers (internal/command/connect.go)
// without its survey prompts. Every override lands on the in-memory config
// and service only — nothing is ever written to the config file.
package app

import (
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/moov-io/iso8583/network"

	"jiso/internal/app/events"
	iconn "jiso/internal/connection"
	"jiso/internal/server"
	"jiso/internal/service"
	"jiso/internal/utils"
)

// tlsApplier and matcherApplier name the two service setters the connect
// path applies before dialing; *service.Service satisfies both.
type (
	tlsApplier interface{ SetTLSConfig(*tls.Config) }

	matcherApplier interface{ SetMockMatcher(iconn.RouteMatcher) }
)

// defaultListenPort is the listener fallback when neither the form nor the
// config carries a bind port (same default as the REPL connect prompt).
const defaultListenPort = "9999"

// ConnectOptions carries the §E dialog's per-attempt overrides. The zero
// value means "use the configured values" for every field, so a caller can
// override exactly what the user edited.
type ConnectOptions struct {
	// Listener dials nobody and waits for an incoming connection instead
	// (REPL "Listener (Wait for incoming)").
	Listener bool
	// LengthType is the header type (ascii4, binary2, bcd2, binary4, NAPS,
	// visa); empty = the configured header, defaulting to ascii4 — the
	// same resolution App.Connect performs.
	LengthType string
	// Host and Port override the caller dial target when both are set;
	// empty falls back to the configured host:port.
	Host, Port string
	// ListenPort binds the listener; empty = configured port, then 9999.
	ListenPort string
	// VisaStationID is validated and stored when LengthType is visa;
	// empty keeps the configured station ID.
	VisaStationID string
	// ProcessUnsolicited installs the transaction file's mock_routes as
	// the unsolicited-message matcher; false explicitly clears it (the
	// REPL "No" answer).
	ProcessUnsolicited bool
	// TLSConfigPath loads and applies a TLS config file for this
	// attempt; empty leaves the service's TLS settings untouched.
	TLSConfigPath string
}

// ConnectWithOptions connects with per-attempt overrides (§E). Success and
// failure are published to the event bus as ConnectionEvent, exactly like
// ConnectWith, so the dashboard card and the frame status chip update
// through the existing bridge path.
func (a *App) ConnectWithOptions(opts ConnectOptions) error {
	svc, err := a.openService()
	if err != nil {
		return err
	}

	lengthType := strings.TrimSpace(opts.LengthType)
	if lengthType == "" {
		lengthType = strings.TrimSpace(a.cfg.GetHeader())
	}
	if lengthType == "" {
		lengthType = defaultLengthType
	}

	if err := a.applyConnectTLS(svc, opts.TLSConfigPath); err != nil {
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}

	if strings.EqualFold(lengthType, "visa") && opts.VisaStationID != "" {
		if _, err := utils.ParseStationID(opts.VisaStationID); err != nil {
			err = fmt.Errorf("invalid visa station ID %q: %w", opts.VisaStationID, err)
			a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

			return err
		}
		a.cfg.SetVisaStationID(opts.VisaStationID)
	}

	a.applyUnsolicitedMatcher(svc, opts.ProcessUnsolicited)

	header, naps, err := selectConnectHeader(lengthType, a.cfg.GetVisaStationID())
	if err != nil {
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}

	if opts.Listener {
		return a.connectListener(svc, header, naps, opts.ListenPort)
	}

	host, port := strings.TrimSpace(opts.Host), strings.TrimSpace(opts.Port)
	if host == "" || port == "" {
		host, port = a.cfg.GetHost(), a.cfg.GetPort()
	}
	if host == "" || port == "" {
		err := fmt.Errorf(
			"target host and port are not configured. Please set target using 'target <host:port>' first",
		)
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}
	if host != a.cfg.GetHost() || port != a.cfg.GetPort() {
		a.cfg.SetHost(host)
		a.cfg.SetPort(port)
		svc.SetTarget(host, port)
	}

	if err := svc.Connect(naps, header); err != nil {
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}
	a.events.Publish(events.ConnectionEvent{State: events.StateConnected, Detail: a.targetAddress()})

	return nil
}

// connectListener waits for an incoming connection on port (REPL listener
// branch, previously service-only). The published Connected event carries
// the listen address, not the configured caller target.
func (a *App) connectListener(svc *service.Service, header network.Header, naps bool, port string) error {
	listenPort := strings.TrimSpace(port)
	if listenPort == "" {
		listenPort = strings.TrimSpace(a.cfg.GetPort())
	}
	if listenPort == "" {
		listenPort = defaultListenPort
	}

	if err := svc.Listen(listenPort, naps, header); err != nil {
		err = fmt.Errorf("listener failed on port %s: %w", listenPort, err)
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}
	if !svc.IsConnected() {
		err := fmt.Errorf("listener connection accepted on port %s but not online", listenPort)
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}
	a.events.Publish(events.ConnectionEvent{
		State:  events.StateConnected,
		Detail: "0.0.0.0:" + listenPort,
	})

	return nil
}

// applyConnectTLS loads opts.TLSConfigPath (when set and different from the
// configured path) and applies it to the service; a failed load is an
// attempt error, never a silent fall back.
func (a *App) applyConnectTLS(svc tlsApplier, path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == a.cfg.GetTLSConfigPath() {
		return nil
	}
	if err := a.cfg.SetTLSConfigPath(path); err != nil {
		return fmt.Errorf("loading TLS config %q: %w", path, err)
	}
	tc := a.cfg.GetTLSConfig()
	if tc == nil || !tc.Enabled {
		svc.SetTLSConfig(nil)

		return nil
	}
	cryptoTLS, err := tc.BuildCryptoTLSConfig()
	if err != nil {
		return fmt.Errorf("building TLS configuration: %w", err)
	}
	svc.SetTLSConfig(cryptoTLS)

	return nil
}

// applyUnsolicitedMatcher mirrors the REPL's unsolicited answer: Yes
// installs the transaction file's mock_routes (nil when the file has
// none), No explicitly clears any previous matcher.
func (a *App) applyUnsolicitedMatcher(svc matcherApplier, process bool) {
	if !process || a.tc == nil {
		svc.SetMockMatcher(nil)

		return
	}
	routes := a.tc.GetMockRoutes()
	if len(routes) == 0 {
		svc.SetMockMatcher(nil)

		return
	}
	svc.SetMockMatcher(server.NewMatcher(routes))
}

// selectConnectHeader builds the header for lengthType. The visa branch is
// resolved from the passed station ID (App's own config) instead of
// utils.SelectLength's process-singleton read, so per-attempt station IDs
// take effect on any config instance.
func selectConnectHeader(lengthType, stationID string) (network.Header, bool, error) {
	if strings.EqualFold(lengthType, "visa") {
		sid := strings.TrimSpace(stationID)
		if sid == "" {
			sid = "000000" // server-role/fallback default, same as SelectLength
		}

		h, err := utils.NewVisaHeader(sid)

		return h, false, err
	}
	h, err := utils.SelectLength(lengthType)

	return h, strings.EqualFold(lengthType, "NAPS"), err
}
