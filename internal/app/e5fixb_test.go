// e5fixb_test.go holds the E5-review regression tests for the app façade:
// the stress-summary persistence (and its error hook) in the TUI process,
// the completed background worker's frozen Runtime, the typed not-found
// sentinels (worker / session / transaction), the errno-wrapped analyze
// capture errors, the §J context-cancellation legs, and the CTF default
// filename stamped from an injected clock.
package app

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jiso/internal/app/events"
	"jiso/internal/db"
)

// M7a: a stress run with a configured DB persists its summary row through the
// synchronous global-conn fallback (the TUI never inits the async logger).
func TestStressSummaryPersistedToDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stress.db")
	if err := db.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	cfg.SetDbPath(dbPath)

	sender := newFakeSender(0, true)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.SetWorkerSenderResolver(func() WorkerSender { return sender })

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	id, err := a.StressStart([]string{"TX_A"}, 50, 50*time.Millisecond, 2*time.Second, 1)
	if err != nil {
		t.Fatalf("StressStart: %v", err)
	}
	drainWorkerEvents(t, ch, id)

	summary, err := a.StressSummaryByID(id)
	if err != nil {
		t.Fatalf("StressSummaryByID: %v", err)
	}

	rows, err := db.GetSessionStressTestSummaries(summary.SessionID)
	if err != nil {
		t.Fatalf("GetSessionStressTestSummaries: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("persisted %d summary rows, want 1", len(rows))
	}
	if rows[0].WorkerID != id {
		t.Fatalf("persisted worker id = %q, want %q", rows[0].WorkerID, id)
	}
}

// M7a: when persistence fails (no conn behind the configured path), the error
// is routed to the app debug hook (events.Logf) instead of being swallowed.
func TestStressSummaryPersistFailureIsLogged(t *testing.T) {
	_ = db.Close() // guarantee the global conn is nil for this test

	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	cfg.SetDbPath(filepath.Join(t.TempDir(), "never-opened.db"))

	sender := newFakeSender(0, true)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.SetWorkerSenderResolver(func() WorkerSender { return sender })

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	id, err := a.StressStart([]string{"TX_A"}, 50, 50*time.Millisecond, 2*time.Second, 1)
	if err != nil {
		t.Fatalf("StressStart: %v", err)
	}

	var logf []events.Logf
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed before WorkerStopped")
			}
			switch e := ev.(type) {
			case events.Logf:
				logf = append(logf, e)
			case events.WorkerStopped:
				if e.ID != id {
					continue
				}
				if !hasErrorLogf(logf, "persist") {
					t.Fatalf("persist failure not routed to events.Logf; saw %+v", logf)
				}

				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for WorkerStopped")
		}
	}
}

func hasErrorLogf(lines []events.Logf, substr string) bool {
	for _, l := range lines {
		if l.Level == "error" && strings.Contains(l.Msg, substr) {
			return true
		}
	}

	return false
}

// M7c: a completed background worker's Runtime stops inflating once finished
// (frozen at the completion instant), matching the stress worker's view.
func TestCompletedBackgroundWorkerRuntimeStable(t *testing.T) {
	sender := newFakeSender(time.Millisecond, true) // always fails -> breaker trips
	a := newWorkerTestApp(t, sender)

	if _, err := a.WorkerStart("TX", 1, 5*time.Millisecond); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}

	// Wait for the circuit breaker to finish the worker (completed, still
	// listed until explicitly stopped).
	deadline := time.Now().Add(5 * time.Second)
	for {
		views := a.Workers()
		if len(views) == 1 && views[0].Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker never reached completed; views = %+v", a.Workers())
		}
		time.Sleep(5 * time.Millisecond)
	}

	first := a.Workers()[0].Runtime
	time.Sleep(60 * time.Millisecond)
	second := a.Workers()[0].Runtime

	if first != second {
		t.Fatalf("completed worker Runtime drifted: %v then %v", first, second)
	}
}

// dbview: SessionStats for a session absent from the DB returns the typed
// db.ErrSessionNotFound instead of a fabricated zero-count map.
func TestSessionStatsNonexistentSessionTyped(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))

	_, err := a.SessionStats(context.Background(), "no-such-session")
	if !errors.Is(err, db.ErrSessionNotFound) {
		t.Fatalf("SessionStats(unknown) = %v, want errors.Is(db.ErrSessionNotFound)", err)
	}

	// A real session still returns its counters.
	if _, err := a.SessionStats(context.Background(), "9f3ca1e2b7d84455a1"); err != nil {
		t.Fatalf("SessionStats(real) = %v, want success", err)
	}
}

// dbview: ReviewTx for an absent transaction row returns the app-level
// ErrTxNotFound sentinel.
func TestReviewTxMissingRowTypedErrTxNotFound(t *testing.T) {
	a := dbViewApp(t, dbViewFixture(t))

	_, err := a.ReviewTx(context.Background(), 999999)
	if !errors.Is(err, ErrTxNotFound) {
		t.Fatalf("ReviewTx(missing) = %v, want errors.Is(ErrTxNotFound)", err)
	}
}

// workers: the not-found errors on WorkerStop / StressStop / StressSummaryByID
// are typed ErrWorkerNotFound while keeping the legacy "not found" text.
func TestWorkerNotFoundSentinels(t *testing.T) {
	sender := newFakeSender(time.Millisecond, false)
	a := newWorkerTestApp(t, sender)

	if err := a.WorkerStop("nope"); !errors.Is(err, ErrWorkerNotFound) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("WorkerStop(unknown) = %v, want ErrWorkerNotFound with legacy text", err)
	}
	if err := a.StressStop("nope"); !errors.Is(err, ErrWorkerNotFound) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("StressStop(unknown) = %v, want ErrWorkerNotFound with legacy text", err)
	}
	if _, err := a.StressSummaryByID("nope"); !errors.Is(err, ErrWorkerNotFound) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("StressSummaryByID(unknown) = %v, want ErrWorkerNotFound with legacy text", err)
	}
}

// analyzeview: a missing capture surfaces the os.Stat errno inside the typed
// ConfigError (errors.Is fs.ErrNotExist), not a discarded-errno text error.
func TestAnalyzeEnumerateMissingPcapWrapsErrno(t *testing.T) {
	a := analyzeApp(t)
	missing := filepath.Join(t.TempDir(), "gone.pcap")

	_, err := a.EnumerateFlows(context.Background(), missing, "ascii4", "")
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != missing {
		t.Fatalf("err = %v (%T), want *ConfigError naming %s", err, err, missing)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want the os.Stat errno wrapped (errors.Is fs.ErrNotExist)", err)
	}

	if _, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{PcapPath: missing, Mode: AnalyzeModeTx}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("RunAnalyze(missing) = %v, want the errno wrapped", err)
	}
}

// analyzeview: a cancelled §J context stops the analyze legs (the entry check
// and the every-100-iteration loop re-checks wired into EnumerateFlows/
// RunAnalyze).
func TestAnalyzeLegsHonourContextCancel(t *testing.T) {
	pcap, _ := analyzeFixture(t)
	a := analyzeApp(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := a.EnumerateFlows(ctx, pcap, "ascii4", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnumerateFlows(cancelled) = %v, want context.Canceled", err)
	}
	if _, err := a.RunAnalyze(ctx, AnalyzeRunOptions{PcapPath: pcap, Mode: AnalyzeModeTx}); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunAnalyze(cancelled) = %v, want context.Canceled", err)
	}
}

// ctfview: the CTF default filename and the record's processing date are
// stamped from the caller's injected clock, not a time.Now inside the
// façade.
func TestCtfOutputPathUsesInjectedNow(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 34, 56, 0, time.UTC)

	path := ctfOutputPath("", now)
	if !strings.Contains(path, "20260909") || !strings.Contains(path, "123456") {
		t.Fatalf("ctfOutputPath = %q, want the injected date/time", path)
	}
	if got := ctfOutputPath("/explicit/out.ctf", now); got != "/explicit/out.ctf" {
		t.Fatalf("explicit path must win, got %q", got)
	}

	a := dbViewApp(t, ctfFixture(t))
	result, _, err := a.generateCTF(context.Background(), "ctfsession1", DefaultCtfCIB, "", 1, now)
	if err != nil {
		t.Fatalf("generateCTF: %v", err)
	}
	wantDate := "26252" // yy + yday for 2026-09-09 (day-of-year 252)
	if result.ProcessingDate != wantDate {
		t.Fatalf("ProcessingDate = %q, want %q (stamped from the injected now)", result.ProcessingDate, wantDate)
	}
}
