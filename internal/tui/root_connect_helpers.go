// root_connect_helpers.go is the seam-split tail of root_connect.go:
// the pure helpers behind the attempt loop — retry-count and backoff
// sources, ConnectOptions derivation, dial-target naming and the framing
// resolution — kept apart so the loop handler stays readable.
package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// connectAttempts is the retry-count source: config reconnect-attempts —
// the same value the REPL connect path feeds into the connection manager.
func (m *RootModel) connectAttempts() int {
	if cfg := m.configOrNil(); cfg != nil {
		if n := cfg.GetReconnectAttempts(); n > 0 {
			return n
		}
	}

	return defaultConnectAttempts
}

// connectBackoffDur resolves the wait before attempt n (injectable for
// fast tests; default: the fixed 1.5s).
func (m *RootModel) connectBackoffDur(n int) time.Duration {
	if m.connectBackoff != nil {
		return m.connectBackoff(n)
	}

	return defaultConnectBackoff
}

// connectOptions maps the form snapshot to per-attempt app overrides:
// listener mode carries the bind port, caller mode splits target into
// host/port (port falling back to the config when the user typed only a
// host), and the header/station/unsolicited/TLS values ride verbatim.
func connectOptions(st *pages.ConnectFormState, cfg *config.Config) app.ConnectOptions {
	opts := app.ConnectOptions{
		LengthType:         connectFormValue(st, pages.ConnectFieldHeader),
		VisaStationID:      strings.TrimSpace(connectFormValue(st, pages.ConnectFieldStation)),
		ProcessUnsolicited: strings.EqualFold(connectFormValue(st, pages.ConnectFieldUnsolicited), "Yes"),
		TLSConfigPath:      strings.TrimSpace(connectFormValue(st, pages.ConnectFieldTLS)),
	}
	port := strings.TrimSpace(connectFormValue(st, pages.ConnectFieldPort))
	if port == "" && cfg != nil {
		port = cfg.GetPort()
	}
	if connectFormValue(st, pages.ConnectFieldMode) == pages.ConnectModeListener {
		opts.Listener = true
		opts.ListenPort = port

		return opts
	}
	opts.Host = strings.TrimSpace(connectFormValue(st, pages.ConnectFieldIP))
	opts.Port = port

	return opts
}

// validateConnectPort checks the shared port field before dialing/listening:
// one Port field for both modes must be 1-65535. An empty value is legal
// (the config fallback fills it); anything else yields the inline error line.
func validateConnectPort(st *pages.ConnectFormState) string {
	v := strings.TrimSpace(connectFormValue(st, pages.ConnectFieldPort))
	if v == "" {
		return ""
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return "port must be 1-65535"
	}

	return ""
}

// connectTargetLabel is the Connected-event detail for the root-stamped
// success (mirrors App.ConnectWithOptions' published address: the listen
// address for listener mode, host:port for a caller dial).
func connectTargetLabel(opts app.ConnectOptions) string {
	if opts.Listener {
		if opts.ListenPort != "" {
			return "0.0.0.0:" + opts.ListenPort
		}

		return "0.0.0.0"
	}
	if opts.Host == "" {
		return ""
	}
	if opts.Port == "" {
		return opts.Host
	}

	return opts.Host + ":" + opts.Port
}

// appConnectWithOptions is the production dial: App.ConnectWithOptions
// (per-attempt overrides + ConnectionEvent publishing on the bus). The
// dial is synchronous and does not observe ctx today; the parameter
// stays so caller cancellations are passed at the right place.
func (m *RootModel) appConnectWithOptions(_ context.Context, opts app.ConnectOptions) error {
	return m.app.ConnectWithOptions(opts)
}

// effectiveLengthType resolves the header framing a connect attempt
// uses: the per-attempt override, then the configured header, then the
// app fallback -- mirroring app.ConnectWithOptions so the display and
// the dial agree by construction, not by coincidence.
func effectiveLengthType(run *connectRun, cfg *config.Config) string {
	if run != nil {
		if v := strings.TrimSpace(run.lengthType); v != "" {
			return v
		}
	}

	if cfg != nil {
		if v := strings.TrimSpace(cfg.GetHeader()); v != "" {
			return v
		}
	}

	return app.DefaultLengthType
}
