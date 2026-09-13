package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"jiso/internal/server"
	"jiso/internal/utils"
)

// TestStateDirOverrideOrder pins the hermetic-test contract: JISO_STATE_DIR
// wins verbatim, then XDG_STATE_HOME/jiso, and the directory is created 0700
// so the TUI debug log (TUI-409) can open its file without a second mkdir.
func TestStateDirOverrideOrder(t *testing.T) {
	t.Run("JISO_STATE_DIR wins verbatim", func(t *testing.T) {
		want := filepath.Join(t.TempDir(), "hermetic")
		t.Setenv("JISO_STATE_DIR", want)
		t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "xdg"))

		got, err := StateDir()
		if err != nil {
			t.Fatalf("StateDir: %v", err)
		}
		if got != want {
			t.Errorf("StateDir = %q, want %q", got, want)
		}
		assertDir0700(t, got)
	})

	t.Run("XDG_STATE_HOME gets the jiso subdir", func(t *testing.T) {
		xdg := t.TempDir()
		t.Setenv("JISO_STATE_DIR", "")
		t.Setenv("XDG_STATE_HOME", xdg)

		got, err := StateDir()
		if err != nil {
			t.Fatalf("StateDir: %v", err)
		}
		if want := filepath.Join(xdg, "jiso"); got != want {
			t.Errorf("StateDir = %q, want %q", got, want)
		}
		assertDir0700(t, got)
	})
}

func assertDir0700(t *testing.T, dir string) {
	t.Helper()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("state dir not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("state dir mode = %o, want 700", perm)
	}
}

func TestServeStateDirOverride(t *testing.T) {
	t.Setenv(ServeStateDirEnv, filepath.Join(t.TempDir(), "state"))

	dir, err := ServeStateDir()
	if err != nil {
		t.Fatalf("ServeStateDir: %v", err)
	}

	want := os.Getenv(ServeStateDirEnv)
	if dir != want {
		t.Fatalf("ServeStateDir = %q, want %q", dir, want)
	}

	path, err := ServeStatePath("8583")
	if err != nil {
		t.Fatalf("ServeStatePath: %v", err)
	}
	if filepath.Dir(path) != want || filepath.Base(path) != "serve-8583.json" {
		t.Errorf("ServeStatePath = %q, want serve-8583.json under %q", path, want)
	}

	statsPath, err := ServeStatsPath("8583")
	if err != nil {
		t.Fatalf("ServeStatsPath: %v", err)
	}
	if filepath.Dir(statsPath) != want || filepath.Base(statsPath) != "serve-8583.stats.json" {
		t.Errorf("ServeStatsPath = %q, want serve-8583.stats.json under %q", statsPath, want)
	}
}

func TestServeStateDirXDG(t *testing.T) {
	t.Setenv(ServeStateDirEnv, "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "xdg"))

	dir, err := ServeStateDir()
	if err != nil {
		t.Fatalf("ServeStateDir: %v", err)
	}

	if want := filepath.Join(filepath.Clean(os.Getenv("XDG_STATE_HOME")), "jiso"); dir != want {
		t.Errorf("ServeStateDir = %q, want %q", dir, want)
	}
}

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Fatal("own pid reported dead")
	}
	if PIDAlive(0) || PIDAlive(-1) {
		t.Fatal("non-positive pid reported alive")
	}
	if PIDAlive(1 << 30) {
		t.Fatal("pid 1<<30 reported alive")
	}
}

func TestServeSideChannelRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ServeStateDirEnv, dir)

	srv := server.NewServer(utils.GetDefaultSpec(), nil, "binary2")
	if err := srv.Start("0"); err != nil {
		t.Fatalf("server start on ephemeral port: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	bound, err := srv.BoundPort()
	if err != nil {
		t.Fatalf("BoundPort: %v", err)
	}

	statePath, stop, err := StartServeSideChannel(srv, "127.0.0.1", "session.db", nil, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("StartServeSideChannel: %v", err)
	}

	state, path, err := ReadServeState(bound)
	if err != nil || state == nil {
		t.Fatalf("ReadServeState = (%v, %v), err %v", state, path, err)
	}
	if path != statePath {
		t.Errorf("state path = %q, want %q", path, statePath)
	}
	if state.PID != os.Getpid() || !PIDAlive(state.PID) {
		t.Errorf("state pid = %d, want %d (alive)", state.PID, os.Getpid())
	}
	if state.Port != bound || state.Host != "127.0.0.1" || state.DB != "session.db" {
		t.Errorf("state = %+v, want host 127.0.0.1/db session.db on port %s", state, bound)
	}
	if state.StartedAt.IsZero() {
		t.Error("state started_at is zero")
	}

	view, _, err := ReadServeStatsSnapshot(bound)
	if err != nil {
		t.Fatalf("ReadServeStatsSnapshot: %v", err)
	}
	if !view.Running || view.Port != bound || view.PID != os.Getpid() || view.SnapshotAt == nil {
		t.Errorf("snapshot = %+v, want running view of port %s with pid/snapshot_at", view, bound)
	}

	time.Sleep(60 * time.Millisecond)
	if err := stop(); err != nil {
		t.Fatalf("ticker stop: %v", err)
	}

	if err := stop(); err != nil {
		t.Fatalf("second ticker stop (idempotent): %v", err)
	}

	after, path, err := ReadServeState(bound)
	if after != nil || err != nil {
		t.Fatalf("state after clean stop: %+v (%v), want absent at %s", after, err, path)
	}
	if _, _, err := ReadServeStatsSnapshot(bound); err == nil {
		t.Fatal("snapshot still present after clean stop")
	}
}
