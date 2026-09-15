package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Layer-2 golden program tests (TUI-407): real tea.Program sessions over
// pipes at the pinned 80×24, goldens under testdata/program/.

// TestProgBootGolden: boot → first frame is the §A dashboard in full
// 80×24 chrome; ctrl+c then returns nil from Run (UAT: 'q' now arms the
// quit confirmation, so goldens exit through the immediate escape).
func TestProgBootGolden(t *testing.T) {
	s := newProgSession(t, 80, 24)
	r := s.run(t, "\x03")
	wantClean(t, r)

	if !strings.Contains(r.frame, "CONNECTION") {
		t.Errorf("boot frame lacks the dashboard CONNECTION card:\n%s", r.frame)
	}
	if !strings.HasPrefix(r.frame, "+- jiso dev") {
		// Wireframe A1: the app label is embedded in the rounded top rule.
		t.Errorf("boot frame must open with the app label in the top rule:\n%s", r.frame)
	}
	assertSectionsFillFrameWidth(t, r, 80)
	checkProgGolden(t, "boot", r.frame)
}

// slotSectionTitles is the section title each hotkey slot renders (the
// page's own first content line), keyed by page id.
var slotSectionTitles = map[string]string{
	"transactions": "TRANSACTIONS",
	"scenarios":    "SCENARIOS",
	"server":       "MOCK SERVER",
	"workers":      "WORKERS & STRESS",
	"sessions":     "SESSIONS",
	"analyze":      "PCAP ANALYZE",
	"ctf":          "VISA BASE II",
}

// TestProgPagesReachable: hotkeys 2..8 each land the program on the
// registry page whose title shows in the frame (hotkey 1 == boot page,
// pinned by TestProgBootGolden). One fresh program per page.
func TestProgPagesReachable(t *testing.T) {
	for i := 1; i < len(PageIDs); i++ {
		t.Run(PageIDs[i], func(t *testing.T) {
			s := newProgSession(t, 80, 24)
			r := s.run(t, string(rune('1'+i))+"\x03")
			wantClean(t, r)

			title := slotSectionTitles[PageIDs[i]]
			if title == "" {
				t.Fatalf("no section title pinned for slot %q", PageIDs[i])
			}
			if !strings.Contains(r.frame, title) {
				t.Errorf("page %d frame lacks title %q:\n%s", i+1, title, r.frame)
			}
			if got := r.model.Current().ID(); got != PageIDs[i] {
				t.Errorf("after %c current page = %q, want %q", '1'+i, got, PageIDs[i])
			}
			checkProgGolden(t, "page_"+PageIDs[i], r.frame)
		})
	}
}

// TestProgHelpOverlay: '?' opens the §M overlay — a frame-level modal,
// NOT a page push (the stack stays at depth 1). The frame is captured
// with the overlay open by letting the session run without a quit key:
// the harness's final timeout kills it (ErrProgramKilled) — the same
// kill that guarantees no golden test can hang CI.
func TestProgHelpOverlay(t *testing.T) {
	s := newProgSession(t, 80, 24)
	r := s.runTimeout(t, "?", 250*time.Millisecond)

	if !errors.Is(r.err, tea.ErrProgramKilled) {
		t.Fatalf("no-quit run err = %v, want ErrProgramKilled (anti-hang contract)", r.err)
	}
	if !strings.Contains(r.frame, "HELP") {
		t.Errorf("help frame lacks the HELP box:\n%s", r.frame)
	}
	if !strings.Contains(r.frame, "context: Dashboard page") {
		t.Errorf("help frame lacks the current-page context:\n%s", r.frame)
	}
	if got := r.model.StackDepth(); got != 1 {
		t.Errorf("with ? the depth = %d, want 1 (overlay modal, not a page push)", got)
	}
	buf := &syncBuf{}
	buf.b.WriteString(r.raw)
	wantAltScreenRestore(t, buf)
	checkProgGolden(t, "help_overlay", r.frame)
	// esc-closes-overlay is layer-1 territory (TestHelpOverlayOpenToggleClose).
}

// TestProgPaletteSend: ':' opens the palette, typing "send" filters to
// the send wizard (proposal 04 §B — the wizard replaced the legacy
// transactions-jump for this query), Enter opens it over the current
// page; ctrl+c quits with the wizard on screen (the captured frame).
func TestProgPaletteSend(t *testing.T) {
	s := newProgSession(t, 80, 24)
	r := s.run(t, ":send\r\x03")
	wantClean(t, r)

	if got := r.model.Current().ID(); got != "dashboard" {
		t.Errorf("after :send+Enter current page = %q, want dashboard (wizard is an overlay)", got)
	}
	if r.model.wizard == nil {
		t.Fatal("Enter on :send must open the send wizard")
	}
	if !strings.Contains(r.frame, "SEND") || !strings.Contains(r.frame, "connect") {
		t.Errorf("frame lacks the wizard title/rail:\n%s", r.frame)
	}
	// The mode chip lives on the header line; the footer legitimately
	// mentions ": palette" as a hint.
	if header := strings.SplitN(r.frame, "\n", 2)[0]; strings.Contains(header, "palette") {
		t.Errorf("palette mode chip leaked after Enter:\n%s", header)
	}
	checkProgGolden(t, "palette_send", r.frame)
}

// TestProgQuitReturns: 'q' + confirm 'y' at root makes Run return nil —
// the program actually terminates (UAT: quit is confirmed first).
func TestProgQuitReturns(t *testing.T) {
	s := newProgSession(t, 80, 24)
	r := s.run(t, "qy")
	wantClean(t, r)

	if r.err != nil {
		t.Fatalf("q at root: Run err = %v, want nil", r.err)
	}
	if r.model.bridge != nil {
		t.Error("bridge armed without an app; expected nil wiring")
	}
}

// TestProgPanicExit: a page that panics inside Update takes the program
// down on v2's recovered error-exit path — Run returns ErrProgramPanic
// and the renderer still left the alt screen (terminal restored). This
// is the real-program counterpart of panic_lifecycle_test.go.
func TestProgPanicExit(t *testing.T) {
	s := newProgSession(t, 80, 24)
	s.m.Replace(panicPage{})
	s.delay = 80 * time.Millisecond // let the first flush write alt-screen enter

	r := s.run(t, "x")
	if !errors.Is(r.err, tea.ErrProgramPanic) {
		t.Fatalf("panic page Run err = %v, want ErrProgramPanic", r.err)
	}
	buf := &syncBuf{}
	buf.b.WriteString(r.raw)
	wantAltScreenRestore(t, buf)

	checkProgGolden(t, "panic_exit", r.frame)
}
