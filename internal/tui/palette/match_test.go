package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestScoreTextTiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		q, s string
		want int
	}{
		{"send", "send", scoreExact},
		{"SEND", "send", scoreExact},        // case-insensitive
		{"se", "send", scorePrefix},         // prefix
		{"fo", "file open", scoreWord},      // word-boundary subsequence
		{"fo", "fiasco orbit", scoreFuzzy},  // plain subsequence
		{"xyz", "send", scoreNoMatch},       // not a subsequence
		{"sd", "send", scoreFuzzy},          // fuzzy s..d
		{"", "anything", scoreFuzzy},        // empty matches all
		{"日本", "日本語", scorePrefix},          // unicode prefix
		{"本語", "日本語", scoreFuzzy},           // unicode subsequence
		{"🔥", "send", scoreNoMatch},         // emoji no-match
		{"gts", "go to status", scoreWord},  // three word starts
		{"go", "go to status", scorePrefix}, // prefix beats word tier
		{"a-b", "aa-b", scoreFuzzy},         // hyphen boundary probe
		{"ab", "a-b", scoreWord},            // '-' separates words
		{"s", "status", scorePrefix},        // single-rune prefix
		{"ta", "status", scoreFuzzy},        // inner fuzzy
		{"qu", "quit jiso", scorePrefix},    // prefix on phrase
		{"ji", "quit jiso", scoreFuzzy},     // fuzzy, not word-initial
		{"qj", "quit jiso", scoreWord},      // word initials q..j
	}
	for _, c := range cases {
		if got := scoreText(c.q, c.s); got != c.want {
			t.Errorf("scoreText(%q, %q) = %d, want %d", c.q, c.s, got, c.want)
		}
	}
}

func TestSearchEmptyQueryReturnsAllInRegistrationOrder(t *testing.T) {
	t.Parallel()

	m := SeedMatcher()
	got := m.Search("", 0)
	want := Seed().Actions()

	if len(got) != len(want) {
		t.Fatalf("empty query: got %d actions, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Fatalf("empty query order: pos %d got %q, want %q", i, got[i].ID, want[i].ID)
		}
	}
}

func TestSearchPrefixBeatsFuzzyAcrossActions(t *testing.T) {
	t.Parallel()

	m := SeedMatcher()
	got := ids(m.Search("se", 0))

	// "se" is a prefix of the keywords "send" (send-wizard and
	// goto.transactions), "sends" (send-history),
	// "server", "sessions", and
	// "settings"; all of them must precede any fuzzy-only hit, and
	// registration order must break the tie (the proposal-04 wizard is
	// registered before the page jumps, then transactions, server,
	// sessions).
	prefix := map[string]bool{"send-wizard": true, "send-history": true, "goto.transactions": true, "goto.server": true, "goto.sessions": true, "goto.settings": true}
	pos := map[string]int{}
	for i, id := range got {
		pos[id] = i
	}
	if pos["send-wizard"] > pos["goto.transactions"] || pos["goto.transactions"] > pos["goto.server"] || pos["goto.server"] > pos["goto.sessions"] {
		t.Fatalf("tie order: got %v, want wizard, transactions, server, sessions", got)
	}
	for _, id := range got {
		if !prefix[id] && scoreAction("se", lookup(t, id)) != scoreFuzzy {
			t.Fatalf("non-prefix hit %q scored %d", id, scoreAction("se", lookup(t, id)))
		}
	}
}

func TestSearchExactKeywordRanksFirst(t *testing.T) {
	t.Parallel()

	m := SeedMatcher()
	got := m.Search("tx", 0)
	if len(got) == 0 || got[0].ID != "goto.transactions" {
		t.Fatalf("exact keyword tx: got %v, want goto.transactions first", ids(got))
	}
}

func TestSearchTieKeepsRegistrationOrder(t *testing.T) {
	t.Parallel()

	m := SeedMatcher()
	got := ids(m.Search("go to", 0)) // prefix for all page jumps (8 hotkey slots + §L), none else

	if len(got) != 9 {
		t.Fatalf("go to: got %d hits, want 9 (%v)", len(got), got)
	}
	want := []string{
		"goto.dashboard", "goto.transactions", "goto.scenarios", "goto.server",
		"goto.workers", "goto.sessions", "goto.analyze", "goto.ctf", "goto.settings",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tie order pos %d: got %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestSearchLimit(t *testing.T) {
	t.Parallel()

	m := SeedMatcher()
	if got := m.Search("", 3); len(got) != 3 {
		t.Fatalf("limit 3: got %d, want 3", len(got))
	}
	if got := m.Search("", -1); len(got) != len(Seed().Actions()) {
		t.Fatalf("limit -1 must mean no limit, got %d", len(got))
	}
}

func TestSearchNoMatchIsEmpty(t *testing.T) {
	t.Parallel()

	m := SeedMatcher()
	if got := m.Search("qqqqzzz", 0); len(got) != 0 {
		t.Fatalf("no-match query: got %v, want empty", ids(got))
	}
}

func TestSearchDigitFindsPage(t *testing.T) {
	t.Parallel()

	m := SeedMatcher()
	got := m.Search("2", 0)
	if len(got) == 0 || got[0].ID != "goto.transactions" {
		t.Fatalf("digit 2: got %v, want goto.transactions first", ids(got))
	}
}

func TestRegistryIgnoresBadRegistrations(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(Action{ID: "a", Title: "a"}) // nil Run dropped
	r.Register(Action{ID: "b", Title: "b", Run: func([]string) tea.Msg { return nil }})
	r.Register(Action{ID: "b", Title: "dup", Run: func([]string) tea.Msg { return nil }})

	if acts := r.Actions(); len(acts) != 1 || acts[0].ID != "b" {
		t.Fatalf("registry: got %+v, want single b", acts)
	}
}

// helpers

func ids(as []Action) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.ID)
	}

	return out
}

func lookup(t *testing.T, id string) Action {
	t.Helper()
	for _, a := range Seed().Actions() {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no seeded action %q", id)

	return Action{}
}

// guard: seed titles stay lowercase ascii so goldens stay stable.
func TestSeedTitlesAreAscii(t *testing.T) {
	t.Parallel()

	for _, a := range Seed().Actions() {
		if strings.TrimSpace(a.Title) == "" {
			t.Fatalf("action %q has empty title", a.ID)
		}
		for _, r := range a.Title {
			if r > 127 {
				t.Fatalf("title %q of %q is non-ascii", a.Title, a.ID)
			}
		}
	}
}
