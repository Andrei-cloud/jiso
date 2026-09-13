package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// countingPage records every WindowSizeMsg the page stack was actually
// relaid out with — the expensive half of a resize (forwardAll to all
// stack pages).
type countingPage struct {
	id    string
	sizes [][2]int
}

func (p countingPage) ID() string { return p.id }

func (p countingPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		p.sizes = append(p.sizes, [2]int{ws.Width, ws.Height})
	}

	return p, nil
}

func (p countingPage) View() tea.View   { return tea.NewView(p.id) }
func (p countingPage) Hints() []KeyHint { return nil }

func topCounting(t *testing.T, m *RootModel) countingPage {
	t.Helper()

	p, ok := m.Current().(countingPage)
	if !ok {
		t.Fatalf("current page = %T, want countingPage", m.Current())
	}

	return p
}

// burstSizes is the "10 sizes in one burst" from the ticket: a drag
// sequence ending far from where it started.
var burstSizes = [][2]int{
	{81, 24},
	{85, 26},
	{90, 28},
	{95, 30},
	{100, 32},
	{105, 34},
	{110, 36},
	{115, 38},
	{120, 40},
	{125, 42},
}

// TestWindowSizeBurstCoalescesRelayouts pins the coalescing contract
// (design §lifecycle non-negotiable 3): during a 10-size burst the page
// stack relays out at most once (leading edge), View tracks the last size
// immediately, and one trailing flush lands the final size. v2 itself only
// coalesces the SIGWINCH signal (signals_unix.go:15-33, cap-1 channel) and
// re-queries the live size per signal (tty.go:105-127) — each delivered
// WindowSizeMsg would otherwise be a full relayout, hence the root window.
func TestWindowSizeBurstCoalescesRelayouts(t *testing.T) {
	m := NewRootModel(nil)
	m.resizeWindow = time.Hour // keep the trailing flush cmd unfired
	m.Replace(countingPage{id: "count"})

	first, last := burstSizes[0], burstSizes[len(burstSizes)-1]

	for _, s := range burstSizes {
		m.Update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})

		// Non-negotiable 3 "from current frame": View always matches the
		// latest size, even while the page relayout is coalesced.
		if lines := strings.Split(m.View().Content, "\n"); len(lines) != s[1] {
			t.Fatalf("%dx%d: view is %d lines", s[0], s[1], len(lines))
		}
	}

	if got := topCounting(t, m).sizes; len(got) != 1 || got[0] != first {
		t.Fatalf("burst relayouts = %v, want exactly the leading %v", got, first)
	}

	// The trailing flush (what resizeFlushCmd returns after the window)
	// applies the last burst size exactly once.
	m.Update(resizeFlushMsg{})

	if got := topCounting(t, m).sizes; len(got) != 2 || got[1] != last {
		t.Fatalf("after flush: relayouts = %v, want [%v %v]", got, first, last)
	}
	if m.resizePending {
		t.Error("flush did not clear the pending flag")
	}
}

// TestResizeFlushCmdFiresOncePerWindow covers the timing side: the leading
// Update returns exactly one flush Cmd, a pending resize schedules no
// second Cmd, and executing the Cmd yields resizeFlushMsg within a bounded
// window that then relayouts with the latest size.
func TestResizeFlushCmdFiresOncePerWindow(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.resizeWindow = 10 * time.Millisecond
	m.Replace(countingPage{id: "count"})

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("leading resize returned no flush cmd")
	}

	_, cmd2 := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd2 != nil {
		t.Errorf("pending resize scheduled a second relayout cmd: %T", cmd2)
	}

	start := time.Now()
	msg := cmd() // the program runs Cmds in their own goroutine
	if _, ok := msg.(resizeFlushMsg); !ok {
		t.Fatalf("flush cmd yielded %T, want resizeFlushMsg", msg)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("flush cmd took %v, want ≈ the coalesce window", elapsed)
	}

	m.Update(msg)
	want := [][2]int{{80, 24}, {100, 30}}
	if got := topCounting(t, m).sizes; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("relayouts = %v, want %v", got, want)
	}
}

// TestResizeSameSizeIsNoop: v2's checkResize can emit duplicate
// WindowSizeMsgs for one size (tty.go:105-127 re-queries per signal);
// identical sizes must never stack relayouts.
func TestResizeSameSizeIsNoop(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.resizeWindow = time.Hour
	m.Replace(countingPage{id: "count"})

	var cmds int
	for range 5 {
		if _, cmd := m.Update(tea.WindowSizeMsg{Width: 90, Height: 27}); cmd != nil {
			cmds++
		}
	}

	if got := topCounting(t, m).sizes; len(got) != 1 {
		t.Fatalf("duplicate sizes relaid out %d times, want 1", len(got))
	}
	if cmds != 1 {
		t.Errorf("duplicate sizes returned %d cmds, want 1", cmds)
	}
}
