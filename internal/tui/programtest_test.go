package tui

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

// Layer-2 golden program harness.
//
// FINDING: charm.land/bubbletea/x/teatest (and /v2/x/teatest) vanity paths
// exist but resolve to NO published version ("missing go.mod at revision"
// on @main, no tags), and bubbletea v2.0.9 ships no x/teatest directory —
// teatest/v2 does not exist yet. This file is the thinnest alternative:
// the real tea.Program driven over in-memory pipes with the injection
// idiom from bubbletea's own tea_test.go (WithInput/WithOutput/
// WithWindowSize/WithColorProfile), pinned size (v2 pushes
// tea.WindowSizeMsg at boot before any input — verified empirically),
// a pinned colorless theme via setTheme (theme.Default is a process
// sync.Once, so t.Setenv alone is not enough), and ALWAYS a
// context.WithTimeout so a hung program surfaces as ErrProgramKilled
// instead of hanging CI.
//
// Final-frame capture boundary: reconstructing the last frame from the
// renderer byte-stream would need a terminal emulator (v2's cursed
// renderer repaints fully once after ansi home+clear, then diffs with
// cursor moves). Instead the golden holds the program's FINAL model View
// after Run returns — the exact content the last frame painted — while
// the raw stream is still asserted for the renderer contract (alt-screen
// enter/exit) via wantRawAltScreenRestore.

var progUpdate = flag.Bool("update", false, "update golden files")

// progTimeout is the harness-wide final timeout: every program run must
// finish (or be killed) within it, so no golden test can hang CI.
const progTimeout = 5 * time.Second

// clockRe matches the hard-status clock slot (frameProps formats
// time.Now as 15:04:05); goldens pin "HH:MM:SS" instead.
var clockRe = regexp.MustCompile(`\d{2}:\d{2}:\d{2}`)

// progResult is what one full program session yields.
type progResult struct {
	err   error      // error from Program.Run (nil on clean quit)
	frame string     // final model frame, clock-normalised
	raw   string     // raw renderer bytes (alt-screen contract checks)
	model *RootModel // post-run model for state assertions
}

// progSession drives one real program over pipes at a pinned window size.
type progSession struct {
	m      *RootModel
	pr     *io.PipeReader
	pw     *io.PipeWriter
	out    *syncBuf
	ctx    context.Context
	cancel context.CancelFunc
	w, h   int

	// delay postpones the typed keys so the fps ticker's first flush
	// (~16ms) has already written the alt-screen enter — panics fired
	// before that never reach the renderer (see lifecycle_helpers_test.go
	// panicPage note).
	delay time.Duration
}

func newProgSession(t *testing.T, width, height int) *progSession {
	t.Helper()

	// Belt-and-braces profile pin (the real determinism is setTheme below;
	// this also covers any theme.Default use outside the frame seam).
	t.Setenv("JISO_ASCII", "1")
	t.Setenv("NO_COLOR", "1")
	// Hermetic state dir: the last-connection prefill (and the STAN
	// persistence dir) must never read or write the real XDG state
	// dir, or goldens drift with whatever the developer connected to
	// last.
	t.Setenv("JISO_STATE_DIR", t.TempDir())

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	m := NewRootModel(nil)
	m.theme = theme.NewWith(colorprofile.ASCII, true)

	ctx, cancel := context.WithTimeout(context.Background(), progTimeout)
	t.Cleanup(cancel)

	return &progSession{m: m, pr: pr, pw: pw, out: &syncBuf{}, ctx: ctx, cancel: cancel, w: width, h: height}
}

// run types keys into the input pipe and joins Program.Run. keys must
// contain the key that quits (usually a trailing "q") or the harness
// timeout surfaces the run as ErrProgramKilled.
func (s *progSession) run(t *testing.T, keys string) progResult {
	t.Helper()

	go func() {
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
		if keys != "" {
			_, _ = io.WriteString(s.pw, keys) // blocks until v2 reads it
		}
	}()

	err := run(s.ctx, s.m,
		tea.WithInput(s.pr),
		tea.WithOutput(s.out),
		tea.WithWindowSize(s.w, s.h),
		tea.WithColorProfile(colorprofile.ASCII),
		tea.WithoutSignalHandler(),
	)
	_ = s.pw.Close() // unblock v2's input goroutine (existing idiom)

	return progResult{err: err, frame: s.finalFrame(), raw: s.out.String(), model: s.m}
}

// runTimeout runs the session with a per-run final timeout (overriding
// progTimeout) and keys that need NOT quit: when keys leave the program
// running, Run returns tea.ErrProgramKilled at the deadline — this is the
// anti-hang CI guarantee observable from the test side.
func (s *progSession) runTimeout(t *testing.T, keys string, d time.Duration) progResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	s.ctx = ctx

	go func() {
		if keys != "" {
			_, _ = io.WriteString(s.pw, keys)
		}
	}()

	err := run(ctx, s.m,
		tea.WithInput(s.pr),
		tea.WithOutput(s.out),
		tea.WithWindowSize(s.w, s.h),
		tea.WithColorProfile(colorprofile.ASCII),
		tea.WithoutSignalHandler(),
	)
	_ = s.pw.Close()

	return progResult{err: err, frame: s.finalFrame(), raw: s.out.String(), model: s.m}
}

// runScripted types chunks with a pause before each (mouse events resolve
// against the last flushed view). The final chunk must quit.
func (s *progSession) runScripted(t *testing.T, gap time.Duration, chunks ...string) progResult {
	t.Helper()

	go func() {
		for _, c := range chunks {
			time.Sleep(gap)
			_, _ = io.WriteString(s.pw, c) // blocks until v2 reads it
		}
	}()

	return s.run(t, "")
}

// finalFrame renders the model's post-run View with the clock slot
// normalised: the golden pins layout + text, not wall time.
func (s *progSession) finalFrame() string {
	v := s.m.View()

	return clockRe.ReplaceAllString(strings.TrimRight(v.Content, "\n"), "HH:MM:SS")
}

// wantClean asserts the session quit cleanly and left the alt screen.
func wantClean(t *testing.T, r progResult) {
	t.Helper()

	if r.err != nil {
		t.Fatalf("program Run err = %v (harness kill? frame:\n%s)", r.err, r.frame)
	}
	buf := &syncBuf{}
	buf.b.WriteString(r.raw)
	wantAltScreenRestore(t, buf)
}

// checkProgGolden compares against testdata/program/<name>.golden,
// honouring -update (same workflow as the frame/theme packages).
func checkProgGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "program", name+".golden")

	if *progUpdate {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui -run Prog -update)", path, err)
	}
	if got+"\n" != string(want) {
		t.Errorf("golden %s mismatch\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}
