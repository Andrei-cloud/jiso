package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"jiso/internal/config"
)

// TargetProbe is the JSON-serializable result of a reachability probe
// (PAR-303 `jiso connect check`). It carries no printer state and nothing
// in this file writes to stdout/stderr; rendering is the command's job.
//
// Error is the empty string on success; LatencyMs is the whole probe cost
// (TCP dial plus TLS handshake when TLS is configured), 0 on failure.
type TargetProbe struct {
	Target    string `json:"target"`
	Reachable bool   `json:"reachable"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error"`

	// Latency is the sub-millisecond precision of LatencyMs, for the
	// human rendering only (the JSON contract pins latency_ms).
	Latency time.Duration `json:"-"`
}

// ProbeTarget performs exactly ONE reachability probe of host:port: a TCP
// dial bounded by timeout, optionally followed by a TLS client handshake
// when tlsCfg enables TLS. It never prompts, never retries (the
// reconnect-attempts=0 semantics of a health probe), and never blocks past
// the timeout beyond ctx cancellation.
//
// A TCP failure or a TLS handshake failure yields Reachable=false with the
// underlying error text; a TLS-config build failure is reported as
// "tls configuration: ..." (the certs/CA on disk are unreadable or
// inconsistent, so the target cannot be used as configured).
func ProbeTarget(ctx context.Context, host, port string, timeout time.Duration, tlsCfg *config.TLSFileConfig) *TargetProbe {
	target := net.JoinHostPort(host, port)
	probe := &TargetProbe{Target: target}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()

	dialer := net.Dialer{}

	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		probe.Error = err.Error()

		return probe
	}
	defer func() { _ = conn.Close() }()

	if tlsCfg != nil && tlsCfg.Enabled {
		cryptoCfg, err := tlsCfg.BuildCryptoTLSConfig()
		if err != nil {
			probe.Error = fmt.Sprintf("tls configuration: %v", err)

			return probe
		}
		if cryptoCfg != nil {
			tlsConn := tls.Client(conn, cryptoCfg)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				probe.Error = fmt.Sprintf("tls handshake: %v", err)

				return probe
			}
			_ = tlsConn.Close()
		}
	}

	probe.Reachable = true
	probe.Latency = time.Since(start)
	probe.LatencyMs = probe.Latency.Milliseconds()

	return probe
}
