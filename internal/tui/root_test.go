package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/palette"
)

// recordingPage records every message routed to it; used to prove the global
// layer forwards rather than swallows keys.
type recordingPage struct {
	id   string
	seen []tea.Msg
}

func (p recordingPage) ID() string { return p.id }

func (p recordingPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	p.seen = append(p.seen, msg)

	return p, nil
}

func (p recordingPage) View() tea.View { return tea.NewView(p.id) }

func (p recordingPage) Hints() []KeyHint {
	return []KeyHint{{Key: "x", Desc: "record " + p.id, Primary: true}}
}

// seenOf returns the messages recorded by the stack-top page after a test
// sequence (the stack holds the live page values).
func seenOf(m *RootModel) []tea.Msg {
	rec, ok := m.Current().(recordingPage)
	if !ok {
		return nil
	}

	return rec.seen
}

// ch builds a printable key press like the terminal delivers it.
func ch(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Text: string(c)} }

// mod builds a modified key press (ctrl/alt/shift) with no printable text.
func mod(c rune, m tea.KeyMod) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Mod: m} }

// special builds a named key press (tab, esc, enter, arrows, backspace).
func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// isQuit reports whether cmd, when executed, yields a tea.QuitMsg.
func isQuit(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()

	if cmd == nil {
		return false
	}

	_, ok := cmd().(tea.QuitMsg)

	return ok
}

// quitPump presses a key and pumps the resulting cmd chain (the program
// loop would; the confirm dialog emits ConfirmedMsg through a cmd)
// returning the last cmd seen — isQuit on it reports the quit.
func quitPump(t *testing.T, m *RootModel, k rune) tea.Cmd {
	t.Helper()

	_, cmd := m.Update(ch(k))
	for i := 0; i < 3 && cmd != nil; i++ {
		msg := cmd()
		if msg == nil {
			return nil
		}
		if _, ok := msg.(tea.QuitMsg); ok {
			return cmd
		}
		_, cmd = m.Update(msg)
	}

	return cmd
}

func wantStack(t *testing.T, m *RootModel, ids ...string) {
	t.Helper()

	got := strings.Join(m.StackIDs(), ",")
	want := strings.Join(ids, ",")
	if got != want {
		t.Fatalf("page stack: got [%s], want [%s]", got, want)
	}
}

// mustCmd runs cmd once and requires its message to be T, naming the
// type that actually arrived instead of panicking on the assertion.
func mustCmd[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()

	if cmd == nil {
		t.Fatalf("no cmd armed, want one yielding %T", *new(T))
	}

	msg := cmd()
	m, ok := msg.(T)
	if !ok {
		t.Fatalf("cmd yielded %T, want %T", msg, m)
	}

	return m
}

// fakeSrc recovers the §J façade the analyzeTestRoot harness injects;
// it lives here because root_analyze_test.go sits at its ratchet line
// ceiling and the assertion needs a checked failure path.
func (r *analyzeTestRoot) fakeSrc(t *testing.T) *fakeAnalyze {
	t.Helper()

	f, ok := r.m.analyzeSrc.(*fakeAnalyze)
	if !ok {
		t.Fatalf("analyzeSrc = %T, want *fakeAnalyze", r.m.analyzeSrc)
	}

	return f
}

// TestPageRegistryShape pins the hotkey contract: the 1..8
// slots in footer order ("1 dash 2 tx 3 scenarios 4 server 5 workers
// 6 sessions 7 analyze 8 ctf"), the short footer labels, every slot a
// real page, and the palette jump list in sync with PageIDs.
func TestPageRegistryShape(t *testing.T) {
	t.Parallel()

	wantIDs := []string{"dashboard", "transactions", "scenarios", "server", "workers", "sessions", "analyze", "ctf"}
	wantLabels := []string{"dash", "tx", "scenarios", "server", "workers", "sessions", "analyze", "ctf"}

	if len(PageIDs) != pageCount || len(PageLabels) != pageCount {
		t.Fatalf("PageIDs/PageLabels: got %d/%d slots, want %d", len(PageIDs), len(PageLabels), pageCount)
	}

	for i := range PageIDs {
		if PageIDs[i] != wantIDs[i] {
			t.Fatalf("slot %d: got %q, want %q", i+1, PageIDs[i], wantIDs[i])
		}
		if PageLabels[i] != wantLabels[i] {
			t.Fatalf("label %d: got %q, want %q", i+1, PageLabels[i], wantLabels[i])
		}
	}

	// No placeholder slots may come back: every hotkey slot is a real page.
	m := NewRootModel(nil)
	for i, p := range m.registry[:len(PageIDs)] {
		if p.ID() != PageIDs[i] {
			t.Fatalf("registry slot %d: got %q, want %q", i+1, p.ID(), PageIDs[i])
		}
	}

	// The palette's digit jumps must mirror PageIDs (leaf-level palette
	// keeps its own copy; this is the drift pin).
	var jumps []string
	for _, a := range palette.Seed().Actions() {
		if len(a.Hints) == 1 && a.Hints[0] != "" && a.Hints[0][0] >= '1' && a.Hints[0][0] <= '8' &&
			strings.HasPrefix(a.ID, "goto.") {
			jumps = append(jumps, strings.TrimPrefix(a.ID, "goto."))
		}
	}
	if strings.Join(jumps, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("palette jumps: got [%s], want [%s]", strings.Join(jumps, ","), strings.Join(wantIDs, ","))
	}
}

func TestJumpKeysReplaceStack(t *testing.T) {
	t.Parallel()

	for n := 1; n <= pageCount; n++ {
		m := NewRootModel(nil)

		_, cmd := m.Update(ch(rune('0' + n)))

		if isQuit(t, cmd) {
			t.Fatalf("jump %d must not quit", n)
		}

		wantStack(t, m, PageIDs[n-1])
	}
}

func TestJumpToCurrentPageIsNoop(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	m.Push(m.registry[8]) // the drill-down inspector, pushed as "help used to be"

	_, _ = m.Update(ch('1')) // top is inspector → the jump wins

	wantStack(t, m, "dashboard")

	_, _ = m.Update(ch('1'))
	wantStack(t, m, "dashboard")

	if m.StackDepth() != 1 {
		t.Fatalf("depth: got %d, want 1", m.StackDepth())
	}
}

func TestJumpFromDeepStackReplacesEverything(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "detail"})
	wantStack(t, m, "dashboard", "detail")

	_, _ = m.Update(ch('4'))
	wantStack(t, m, "server")
}

// "?" opens the §M overlay above the stack (the page stack is
// never touched); "?" toggles it closed, Esc closes it, and while open
// page keys are swallowed (q neither pops nor quits).
func TestHelpOverlayOpenToggleClose(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	_, _ = m.Update(ch('?'))
	wantStack(t, m, "dashboard")

	if m.help == nil {
		t.Fatal("'?' must open the §M overlay")
	}

	_, cmd := m.Update(ch('q'))
	if isQuit(t, cmd) {
		t.Fatal("q with the overlay open must be swallowed, not quit")
	}
	if m.help == nil {
		t.Fatal("q must not close the overlay")
	}

	_, _ = m.Update(ch('?')) // toggle closed
	if m.help != nil {
		t.Fatal("second '?' must toggle the overlay closed")
	}

	_, _ = m.Update(ch('?'))
	_, _ = m.Update(special(tea.KeyEscape))
	if m.help != nil {
		t.Fatal("esc must close the overlay")
	}

	_, cmd = m.Update(ch('q'))
	if isQuit(t, cmd) {
		t.Fatal("q at root must arm the quit confirmation (UAT), not quit")
	}
	if m.workersConfirm == nil {
		t.Fatal("q must open the quit confirmation modal")
	}
	if !isQuit(t, quitPump(t, m, 'y')) {
		t.Fatal("y must confirm the quit")
	}
}

func TestQuitPopsPushedPage(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "scratch"})

	_, cmd := m.Update(ch('q'))
	if isQuit(t, cmd) {
		t.Fatal("q with depth 2 must pop")
	}
	wantStack(t, m, "dashboard")

	if seen := seenOf(m); len(seen) != 0 {
		t.Fatalf("popped page must not receive q; status saw %v", seen)
	}
}

func TestFastKeypressBurst(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	type step struct {
		msg  tea.KeyPressMsg
		want []string
	}

	steps := []step{
		{ch('1'), []string{"dashboard"}},
		{ch('2'), []string{"transactions"}},
		{ch('3'), []string{"scenarios"}},
		// '?' opens the overlay (stack untouched) and q is
		// swallowed while it is open; esc closes it first (§N1).
		{ch('?'), []string{"scenarios"}},
		{ch('q'), []string{"scenarios"}},
		{special(tea.KeyEscape), []string{"scenarios"}},
	}

	for i, s := range steps {
		_, cmd := m.Update(s.msg)
		if isQuit(t, cmd) {
			t.Fatalf("step %d (%s) quit early", i, s.msg)
		}
		wantStack(t, m, s.want...)
	}

	_, cmd := m.Update(ch('q'))
	if isQuit(t, cmd) {
		t.Fatal("burst must end in the quit confirmation at root (UAT)")
	}
	if !isQuit(t, quitPump(t, m, 'y')) {
		t.Fatal("y must confirm the quit")
	}
}

func TestTabForwardsPaneFocusMsg(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "detail"})

	_, cmd := m.Update(special(tea.KeyTab))
	if isQuit(t, cmd) {
		t.Fatal("tab must not quit")
	}

	_, _ = m.Update(special(tea.KeyTab))
	_, _ = m.Update(special(tea.KeyTab))

	seen := seenOf(m)

	var got []PaneFocusMsg
	for _, msg := range seen {
		if pf, ok := msg.(PaneFocusMsg); ok {
			got = append(got, pf)
		}
	}

	if len(got) != 3 {
		t.Fatalf("tab count: got %d PaneFocusMsg, want 3 (seen %v)", len(got), seen)
	}

	for i, pf := range got {
		if pf.Reverse {
			t.Fatalf("tab %d: Reverse=true, want false", i)
		}
	}
}

func TestShiftTabForwardsReversePaneFocus(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "detail"})

	_, _ = m.Update(mod(tea.KeyTab, tea.ModShift))

	seen := seenOf(m)
	if len(seen) != 1 {
		t.Fatalf("shift+tab: got %d msgs, want 1", len(seen))
	}

	pf, ok := seen[0].(PaneFocusMsg)
	if !ok {
		t.Fatalf("shift+tab: got %T, want PaneFocusMsg", seen[0])
	}

	if !pf.Reverse {
		t.Fatal("shift+tab must set Reverse")
	}
}

func TestArrowsHjklAndUnknownKeysForwarded(t *testing.T) {
	t.Parallel()

	keys := []tea.KeyPressMsg{
		special(tea.KeyUp), special(tea.KeyDown), special(tea.KeyLeft), special(tea.KeyRight),
		ch('h'), ch('j'), ch('k'), ch('l'),
		ch('x'), ch('g'), ch('G'), ch('r'),
	}

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "detail"})

	for _, msg := range keys {
		_, cmd := m.Update(msg)
		if isQuit(t, cmd) {
			t.Fatalf("key %s must not quit globally", msg)
		}
	}

	seen := seenOf(m)
	if len(seen) != len(keys) {
		t.Fatalf("forwarded %d keys, page saw %d", len(keys), len(seen))
	}

	wantStack(t, m, "dashboard", "detail")
}

func TestWindowSizeReachesAllStackPages(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "detail"})

	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	if m.width != 120 || m.height != 32 {
		t.Fatalf("root size: got %dx%d, want 120x32", m.width, m.height)
	}

	// The dashboard is a pointer page shared with the registry, so it is
	// resized in place; a registry page that never entered the stack
	// (CTF, slot 8) must stay unsized — only stack pages get the size.
	if w, h := m.ctf.Size(); w != 0 || h != 0 {
		t.Fatalf("hidden registry page must not be resized; got %dx%d", w, h)
	}

	if w, h := m.dash.Size(); w != 120 || h != 32 {
		t.Fatalf("dashboard page size: got %dx%d, want 120x32", w, h)
	}

	seen := seenOf(m)
	if len(seen) != 1 {
		t.Fatalf("detail page saw %d msgs, want 1", len(seen))
	}

	if _, ok := seen[0].(tea.WindowSizeMsg); !ok {
		t.Fatalf("detail page got %T, want WindowSizeMsg", seen[0])
	}
}

func TestCtrlCQuitsGracefully(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(m.registry[8]) // any non-root page; ctrl+c stays global at any depth

	_, cmd := m.Update(mod('c', tea.ModCtrl))
	if !isQuit(t, cmd) {
		t.Fatal("ctrl+c must return tea.Quit (graceful, runtime-managed)")
	}
}

func TestReservedKeysStayUnbound(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(recordingPage{id: "detail"})

	// Ctrl+Z and Ctrl+\ are reserved for the runtime: the global layer must
	// neither quit nor swallow them.
	for _, msg := range []tea.KeyPressMsg{
		mod('z', tea.ModCtrl),
		{Code: '\\', Mod: tea.ModCtrl},
	} {
		_, cmd := m.Update(msg)
		if isQuit(t, cmd) {
			t.Fatalf("reserved key %s must not quit", msg)
		}
	}

	seen := seenOf(m)
	if len(seen) != 2 {
		t.Fatalf("reserved keys: page saw %d msgs, want 2 (forwarded, not swallowed)", len(seen))
	}
}

func TestViewContract(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	v := m.View()
	if !v.AltScreen {
		t.Fatal("root view must own the alternate screen")
	}
	// Page 1 is the real §A dashboard: the grid
	// sections, not placeholders (the EVENT FEED pane is gone; the
	// SERVER LOG card replaced it as the live-signal pane).
	for _, want := range []string{"CONNECTION", "SESSION", "QUICK ACTIONS", "SERVER LOG"} {
		if !strings.Contains(v.Content, want) {
			t.Fatalf("dashboard frame lacks %q; got:\n%s", want, v.Content)
		}
	}

	_, _ = m.Update(ch('3'))
	// Slot 3 is the §F scenarios page.
	if !strings.Contains(m.View().Content, "SCENARIOS") {
		t.Fatal("view must follow the page stack")
	}

	_, _ = m.Update(ch(':'))
	content := m.View().Content
	if !strings.Contains(content, "go to transactions") {
		t.Fatalf("palette overlay must list actions; got:\n%s", content)
	}
	if !strings.Contains(content, ":") {
		t.Fatal("palette bar must show the ':' prefix")
	}
}

func TestPushPopReplaceAPI(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	m.Push(recordingPage{id: "b"})
	m.Push(recordingPage{id: "c"})
	wantStack(t, m, "dashboard", "b", "c")

	if got := m.Pop(); got == nil || got.ID() != "c" {
		t.Fatalf("Pop: got %v, want c", got)
	}
	wantStack(t, m, "dashboard", "b")

	m.Replace(recordingPage{id: "solo"})
	wantStack(t, m, "solo")

	if got := m.Pop(); got != nil {
		t.Fatalf("Pop at depth 1: got %v, want nil (stack never empties)", got)
	}
	wantStack(t, m, "solo")
}
