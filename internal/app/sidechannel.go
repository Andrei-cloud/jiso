package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"jiso/internal/config"
	"jiso/internal/server"
	"jiso/internal/utils"
)

// PAR-304 serve side-channel. The serve path (cobra `jiso serve start` and
// the REPL `serve`) publishes two files in the jiso state dir so a separate
// process can query a RUNNING server:
//
//	serve-<port>.json        state file, written once at start
//	serve-<port>.stats.json  ServerStats snapshot, rewritten atomically
//	                         every ServeStatsRefreshInterval while running
//
// The snapshot file was chosen over an admin TCP/HTTP endpoint: it needs no
// extra port (no collisions, nothing to record beyond the state file), a
// same-directory temp file plus rename makes every read observe a complete
// file, and a 1 s refresh is fresh enough for a stats view. A clean stop
// removes both files; after a crash or kill -9 the state file goes stale
// and readers treat it as "not running" via PIDAlive without deleting it
// (the next `serve start` overwrites it).
//
// The state dir is $JISO_STATE_DIR when set (tests point it at $WORK so the
// real XDG state dir is never polluted), else <XDG state home>/jiso
// ($XDG_STATE_HOME or $HOME/.local/state per the XDG spec).

const (
	// ServeStateDirEnv overrides the state dir; honored first so tests never
	// touch the real XDG state directory.
	ServeStateDirEnv = utils.StateDirEnv

	// ServeStatsRefreshInterval is how often the running server rewrites
	// its stats snapshot file.
	ServeStatsRefreshInterval = time.Second
)

// ServeRoute is the route set persisted into serve-<port>.json at server
// start: the display fields of a configured mock route (name, match
// fields, response MTI, latency) — exactly what `jiso serve routes` lists,
// so the command reports the set the RUNNING server matches against
// (UAT-02) instead of re-deriving routes from the querying process's own
// spec/tx config slots. Response templates are deliberately excluded: the
// state file stays a small identity file.
type ServeRoute struct {
	Name        string         `json:"name"`
	MatchFields map[string]any `json:"match_fields,omitempty"`
	ResponseMTI string         `json:"response_mti,omitempty"`
	DelayMs     int            `json:"delay_ms,omitempty"`
	LatencyMs   int            `json:"latency_ms,omitempty"`
	JitterMs    int            `json:"jitter_ms,omitempty"`
}

// ServeState is the content of serve-<port>.json: enough for an outside
// process to decide whether the server is alive, where to look for its
// stats snapshot, and which mock routes it loaded. AdminPort stays nil
// while the channel is the snapshot file (reserved for a future admin
// endpoint). Routes is always an array in files written by this build
// (empty when the server has none); a nil Routes decodes only from a
// pre-UAT-02 state file, whose reader falls back to the stats snapshot's
// route counts.
type ServeState struct {
	PID       int          `json:"pid"`
	Host      string       `json:"host"`
	Port      string       `json:"port"`
	StartedAt time.Time    `json:"started_at"`
	AdminPort *int         `json:"admin_port,omitempty"`
	DB        string       `json:"db,omitempty"`
	Routes    []ServeRoute `json:"routes"`
}

// serveRoutesFromConfig projects the server's resolved mock routes into
// the state-file representation. The result is never nil, so a fresh
// state file always carries a "routes" array (empty = the server has no
// routes) and readers can tell "no routes" from "legacy file".
func serveRoutesFromConfig(routes []config.MockRouteConfig) []ServeRoute {
	out := make([]ServeRoute, 0, len(routes))
	for _, r := range routes {
		out = append(out, ServeRoute{
			Name:        r.Name,
			MatchFields: r.MatchFields,
			ResponseMTI: r.ResponseMTI,
			DelayMs:     r.DelayMs,
			LatencyMs:   r.LatencyMs,
			JitterMs:    r.JitterMs,
		})
	}

	return out
}

// ServeStateDir returns the directory holding serve state files:
// $JISO_STATE_DIR when non-empty, else <XDG state home>/jiso (no mkdir;
// PAR-304 goldens pin ~/.local/state as the home fallback). The STAN
// counter's persistence dir (utils.StateDir) resolves identically so
// both state files always share one directory.
func ServeStateDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(ServeStateDirEnv)); dir != "" {
		return dir, nil
	}

	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving XDG state home (or set $%s): %w", ServeStateDirEnv, err)
		}

		base = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(base, "jiso"), nil
}

// ServeStatePath resolves serve-<port>.json inside the state dir.
func ServeStatePath(port string) (string, error) {
	dir, err := ServeStateDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, fmt.Sprintf("serve-%s.json", port)), nil
}

// ServeStatsPath resolves serve-<port>.stats.json inside the state dir.
func ServeStatsPath(port string) (string, error) {
	dir, err := ServeStateDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, fmt.Sprintf("serve-%s.stats.json", port)), nil
}

// PIDAlive reports whether pid currently belongs to a live process
// (signal 0 probe). EPERM counts as alive: the process exists, it just
// belongs to someone else.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}

// StartServeSideChannel publishes serve-<port>.json for the RUNNING srv
// (including the route set it was built with, so `jiso serve routes` can
// report the LIVE route set — UAT-02) and refreshes
// serve-<port>.stats.json every interval. The port is resolved via
// BoundPort once, so an ephemeral "0" start is still queryable under its
// real port and every later file operation names the same pair of files.
// The first snapshot is written inline, so a fast `serve stats` never sees
// a state file without its snapshot.
//
// The returned stop function waits for the writer goroutine to exit and
// then removes both files (a clean stop leaves no stale state); it is
// idempotent and safe to call when the channel never started.
func StartServeSideChannel(srv *server.Server, host, dbPath string, routes []config.MockRouteConfig, interval time.Duration) (statePath string, stop func() error, err error) {
	if interval <= 0 {
		interval = ServeStatsRefreshInterval
	}

	port, err := srv.BoundPort()
	if err != nil {
		return "", nil, fmt.Errorf("resolving bound port for state file: %w", err)
	}

	if host == "" {
		host = "0.0.0.0"
	}

	state := &ServeState{
		PID:       os.Getpid(),
		Host:      host,
		Port:      port,
		StartedAt: time.Now(),
		DB:        dbPath,
		Routes:    serveRoutesFromConfig(routes),
	}

	statePath, err = ServeStatePath(port)
	if err != nil {
		return "", nil, err
	}

	if err := writeJSONAtomic(statePath, state); err != nil {
		return "", nil, err
	}

	if err := writeServeSnapshot(srv, port); err != nil {
		_ = os.Remove(statePath)

		return "", nil, err
	}

	stopCh := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(exited)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				_ = writeServeSnapshot(srv, port)
			}
		}
	}()

	stop = func() error {
		once.Do(func() { close(stopCh) })
		<-exited

		return RemoveServeSideChannel(port)
	}

	return statePath, stop, nil
}

// writeServeSnapshot rewrites serve-<port>.stats.json with the current
// ServerStats view of the server.
func writeServeSnapshot(srv *server.Server, port string) error {
	path, err := ServeStatsPath(port)
	if err != nil {
		return err
	}

	view := NewServerStatsFromServerStats(srv.GetStats(), port, srv.GetHeaderType(), srv.IsRunning(), srv.ActiveConnections())
	view.PID = os.Getpid()
	snapshotAt := time.Now()
	view.SnapshotAt = &snapshotAt

	return writeJSONAtomic(path, view)
}

// RemoveServeSideChannel deletes the state and snapshot files for port,
// ignoring their absence.
func RemoveServeSideChannel(port string) error {
	var errs []error
	for _, path := range serveSideChannelPaths(port) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func serveSideChannelPaths(port string) []string {
	dir := "."
	if d, err := ServeStateDir(); err == nil {
		dir = d
	}

	return []string{
		filepath.Join(dir, fmt.Sprintf("serve-%s.json", port)),
		filepath.Join(dir, fmt.Sprintf("serve-%s.stats.json", port)),
	}
}

// ReadServeState loads serve-<port>.json. A missing file is not an error:
// state is nil and the path is still returned so callers can name it.
func ReadServeState(port string) (*ServeState, string, error) {
	path, err := ServeStatePath(port)
	if err != nil {
		return nil, "", err
	}

	state := &ServeState{}
	if err := readJSONFile(path, state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, path, nil
		}

		return nil, path, err
	}

	return state, path, nil
}

// ReadServeStatsSnapshot loads serve-<port>.stats.json.
func ReadServeStatsSnapshot(port string) (*ServerStats, string, error) {
	path, err := ServeStatsPath(port)
	if err != nil {
		return nil, "", err
	}

	view := &ServerStats{}
	if err := readJSONFile(path, view); err != nil {
		return nil, path, err
	}

	return view, path, nil
}

func readJSONFile(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	return nil
}

// WriteJSONAtomic exposes the side-channel's atomic write pattern (temp
// file in the target directory + rename) to PAR-309's `serve start --report`
// final-stats dump, so the report reader can never observe a partial file.
func WriteJSONAtomic(path string, v any) error {
	return writeJSONAtomic(path, v)
}

// writeJSONAtomic writes v as indented JSON to a temp file in the target
// directory and renames it into place, so readers never observe a partial
// file even while the ticker rewrites every second.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	writeAndClose := func() error {
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close() // rollback path: the write error is the reportable one

			return err
		}

		if err := tmp.Chmod(0o600); err != nil {
			_ = tmp.Close() // rollback path: the chmod error is the reportable one

			return err
		}

		return tmp.Close()
	}

	if err := writeAndClose(); err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("writing %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("renaming %s to %s: %w", tmpName, path, err)
	}

	return nil
}

// StateDir (added at E3-M1 merge) resolves the *general* jiso state dir for
// artifacts like the TUI debug log (TUI-409): JISO_STATE_DIR → XDG_STATE_HOME
// /jiso → os.UserConfigDir()/jiso, created 0700 on first use. It intentionally
// differs from ServeStateDir above: the serve *reader* must stay side-effect
// free (never MkdirAll) and PAR-304 pinned the home fallback to
// ~/.local/state for goldens; this creator prefers the platform config dir on
// macOS. Divergence documented in kanban E3-M1; unify only behind an explicit
// ticket that re-pins the PAR-304 goldens.
func StateDir() (string, error) {
	if v := os.Getenv("JISO_STATE_DIR"); v != "" {
		return ensureStateDir(v)
	}

	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return ensureStateDir(filepath.Join(v, "jiso"))
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user state dir: %w", err)
	}

	return ensureStateDir(filepath.Join(base, "jiso"))
}

// ensureStateDir creates dir (0700, parents included) and returns it.
func ensureStateDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create state dir %s: %w", dir, err)
	}

	return dir, nil
}
