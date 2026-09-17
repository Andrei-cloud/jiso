package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
)

// debug plumbing tests. All hermetic: JISO_STATE_DIR points at a
// t.TempDir, so the lifecycle log can never touch the developer's real
// state directory, and programs run over the pipe harness (no PTY).

func keyCh(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Text: string(c)} }

func keySpecial(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// readDebugLog returns the lifecycle log lines (trailing blank trimmed).
func readDebugLog(t *testing.T, dir string) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dir, tuiLogFileName))
	if err != nil {
		t.Fatalf("read %s: %v", tuiLogFileName, err)
	}

	return strings.FieldsFunc(string(raw), func(r rune) bool { return r == '\n' })
}

func wantDebugLine(t *testing.T, lines []string, want string) {
	t.Helper()

	for _, l := range lines {
		if strings.Contains(l, want) {
			return
		}
	}

	t.Errorf("lifecycle log missing line containing %q; lines:\n%s", want, strings.Join(lines, "\n"))
}

// TestDebugLogOnQuitRun: program-level milestone contract — a quit run
// (keys "2q": jump to send, then quit at root) logs program start first,
// the page transition in the middle, and program exit reason=quit last.
func TestDebugLogOnQuitRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(debugEnv, "1")
	t.Setenv("JISO_STATE_DIR", dir)

	m := NewRootModel(nil)

	ctx, cancel := context.WithTimeout(context.Background(), progTimeout)
	defer cancel()

	in, pw := openPipeInput(t)
	out := &syncBuf{}

	go func() { _, _ = io.WriteString(pw, "2\x03") }()

	if err := run(ctx, m, runOptions(in, out)...); err != nil {
		t.Fatalf("quit run returned %v", err)
	}

	_ = pw.Close()

	lines := readDebugLog(t, dir)
	if len(lines) < 3 {
		t.Fatalf("want >=3 lifecycle lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	if !strings.Contains(lines[0], "program start") {
		t.Errorf("first line = %q, want program start", lines[0])
	}

	if last := lines[len(lines)-1]; !strings.Contains(last, "program exit reason=quit") {
		t.Errorf("last line = %q, want program exit reason=quit", last)
	}

	wantDebugLine(t, lines, "page jump from=dashboard to=transactions")

	// Prefix contract: stdlib stamp + "tui: " prefix from LogToFileWith.
	for _, l := range lines {
		if !strings.Contains(l, "tui: ") {
			t.Errorf("line %q lacks the tui: prefix", l)
		}
	}
}

// TestDebugOffWritesNoLogFile: with $JISO_DEBUG unset, run must not create
// the state dir file at all — no side channel when the knob is off.
func TestDebugOffWritesNoLogFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(debugEnv, "")
	t.Setenv("JISO_STATE_DIR", dir)

	m := NewRootModel(nil)

	ctx, cancel := context.WithTimeout(context.Background(), progTimeout)
	defer cancel()

	in, pw := openPipeInput(t)
	out := &syncBuf{}

	go func() { _, _ = io.WriteString(pw, "\x03") }()

	if err := run(ctx, m, runOptions(in, out)...); err != nil {
		t.Fatalf("quit run returned %v", err)
	}

	_ = pw.Close()

	if _, err := os.Stat(filepath.Join(dir, tuiLogFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("debug-off created %s (stat err = %v)", tuiLogFileName, err)
	}
}

// TestDebugPaletteAndBridgeLines: unit-level coverage of the remaining
// milestones (palette open/exec/close, bridge start/stop) without a
// program, using the same hooks run drives.
func TestDebugPaletteAndBridgeLines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(debugEnv, "1")
	t.Setenv("JISO_STATE_DIR", dir)

	dbg := newDebugLogger()
	if dbg == nil {
		t.Fatal("newDebugLogger off with JISO_DEBUG=1")
	}

	m := NewRootModel(nil)
	m.setDebug(dbg)

	m.Update(keyCh(':'))
	for _, r := range "stress" {
		m.Update(keyCh(r))
	}

	if _, cmd := m.Update(keySpecial(tea.KeyEnter)); cmd == nil {
		t.Fatal("enter on a filtered palette must submit")
	}

	m.Update(keyCh(':'))
	m.Update(keySpecial(tea.KeyEscape))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.setEventWiring(ctx, func(tea.Msg) {})
	m.SetEventSource(make(chan events.Event))

	if cmd := m.armBridgeCmd(); cmd == nil {
		t.Fatal("armBridgeCmd returned nil with a pending source")
	}

	m.stopBridge()
	dbg.close()

	lines := readDebugLog(t, dir)
	wantDebugLine(t, lines, "palette open")
	wantDebugLine(t, lines, "palette exec id=goto.workers")
	wantDebugLine(t, lines, "palette close")
	wantDebugLine(t, lines, "bridge start")
	wantDebugLine(t, lines, "bridge stop")
}

// TestDebugLoggerNilSafe: the disabled logger must swallow every hook call.
func TestDebugLoggerNilSafe(t *testing.T) {
	t.Setenv(debugEnv, "")

	var dbg *debugLogger

	if dbg != nil {
		t.Fatal("debug logger not off with JISO_DEBUG unset")
	}

	dbg.logf("must not panic %s", "x")

	dbg.close()
}

// TestExitReasonNamesEveryProgramExit: the exit line vocabulary.
func TestExitReasonNamesEveryProgramExit(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "quit"},
		{tea.ErrProgramKilled, "killed: " + tea.ErrProgramKilled.Error()},
		{tea.ErrProgramPanic, "panic: " + tea.ErrProgramPanic.Error()},
		{errors.New("boom"), "error: boom"},
	}

	for _, tc := range cases {
		if got := exitReason(tc.err); got != tc.want {
			t.Errorf("exitReason(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
