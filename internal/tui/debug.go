package tui

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
)

// Env knobs (TUI-409). Read here, at the TUI edge, mirroring the CLI-104
// spellings; internal/tui must not import internal/cli (imports_guard).
const (
	debugEnv       = "JISO_DEBUG"
	profileEnv     = "JISO_PROFILE"
	profilePortEnv = "JISO_PROFILE_PORT"

	tuiLogFileName = "tui.log"
)

// debugLogger writes TUI lifecycle milestones to <state>/tui.log while
// $JISO_DEBUG is truthy. The nil *debugLogger is the disabled state: every
// hook calls the nil-safe methods below, so Update stays I/O-free (and
// allocation-free on the hot path) whenever debug is off.
//
// Content contract: fixed milestone lines only — never config file
// contents, connection payloads, or anything that could carry a secret.
//
// Line format (stdlib log stamps date+time; "tui: " is the prefix handed
// to tea.LogToFileWith):
//
//	2026/09/07 12:00:00 tui: program start
//	2026/09/07 12:00:00 tui: page jump from=status to=send
//	2026/09/07 12:00:01 tui: program exit reason=quit
type debugLogger struct {
	l *log.Logger
	f *os.File
}

// newDebugLogger opens the lifecycle log when $JISO_DEBUG is truthy, else
// returns nil (disabled). Failures are silent on purpose: the terminal is
// about to be captured by the program, so there is no better channel than
// the log file itself, and a broken side channel must never kill the TUI.
func newDebugLogger() *debugLogger {
	if !envTruthy(os.Getenv(debugEnv)) {
		return nil
	}

	dir, err := app.StateDir()
	if err != nil {
		return nil
	}

	// LogToFileWith, not LogToFile: identical open semantics (O_APPEND,
	// 0600, prefix normalisation) but redirected into a private
	// *log.Logger instead of log.Default() — a single debug session must
	// not rewire the process-global logger out from under every other
	// log.Printf caller in the binary.
	l := log.New(io.Discard, "", log.LstdFlags)

	f, err := tea.LogToFileWith(filepath.Join(dir, tuiLogFileName), "tui: ", l)
	if err != nil {
		return nil
	}

	return &debugLogger{l: l, f: f}
}

// logf writes one milestone line; nil-safe.
func (d *debugLogger) logf(format string, args ...any) {
	if d == nil || d.f == nil {
		return
	}

	d.l.Printf(format, args...)
}

// close closes the log file and deadens the logger (a stdlib log.Logger
// whose file is gone echoes to os.Stderr, which would pollute the CLI
// stream); nil-safe.
func (d *debugLogger) close() {
	if d == nil || d.f == nil {
		return
	}

	_ = d.f.Close()

	d.f = nil
	d.l.SetOutput(io.Discard)
}

// envTruthy accepts the same spellings as the CLI-104 debug resolution
// (1/true/yes/on vs 0/false/no/off); anything else counts as unset.
func envTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// exitReason names why program.Run returned, for the final log line.
func exitReason(err error) string {
	switch {
	case err == nil:
		return "quit"
	case errors.Is(err, tea.ErrProgramPanic):
		return "panic: " + err.Error()
	case errors.Is(err, tea.ErrProgramKilled):
		return "killed: " + err.Error()
	default:
		return "error: " + err.Error()
	}
}
