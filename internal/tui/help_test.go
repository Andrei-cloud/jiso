package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// §M help overlay: context-aware keymap registry + frame-level modal.

// helpGoldenTheme pins an explicit profile (theme.Default is
// process-wide and untrustworthy in tests, same rule as the program
// golden harness).
func helpGoldenTheme(prof colorprofile.Profile) *theme.Theme {
	return theme.NewWith(prof, true)
}

// checkHelpGolden compares against testdata/help/<name>.golden,
// honouring the package's shared -update flag.
func checkHelpGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "help", name+".golden")

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
		t.Fatalf("read golden %s: %v (run: go test ./internal/tui -run Help -update)", path, err)
	}
	if got+"\n" != string(want) {
		t.Errorf("golden %s mismatch\nwant:\n%s\ngot:\n%s", path, want, got)
	}
}

func TestHelpOverlayOpensFromEveryRegistryPage(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	for _, p := range m.registry {
		m.Replace(p)
		m.help = nil

		_, _ = m.Update(ch('?'))

		if m.help == nil {
			t.Fatalf("page %s: '?' must open the overlay from EVERY page", p.ID())
		}
		if want := helpContextName(p.ID()); m.help.context != want {
			t.Errorf("page %s: context = %q, want %q", p.ID(), m.help.context, want)
		}
		if len(m.help.groups) < 1 {
			t.Errorf("page %s: overlay has no groups", p.ID())
		}
	}
}

func TestHelpOverlayContextLabelsMatchWireframePages(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		pages.TransactionsPageID: "Transactions page",
		pages.WorkersPageID:      "Workers page",
		pages.SessionsPageID:     "Sessions page",
		pages.SettingsPageID:     "Settings page",
	}
	m := NewRootModel(nil)

	for id, ctx := range want {
		m.help = nil
		m.Replace(m.pageByIDForTest(id))
		_, _ = m.Update(ch('?'))

		if m.help == nil {
			t.Fatalf("page %s: overlay did not open", id)
		}
		if m.help.context != ctx {
			t.Errorf("page %s: context = %q, want %q", id, m.help.context, ctx)
		}
	}
}

// pageByIDForTest resolves a registry entry by page ID (tests); callers
// assert non-nil via Replace/Push panics or their own checks. Unknown
// ids (test fakes, deep pages) fall back to a recording page.
func (m *RootModel) pageByIDForTest(id string) Page {
	for _, p := range m.registry {
		if p.ID() == id {
			return p
		}
	}

	return recordingPage{id: id}
}

func TestHelpOverlaySwallowsGlobalKeys(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(ch('?'))

	for _, msg := range []tea.KeyPressMsg{ch('4'), ch('q'), ch('c'), ch(':')} {
		_, cmd := m.Update(msg)
		if isQuit(t, cmd) {
			t.Fatalf("%s must be swallowed by the overlay", msg)
		}
		wantStack(t, m, "dashboard")

		if m.pal != nil || m.dlg != nil {
			t.Fatalf("%s must not open another overlay while help is open", msg)
		}
	}
}

func TestHelpOverlayCtrlCStillQuits(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(ch('?'))

	_, cmd := m.Update(mod('c', tea.ModCtrl))
	if !isQuit(t, cmd) {
		t.Fatal("ctrl+c stays global above the help overlay")
	}
}

// §N1 stacking: the confirm dialog owns the keyboard first, so `?` is
// swallowed there and the overlay never opens over a pending confirm.
func TestHelpSwallowedWhileConfirmPending(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.workersConfirm = widgets.NewConfirmDialog(nil, "stop all workers?")

	_, _ = m.Update(ch('?'))

	if m.help != nil {
		t.Fatal("the confirm dialog must swallow '?'; the overlay must not open")
	}
	if !m.workersConfirm.Pending() {
		t.Fatal("the '?' must not have reached the confirm dialog either")
	}

	_, _ = m.Update(special(tea.KeyEscape)) // esc cancels the confirm first
	if m.workersConfirm.Pending() {
		t.Fatal("esc must cancel the pending confirm")
	}
	if m.help != nil {
		t.Fatal("esc went to the confirm, not the overlay")
	}
}

func TestHelpQuestionTypesIntoPaletteFilter(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(ch(':'))
	_, _ = m.Update(ch('?'))

	if m.help != nil {
		t.Fatal("'?' must not open the overlay while the palette owns the keyboard")
	}
	if m.pal == nil || m.pal.Query() != "?" {
		t.Fatalf("palette query: got %q, want '?'", m.pal.Query())
	}
}

func TestHelpNotStolenFromPageFilterInput(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(ch('2')) // transactions page
	_, _ = m.Update(ch('/')) // live filter claims the keyboard

	_, _ = m.Update(ch('?'))

	if m.help != nil {
		t.Fatal("'?' typed into the live filter must not open the overlay")
	}
	tx, ok := m.Current().(*pages.Transactions)
	if !ok {
		t.Fatalf("current page: %T, want *pages.Transactions", m.Current())
	}
	if f, _ := tx.Filter(); f != "?" {
		t.Fatalf("filter text: got %q, want '?'", f)
	}
}

// §N1: with a pushed page underneath, Esc closes the overlay FIRST and
// the page never sees it (no pop).
func TestHelpEscClosesBeforePagePop(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	m.Push(m.pageByIDForTest(pages.TransactionsPageID))
	wantStack(t, m, "dashboard", "transactions")

	_, _ = m.Update(ch('?'))
	if m.help == nil {
		t.Fatal("overlay must open over the pushed page")
	}
	if m.help.context != "Transactions page" {
		t.Fatalf("context: got %q, want Transactions page", m.help.context)
	}

	_, _ = m.Update(special(tea.KeyEscape))

	if m.help != nil {
		t.Fatal("esc must close the overlay")
	}
	wantStack(t, m, "dashboard", "transactions") // the overlay ate the esc; page never popped

	_, _ = m.Update(special(tea.KeyEscape)) // second esc reaches the page
	if m.help != nil {
		t.Fatal("the page's esc must not reopen the overlay")
	}
}

func TestHelpOverlayModeChip(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, _ = m.Update(ch('?'))

	// The invented mode chip is gone (wireframe parity A4): the HELP box
	// itself is the mode signal; the top rule keeps the app label.
	v := m.View().Content
	if !strings.Contains(v, "HELP") || !strings.Contains(v, "jiso") {
		t.Fatalf("help overlay must render under the app label top rule:\n%s", v)
	}
}

// TestHelpRegistryDump pins the FULL §M content for every registry page
// (groups, keys, notes): completeness and staleness of the whole registry
// in one golden, plain text (no theme, paths, or timestamps).
func TestHelpRegistryDump(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	checkHelpGolden(t, "registry_dump", helpRegistryDump(append(append([]Page{}, m.registry...), m.send), &m.keys))
}

// renderHelpOverlay renders the overlay box standalone for one registry
// page at a terminal width with an explicit theme profile.
func renderHelpOverlay(t *testing.T, pageID string, prof colorprofile.Profile, width int) string {
	t.Helper()

	m := NewRootModel(nil)
	th := helpGoldenTheme(prof)
	m.theme = th

	return newHelpOverlay(th, m.pageByIDForTest(pageID), &m.keys, width).View()
}

func TestHelpOverlayGoldenTransactionsAscii(t *testing.T) {
	t.Parallel()

	checkHelpGolden(t, "overlay_transactions_ascii",
		renderHelpOverlay(t, pages.TransactionsPageID, colorprofile.ASCII, 100))
}

func TestHelpOverlayGoldenTransactionsTrueColor(t *testing.T) {
	t.Parallel()

	checkHelpGolden(t, "overlay_transactions_truecolor",
		renderHelpOverlay(t, pages.TransactionsPageID, colorprofile.TrueColor, 100))
}

// TestHelpOverlayTrueColorKeysAreBold pins UAT round-8 finding 3: every
// key token in the truecolor overlay must carry the Theme.Key badge
// (bold + accent), not the plain-accent style the overlay used before.
// The bold form is the combined SGR "\x1b[1;38;2;68;147;248m" (dark-mode
// accent #4493f8); the old plain-accent rendering "\x1b[38;2;68;147;248m"
// directly before a key must be gone. Key TEXT is untouched (Phase 5).
func TestHelpOverlayTrueColorKeysAreBold(t *testing.T) {
	t.Parallel()

	got := renderHelpOverlay(t, pages.TransactionsPageID, colorprofile.TrueColor, 100)

	const boldOpen = "\x1b[1;38;2;68;147;248m"
	const plainOpen = "\x1b[38;2;68;147;248m"
	const reset = "\x1b[m"

	for _, key := range []string{"up/k", "enter", "ctrl+c"} {
		if want := boldOpen + key + reset; !strings.Contains(got, want) {
			t.Errorf("overlay lacks the bold-accent Theme.Key badge for %q (%q)", key, want)
		}
		if plain := plainOpen + key + reset; strings.Contains(got, plain) {
			t.Errorf("key %q still renders in plain accent without bold", key)
		}
	}
}

func TestHelpOverlayGoldenWorkersAscii(t *testing.T) {
	t.Parallel()

	checkHelpGolden(t, "overlay_workers_ascii",
		renderHelpOverlay(t, pages.WorkersPageID, colorprofile.ASCII, 100))
}

func TestHelpOverlayGoldenSessionsAscii(t *testing.T) {
	t.Parallel()

	checkHelpGolden(t, "overlay_sessions_ascii",
		renderHelpOverlay(t, pages.SessionsPageID, colorprofile.ASCII, 100))
}

// Narrow terminals drop the aligned label column (compact lines) and the
// box hugs the terminal width.
func TestHelpOverlayGoldenNarrowAscii(t *testing.T) {
	t.Parallel()

	checkHelpGolden(t, "overlay_narrow_ascii",
		renderHelpOverlay(t, pages.SessionsPageID, colorprofile.ASCII, 36))
}

func TestHelpOverlayGoldenNarrowTrueColor(t *testing.T) {
	t.Parallel()

	checkHelpGolden(t, "overlay_narrow_truecolor",
		renderHelpOverlay(t, pages.TransactionsPageID, colorprofile.TrueColor, 36))
}

// Pages without a registry (placeholders, fakes) still get the global
// group and an honest context label.
func TestHelpOverlayUnknownPageGetsGlobalOnly(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Push(recordingPage{id: "scratch"})
	_, _ = m.Update(ch('?'))

	if m.help == nil {
		t.Fatal("overlay must open over a pushed fake page")
	}

	var titles []string
	for _, g := range m.help.groups {
		titles = append(titles, g.Title)
	}

	if strings.Join(titles, ",") != helpGroupGlobal {
		t.Fatalf("groups: got %v, want only %q", titles, helpGroupGlobal)
	}
	if !strings.Contains(m.help.View(), "context: scratch page") {
		t.Errorf("context label must name the current page:\n%s", m.help.View())
	}
}

// helpRegistryDump renders the assembled §M registry for every registry
// page as plain text (no theme styling, no paths, no timestamps) — the
// golden that pins completeness and staleness of the whole overlay
// content. It lives here because only this golden reads it.
func helpRegistryDump(registry []Page, km *globalKeyMap) string {
	var b strings.Builder

	for _, p := range registry {
		_, _ = fmt.Fprintf(&b, "== %s — %s\n", p.ID(), helpContextName(p.ID()))
		for _, g := range helpGroupsFor(p, km) {
			for _, e := range g.Entries {
				_, _ = fmt.Fprintf(&b, "[%s] %s\t%s\n", g.Title, e.Keys, e.Note)
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}
