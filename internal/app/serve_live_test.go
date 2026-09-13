// serve_live_test.go exercises the in-process serve façade (serve.go)
// against the REAL engine: ephemeral boot (Start("0") + BoundPort, the
// PAR-301 harness pattern), a framed client exchange, and the
// ServeSnapshot/ServeStop claims in the façade docs.
package app

import (
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// serveLiveTxJSON: a composable 0200 and an MTI-matched route (delay 0).
const serveLiveTxJSON = `[
 {"type":"transaction","name":"Purchase","description":"0200","fields":{"0":"0200","3":"000000","11":"000001"}},
 {"type":"mock_route","name":"0200/proc","match_fields":{"0":"0200"},"response_mti":"0210","response_fields":{"0":"0210","39":"00"},"delay_ms":0}
]`

func serveLiveApp(t *testing.T) *App {
	t.Helper()
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.ServeStop(); _ = a.Close() })

	return a
}

func serveLiveTxFile(t *testing.T, body string) string {
	t.Helper()
	p := t.TempDir() + "/tx.json"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}

	return p
}

// serveLiveExchange sends ComposeRaw(name) framed like the engine reads
// (binary2 header) and unpacks the response; RecordMessage precedes the
// write, so snapshot counts hold on return.
func serveLiveExchange(t *testing.T, a *App, txPath, name string) (respMTI, respCode string) {
	t.Helper()

	spec := utils.GetDefaultSpec()
	repo, err := transactions.NewTransactionCollection(txPath, spec)
	if err != nil {
		t.Fatalf("client collection: %v", err)
	}
	req, err := repo.ComposeRaw(name)
	if err != nil {
		t.Fatalf("ComposeRaw: %v", err)
	}
	payload, err := req.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	port, err := a.srv.BoundPort()
	if err != nil {
		t.Fatalf("BoundPort: %v", err)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
	if err != nil {
		t.Fatalf("dial :%s: %v", port, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	h := utils.NewBinary2BytesAdapter()
	h.SetLength(len(payload))
	if _, err := h.WriteTo(conn); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	rh := utils.NewBinary2BytesAdapter()
	if _, err := rh.ReadFrom(conn); err != nil {
		t.Fatalf("read resp header: %v", err)
	}
	buf := make([]byte, rh.Length())
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read resp payload: %v", err)
	}
	resp := iso8583.NewMessage(spec)
	if err := resp.Unpack(buf); err != nil {
		t.Fatalf("unpack resp: %v", err)
	}
	mti, _ := resp.GetMTI()
	if f39 := resp.GetField(39); f39 != nil {
		respCode, _ = f39.String()
	}

	return mti, respCode
}

func TestServeLiveStartExchangeSnapshotStop(t *testing.T) {
	a := serveLiveApp(t)
	tx := serveLiveTxFile(t, serveLiveTxJSON)
	// Empty header → binary2 default; "0" → ephemeral port.
	if err := a.ServeStart("0", "", "", tx); err != nil {
		t.Fatalf("ServeStart: %v", err)
	}
	if !a.ServeRunning() {
		t.Fatal("ServeRunning false after start")
	}
	snap := a.ServeSnapshot()
	// Port is the port actually bound, not the "0" that was asked for: an operator
	// looking at the server card needs the number to connect to, and "0" tells them
	// nothing. So assert it resolved to something real instead.
	if !snap.Running || snap.Port == "" || snap.Port == "0" || snap.HeaderType != "binary2" {
		t.Fatalf("start snapshot = running:%v port:%q header:%q", snap.Running, snap.Port, snap.HeaderType)
	}
	if snap.TotalServed != 0 || snap.Matched != 0 {
		t.Fatalf("fresh snapshot has traffic: %+v", snap)
	}
	respMTI, respCode := serveLiveExchange(t, a, tx, "Purchase")
	if respMTI != "0210" || respCode != "00" {
		t.Fatalf("engine responded %s/%s, want 0210/00", respMTI, respCode)
	}

	snap = a.ServeSnapshot()
	if snap.TotalServed != 1 || snap.Matched != 1 || snap.RouteCounts["0200/proc"] != 1 {
		t.Fatalf("post-exchange snapshot: served=%d matched=%d routes=%v", snap.TotalServed, snap.Matched, snap.RouteCounts)
	}
	if snap.Uptime <= 0 || snap.StartTime.IsZero() {
		t.Fatalf("uptime/start time not stamped: %+v", snap)
	}
	routes := a.ServeRoutes()
	if len(routes) != 1 || routes[0].Name != "0200/proc" {
		t.Fatalf("ServeRoutes = %+v", routes)
	}

	// Stop: no error, truth flips, totals freeze until the next Start.
	if err := a.ServeStop(); err != nil {
		t.Fatalf("ServeStop: %v", err)
	}
	if a.ServeRunning() {
		t.Fatal("still running after stop")
	}
	frozen := a.ServeSnapshot()
	if frozen.TotalServed != 1 || frozen.Matched != 1 {
		t.Fatalf("snapshot not frozen after stop: %+v", frozen)
	}
	if err := a.ServeStop(); err != nil {
		t.Fatalf("second ServeStop must be a no-op, got %v", err)
	}

	// A restart builds a fresh engine whose tracker starts at zero.
	if err := a.ServeStart("0", "binary2", "", tx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if snap := a.ServeSnapshot(); snap.TotalServed != 0 {
		t.Fatalf("restart snapshot not reset: %d", snap.TotalServed)
	}
}

func TestServeLiveSpecFallbackAndRoutesFailure(t *testing.T) {
	a := serveLiveApp(t)
	tx := serveLiveTxFile(t, serveLiveTxJSON)
	// An unloadable spec path silently falls back to the default spec —
	// proven behaviourally: the engine answers a default-spec message.
	if err := a.ServeStart("0", "binary2", t.TempDir()+"/nope.json", tx); err != nil {
		t.Fatalf("ServeStart with bad spec path: %v", err)
	}
	if _, code := serveLiveExchange(t, a, tx, "Purchase"); code != "00" {
		t.Fatalf("default-spec fallback exchange got code %q", code)
	}
	_ = a.ServeStop()
	// An unloadable tx path yields zero routes; traffic lands on the
	// catch-all fallback key, Matched stays 0.
	a2 := serveLiveApp(t)
	if err := a2.ServeStart("0", "binary2", "", t.TempDir()+"/missing-tx.json"); err != nil {
		t.Fatalf("ServeStart with bad tx path: %v", err)
	}
	if n := len(a2.ServeRoutes()); n != 0 {
		t.Fatalf("ServeRoutes after tx load failure = %d, want 0", n)
	}
	// Compose the request from the good file; the server has no routes.
	if _, code := serveLiveExchange(t, a2, tx, "Purchase"); code == "00" {
		t.Fatalf("route-less server must not answer 00, got %q", code)
	}
	snap := a2.ServeSnapshot()
	if snap.TotalServed != 1 || snap.Matched != 0 || snap.RouteCounts[ServeFallbackRoute] != 1 {
		t.Fatalf("fallback accounting: served=%d matched=%d routes=%v", snap.TotalServed, snap.Matched, snap.RouteCounts)
	}
}

func TestServeLiveAlreadyRunningNeverRestarts(t *testing.T) {
	a := serveLiveApp(t)
	tx := serveLiveTxFile(t, serveLiveTxJSON)
	if err := a.ServeStart("0", "binary2", "", tx); err != nil {
		t.Fatalf("first start: %v", err)
	}
	first, err := a.srv.BoundPort()
	if err != nil {
		t.Fatalf("BoundPort: %v", err)
	}
	err = a.ServeStart("0", "binary2", "", tx)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second start = %v, want already-running error", err)
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second start = %v, want errors.Is(ErrAlreadyRunning)", err)
	}
	if second, err := a.srv.BoundPort(); err != nil || second != first {
		t.Fatalf("listener moved after rejected start: %q vs %q (err %v)", second, first, err)
	}
	mti, code := serveLiveExchange(t, a, tx, "Purchase")
	if mti != "0210" || code != "00" {
		t.Fatalf("first server stopped serving after rejected restart: %s/%s", mti, code)
	}
}

func TestServeSnapshotWithoutServerAndTLSMisconfig(t *testing.T) {
	a := serveLiveApp(t)
	snap := a.ServeSnapshot()
	if snap.Running || snap.Port != "" || snap.TotalServed != 0 {
		t.Fatalf("never-started snapshot = %+v, want idle", snap)
	}

	// An enabled-but-broken TLS block fails before the listener opens.
	cfg := a.Config()
	cfg.SetTLSConfig(&config.TLSFileConfig{Enabled: true, ServerCert: "missing.pem", ServerKey: "missing.key"})
	t.Cleanup(func() { cfg.SetTLSConfig(nil) })
	tx := serveLiveTxFile(t, serveLiveTxJSON)
	err := a.ServeStart("0", "binary2", "", tx)
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("ServeStart with broken TLS config = %v, want TLS failure", err)
	}
	if a.ServeRunning() {
		t.Fatal("server running after TLS misconfig")
	}
}

// Port-default mirror pin: serveDefaultPort/Header == the cobra shim.
func TestServeDefaultPortMirrorPin(t *testing.T) {
	t.Parallel()

	if serveDefaultPort != "9999" || serveDefaultHeader != "binary2" {
		t.Fatalf("serve defaults drifted: %q/%q", serveDefaultPort, serveDefaultHeader)
	}
}

// M7d: ServeStart on a closed App returns the typed ErrClosed (the same
// sentinel the other App methods surface after Close), not an untyped text.
func TestServeStartOnClosedAppReturnsErrClosed(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = a.ServeStart("0", "binary2", "", t.TempDir()+"/tx.json")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("ServeStart on closed App = %v, want errors.Is(ErrClosed)", err)
	}
	if a.ServeRunning() {
		t.Fatal("ServeStart on a closed App started an engine")
	}
}

// M7b: App.Close stops the embedded serve engine — its listener port is freed
// (a dial fails immediately) and its accept loop does not outlive the App
// (goleak-style goroutine settle, same pattern as workers_test.go).
func TestCloseStopsServeEngine(t *testing.T) {
	before := runtime.NumGoroutine()

	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tx := serveLiveTxFile(t, serveLiveTxJSON)

	if err := a.ServeStart("0", "binary2", "", tx); err != nil {
		t.Fatalf("ServeStart: %v", err)
	}
	if !a.ServeRunning() {
		t.Fatal("not running after start")
	}
	port, err := a.srv.BoundPort()
	if err != nil {
		t.Fatalf("BoundPort: %v", err)
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second); err != nil {
		t.Fatalf("dial live engine :%s: %v", port, err)
	} else {
		_ = c.Close()
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if a.ServeRunning() {
		t.Fatal("ServeRunning true after Close")
	}

	// Port freed: a fresh dial fails immediately (nothing is listening).
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 500*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatalf("dial still succeeds after Close on :%s (listener leaked)", port)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if n := runtime.NumGoroutine(); n <= before+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: baseline %d, now %d after serve+close", before, runtime.NumGoroutine())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
