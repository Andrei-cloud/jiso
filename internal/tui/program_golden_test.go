package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
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

// TestProgMouseClickInert pins the mouse plumbing end-to-end (Task 8.1,
// finding 9): the armed view writes the mouse-mode DECSET sequences
// (1002+1006, a mode change — NOT SGR, so the colorless goldens pinning
// the final frame content are untouched), a real SGR mouse report typed
// into the input pipe parses and reaches View.OnMouse, and a click over
// still-unregistered space is silently inert — the session keeps
// rendering and quits clean. Real hit registration arrives with
// Tasks 8.2–8.5; this proves the pipe is live underneath them.
func TestProgMouseClickInert(t *testing.T) {
	s := newProgSession(t, 80, 24)
	s.delay = 80 * time.Millisecond // first frame (and its OnMouse) flushed
	// SGR press+release at 1-based (5,5) = absolute cell (4,4), then quit.
	r := s.run(t, "\x1b[<0;5;5M\x1b[<0;5;5m\x03")
	wantClean(t, r)

	if !strings.Contains(r.raw, "\x1b[?1002h") || !strings.Contains(r.raw, "\x1b[?1006h") {
		t.Error("raw stream lacks the mouse DECSET sequences (1002/1006)")
	}
	if got := r.model.Current().ID(); got != "dashboard" {
		t.Errorf("dead-space click moved the page to %q, want dashboard (no hits registered yet)", got)
	}
	// The frame itself must not shift a single cell because of the mouse.
	checkProgGolden(t, "boot", r.frame)
}

// sgrWheelUp/sgrWheelDown encode an SGR (xterm 1006) wheel report at the
// ABSOLUTE 0-based cell (x,y): the terminal spells rows/columns 1-based,
// button 64 = wheel up, 65 = wheel down (ultraviolet decodeMouseButton).
func sgrWheelUp(x, y int) string   { return fmt.Sprintf("\x1b[<64;%d;%dM", x+1, y+1) }
func sgrWheelDown(x, y int) string { return fmt.Sprintf("\x1b[<65;%d;%dM", x+1, y+1) }

// TestWheelScrollsServerLog proves the wheel end-to-end (Task 8.2b,
// finding 9): a real SGR wheel report typed into the input pipe resolves
// against the §G hit-map region, dispatches scrollMsg at the root, and
// moves the SAME logScroll offset the j/k keys drive — while a wheel over
// a cell outside the log pane stays inert. The rendered log window must
// move with the offset.
func TestWheelScrollsServerLog(t *testing.T) {
	// Baseline: §G with a log taller than the pane, following the newest.
	base := newProgSession(t, 80, 24)
	seedServerLogLines(base.m, 30)
	r0 := base.runScripted(t, 80*time.Millisecond, "4", "\x03")
	wantClean(t, r0)

	if got := r0.model.Current().ID(); got != "server" {
		t.Fatalf("after '4' current page = %q, want server", got)
	}
	if !strings.Contains(r0.frame, "log line 30") {
		t.Fatalf("baseline frame must render the newest log line at the pane bottom:\n%s", r0.frame)
	}
	if got := r0.model.server.LogScroll(); got != 0 {
		t.Fatalf("baseline log offset = %d, want 0 (following the newest)", got)
	}

	// The wheel cell is the CENTRE of the region the page published from
	// its last render — but the published rect is CONTENT-RELATIVE, so
	// the test translates it by frame.ContentOrigin (the same offset
	// hitMap.addAbs applies) before typing it as an SGR report: these are
	// genuinely ABSOLUTE terminal cells. If the translation were dropped
	// from addAbs the reports would land outside the registered rect and
	// the wheel would stop moving the log. The outside cell sits one row
	// ABOVE the box's drawn top border — the row above is page head with
	// no region registered, so a wheel there must stay inert.
	regions := r0.model.server.ScrollRegions()
	if len(regions) != 1 || regions[0].ID != pages.RegionServerLog {
		t.Fatalf("§G must publish exactly the %q region, got %#v", pages.RegionServerLog, regions)
	}
	ox, oy := r0.model.contentOrigin()
	rel := regions[0].Rect
	abs := geom.Rect{X: rel.X + ox, Y: rel.Y + oy, W: rel.W, H: rel.H}
	wx, wy := abs.X+abs.W/2, abs.Y+abs.H/2
	outX, outY := wx, abs.Y-1
	if abs.Contains(outX, outY) {
		t.Fatalf("outside cell (%d,%d) is inside the log rect %v", outX, outY, abs)
	}

	// Wheel session: two wheel-UPs walk the window back through history
	// (offset 0→2), one wheel-DOWN advances it toward the newest (2→1),
	// and a wheel-UP over the outside cell must change nothing.
	s := newProgSession(t, 80, 24)
	seedServerLogLines(s.m, 30)
	r := s.runScripted(t, 80*time.Millisecond,
		"4",
		sgrWheelUp(wx, wy)+sgrWheelUp(wx, wy),
		sgrWheelDown(wx, wy),
		sgrWheelUp(outX, outY),
		"\x03",
	)
	wantClean(t, r)

	if got := r.model.server.LogScroll(); got != 1 {
		t.Fatalf("after 2x wheel-up + 1x wheel-down + 1x wheel-up outside the pane, log offset = %d, want 1", got)
	}
	// The rendered log window moved with the offset: the newest line left
	// the pane and the row just above the fold is now visible.
	if strings.Contains(r.frame, "log line 30") {
		t.Errorf("scrolled frame still shows the newest line:\n%s", r.frame)
	}
	if !strings.Contains(r.frame, "log line 29") {
		t.Errorf("scrolled frame must show the shifted window (log line 29):\n%s", r.frame)
	}
	if r.frame == r0.frame {
		t.Error("the wheel did not change the rendered frame")
	}
	checkProgGolden(t, "wheel_server_log", r.frame)
}

// TestWheelFrozenByModal pins Task 8.2c fix 1 end-to-end: with the
// command palette open over §G, the SAME absolute wheel cell that moves
// the log in TestWheelScrollsServerLog must leave the page frozen — the
// palette owns the screen and draws over the page, so the wheel over
// the page area behind it scrolls nothing.
func TestWheelFrozenByModal(t *testing.T) {
	// Baseline: read the region geometry from a plain §G run (the page
	// publishes it content-relative; the wheel cell is the centre
	// translated by frame.ContentOrigin, exactly as in the 8.2b test).
	base := newProgSession(t, 80, 24)
	seedServerLogLines(base.m, 30)
	r0 := base.runScripted(t, 80*time.Millisecond, "4", "\x03")
	wantClean(t, r0)

	regions := r0.model.server.ScrollRegions()
	if len(regions) != 1 || regions[0].ID != pages.RegionServerLog {
		t.Fatalf("§G must publish exactly the %q region, got %#v", pages.RegionServerLog, regions)
	}
	ox, oy := r0.model.contentOrigin()
	rel := regions[0].Rect
	wx, wy := rel.X+ox+rel.W/2, rel.Y+oy+rel.H/2

	// Wheel session: the palette opens over §G, then two wheel-UPs land
	// on the log pane's absolute centre. The page behind stays frozen:
	// LogScroll must still be 0 when the session ends.
	s := newProgSession(t, 80, 24)
	seedServerLogLines(s.m, 30)
	r := s.runScripted(t, 80*time.Millisecond,
		"4",
		":",
		sgrWheelUp(wx, wy)+sgrWheelUp(wx, wy),
		"\x03",
	)
	wantClean(t, r)

	if r.model.pal == nil {
		t.Fatal("precondition: the palette must be open when the wheel lands")
	}
	if got := r.model.server.LogScroll(); got != 0 {
		t.Fatalf("wheel over the page behind an open palette moved logScroll to %d, want 0 (modal freeze)", got)
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
