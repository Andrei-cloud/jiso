package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
)

// Layer-2 golden program tests: real tea.Program sessions over
// pipes at the pinned 80×24, goldens under testdata/program/.

// TestProgBootGolden: boot → first frame is the §A dashboard in full
// 80×24 chrome; ctrl+c then returns nil from Run ('q' now arms the
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

// TestProgErrModal: the error screen renders centered over the §B
// transactions page: the multi-line body windows ten content rows at a
// time (six `j` presses walk the window down), the footer swaps the page
// keys for the modal's own, and the digits stay inert while it is open.
func TestProgErrModal(t *testing.T) {
	s := newProgSession(t, 80, 24)
	_, _ = s.m.Update(ch('2')) // land on §B first; the open modal eats later jumps
	fixture := errModalFixture(25)
	s.m.openErrorModal("cannot load transaction file", errors.New(fixture))

	r := s.run(t, "jjjjjj\x03")
	wantClean(t, r)

	if r.model.errModal == nil {
		t.Fatal("the error modal must stay open until ctrl+c ends the session")
	}
	if got := r.model.Current().ID(); got != "transactions" {
		t.Errorf("j-typing moved the frozen page to %q, want transactions", got)
	}
	frame := ansi.Strip(r.frame)
	if !strings.Contains(frame, "cannot load transaction file") {
		t.Errorf("frame lacks the error title:\n%s", frame)
	}
	if !strings.Contains(frame, "line 07") || !strings.Contains(frame, "line 16") {
		t.Errorf("frame must show the ten-row window 07–16:\n%s", frame)
	}
	if strings.Contains(frame, "line 06") || strings.Contains(frame, "line 17") {
		t.Errorf("frame must hide the rows outside the window:\n%s", frame)
	}
	if !strings.Contains(frame, "TRANSACTIONS") {
		t.Errorf("frame lacks the §B page under the modal:\n%s", frame)
	}
	checkProgGolden(t, "err_modal", r.frame)
}

// TestProgPaletteSend: ':' opens the palette, typing "send" filters to
// the send wizard (the wizard replaced the legacy
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
// the program actually terminates (quit is confirmed first).
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

// end-to-end mouse plumbing: the armed view writes the mouse DECSETs
// (1002/1006, not SGR), a typed SGR report reaches View.OnMouse, and a
// click over unregistered space is inert with the session quitting clean.
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

// f9Bytes is F9 as the terminal spells it on the wire.
const f9Bytes = "\x1b[20~"

// a mouse-off model writes NO mouse DECSET (the terminal keeps native
// text selection) and a hand-typed SGR wheel report changes nothing.
func TestProgMouseOffReleasesDecset(t *testing.T) {
	// region geometry comes from a plain mouse-on §G run (the rect only exists after the page rendered)
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

	s := newProgSession(t, 80, 24)
	s.m.mouseEnabled = false
	seedServerLogLines(s.m, 30)
	// Go to §G, wheel UP twice over the log-pane centre, then quit.
	r := s.runScripted(t, 80*time.Millisecond,
		"4",
		sgrWheelUp(wx, wy)+sgrWheelUp(wx, wy),
		"\x03",
	)
	wantClean(t, r)

	for _, seq := range []string{"\x1b[?1002h", "\x1b[?1006h"} {
		if strings.Contains(r.raw, seq) {
			t.Errorf("mouse-off session still emitted the DECSET enable %q", seq)
		}
	}
	if got := r.model.server.LogScroll(); got != 0 {
		t.Fatalf("wheel over §G log with the mouse off moved the offset to %d, want 0 (inert)", got)
	}
	if !strings.Contains(r.frame, "log line 30") {
		t.Errorf("mouse-off frame must still render the newest line (nothing moved):\n%s", r.frame)
	}
}

// end-to-end toggle: mouse arms at boot, F9 releases the terminal (further
// wheels inert), F9 again re-arms.
func TestProgF9TogglesDecset(t *testing.T) {
	// region geometry comes from a plain §G run (content-relative rect translated by the content origin)
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

	s := newProgSession(t, 80, 24)
	seedServerLogLines(s.m, 30)
	r := s.runScripted(t, 80*time.Millisecond,
		"4",
		sgrWheelUp(wx, wy), // mouse on: offset 0 → 1
		f9Bytes,            // release the terminal
		sgrWheelUp(wx, wy), // inert: offset stays 1
		f9Bytes,            // re-arm
		sgrWheelUp(wx, wy), // mouse on again: offset 1 → 2
		"\x03",
	)
	wantClean(t, r)

	for _, seq := range []string{"\x1b[?1002h", "\x1b[?1006h"} {
		if !strings.Contains(r.raw, seq) {
			t.Errorf("mouse-on frames must emit the DECSET enable %q", seq)
		}
	}
	for _, seq := range []string{"\x1b[?1002l", "\x1b[?1006l"} {
		if !strings.Contains(r.raw, seq) {
			t.Errorf("F9 must write the DECSET disable %q to release the terminal", seq)
		}
	}
	if got := r.model.Current().ID(); got != "server" {
		t.Errorf("F9 moved the page to %q, want server (global key, no navigation)", got)
	}
	if got := r.model.server.LogScroll(); got != 2 {
		t.Fatalf("after wheel-on + wheel-off + wheel-on, log offset = %d, want 2 (off step inert)", got)
	}
}

// sgrWheelUp/sgrWheelDown encode an SGR (xterm 1006) wheel report at the
// absolute 0-based cell (x,y) — the terminal spells it 1-based.
func sgrWheelUp(x, y int) string   { return fmt.Sprintf("\x1b[<64;%d;%dM", x+1, y+1) }
func sgrWheelDown(x, y int) string { return fmt.Sprintf("\x1b[<65;%d;%dM", x+1, y+1) }

// a typed SGR wheel report resolves the §G region, moves the same
// logScroll offset the keys drive, and the rendered window moves with it;
// a wheel outside the pane stays inert.
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

	// the wheel cell is the region centre translated by ContentOrigin (the
	// published rect is content-relative; the reports must be absolute);
	// the outside cell sits one row above the drawn box top
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

	// two wheel-ups walk back through history, one down advances, one up outside changes nothing
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
	// the rendered log window moved with the offset
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

// with the palette open over §G, the same absolute wheel cell that moves
// the log leaves the page behind frozen.
func TestWheelFrozenByModal(t *testing.T) {
	// baseline: read the region geometry from a plain §G run
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

	// the palette opens over §G, then two wheel-ups land on the log centre; the page stays frozen
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
