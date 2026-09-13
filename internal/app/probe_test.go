package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"jiso/internal/config"
)

func TestProbeTargetOpenPort(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	probe := ProbeTarget(context.Background(), host, port, 2*time.Second, nil)

	if !probe.Reachable {
		t.Fatalf("reachable = false, want true (error: %s)", probe.Error)
	}
	if probe.Target != ln.Addr().String() {
		t.Errorf("target = %q, want %q", probe.Target, ln.Addr().String())
	}
	if probe.Error != "" {
		t.Errorf("error = %q, want empty", probe.Error)
	}
	if probe.Latency <= 0 {
		t.Errorf("latency = %v, want > 0", probe.Latency)
	}
}

func TestProbeTargetClosedPort(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	probe := ProbeTarget(context.Background(), host, port, 2*time.Second, nil)

	if probe.Reachable {
		t.Fatal("reachable = true for closed port, want false")
	}
	if probe.Error == "" {
		t.Error("error empty, want the dial failure text")
	}
	if probe.LatencyMs != 0 {
		t.Errorf("latency_ms = %d, want 0 on failure", probe.LatencyMs)
	}

	data, err := json.Marshal(probe)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, key := range []string{`"target"`, `"reachable":false`, `"latency_ms":0`, `"error":"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("probe JSON %s missing %s", data, key)
		}
	}
}

func TestProbeTargetTLSHandshakeSuccess(t *testing.T) {
	t.Parallel()

	ln, serverCfg := tlsListener(t)
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			tlsConn, ok := conn.(*tls.Conn)
			if !ok {
				t.Errorf("accepted conn = %T, want *tls.Conn", conn)
				_ = conn.Close()
				continue
			}
			_ = tlsConn.HandshakeContext(context.Background())
			_ = conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	// Same cert as the server's CA would need: skip verification, exactly
	// what insecure_skip_verify in a TLS config file expresses.
	tlsCfg := &config.TLSFileConfig{Enabled: true, InsecureSkipVerify: true}

	probe := ProbeTarget(context.Background(), "127.0.0.1", port, 2*time.Second, tlsCfg)

	if !probe.Reachable {
		t.Fatalf("reachable = false, want true (error: %s)", probe.Error)
	}
	_ = serverCfg
}

func TestProbeTargetTLSAgainstPlainListener(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Read(make([]byte, 64))
			_ = conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	tlsCfg := &config.TLSFileConfig{Enabled: true, InsecureSkipVerify: true}

	probe := ProbeTarget(context.Background(), "127.0.0.1", port, 2*time.Second, tlsCfg)

	if probe.Reachable {
		t.Fatal("reachable = true with TLS configured against a plain port, want false")
	}
	if !strings.HasPrefix(probe.Error, "tls handshake: ") {
		t.Errorf("error = %q, want it to name the tls handshake failure", probe.Error)
	}
}

// tlsListener starts a TLS listener with an in-memory self-signed cert.
func tlsListener(t *testing.T) (net.Listener, *tls.Config) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "jiso-probe-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}

	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}

	return ln, cfg
}
