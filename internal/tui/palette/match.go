package palette

import (
	"sort"
	"strings"
	"unicode"
)

// Fuzzy score tiers for one (query, text) pair. Higher wins; equal tiers
// fall back to registration order (sort.SliceStable), which keeps the
// palette deterministic and registration-order stable.
const (
	scoreNoMatch = 0
	scoreFuzzy   = 1 // query is a plain subsequence of text
	scoreWord    = 2 // every query rune lands on a word start
	scorePrefix  = 3 // text starts with the query
	scoreExact   = 4 // text equals the query
)

// isWordStart reports whether the rune at rune-index i begins a word:
// index 0, or the previous rune is not a letter/digit (space, '-', '_',
// '.', '/' all separate words).
func isWordStart(sr []rune, i int) bool {
	if i == 0 {
		return true
	}
	last := sr[i-1]

	return !unicode.IsLetter(last) && !unicode.IsDigit(last)
}

// subsequence reports whether every rune of q appears in s in order
// (greedy leftmost). wordOnly is true when the match also works with
// every query rune landing on a word start.
func subsequence(q, s string) (fuzzy, wordOnly bool) {
	wordOnly = true
	qr, sr := []rune(q), []rune(s)
	qi := 0
	for i := 0; i < len(sr) && qi < len(qr); i++ {
		if sr[i] == qr[qi] {
			if !isWordStart(sr, i) {
				wordOnly = false
			}
			qi++
		}
	}
	if qi < len(qr) {
		return false, false
	}

	return true, wordOnly && len(qr) > 0
}

// scoreText scores one field. An empty query matches everything at
// scoreFuzzy-1 (so all actions show, relative order = registration).
func scoreText(q, s string) int {
	if q == "" {
		return scoreFuzzy
	}
	ql, sl := strings.ToLower(q), strings.ToLower(s)
	switch {
	case ql == sl:
		return scoreExact
	case strings.HasPrefix(sl, ql):
		return scorePrefix
	}
	fuzzy, wordOnly := subsequence(ql, sl)
	if !fuzzy {
		return scoreNoMatch
	}
	if wordOnly {
		return scoreWord
	}

	return scoreFuzzy
}

// scoreAction scores an action as the best of its title and keywords
// (keywords carry aliases and hotkey digits so ":2" finds the page).
func scoreAction(q string, a Action) int {
	best := scoreText(q, a.Title)
	for _, kw := range a.Keywords {
		if s := scoreText(q, kw); s > best {
			best = s
		}
	}

	return best
}

// Matcher searches a registry with the tiered fuzzy scorer.
type Matcher struct {
	registry *Registry
}

// NewMatcher binds a matcher to an action registry.
func NewMatcher(r *Registry) *Matcher { return &Matcher{registry: r} }

// All returns every registered action, unfiltered. The palette uses it to size
// its hint column from the whole registry rather than the filtered subset, so
// the column does not move while the operator types.
func (m *Matcher) All() []Action { return m.registry.Actions() }

// SeedMatcher is the convenience constructor over Seed()'s registry.
func SeedMatcher() *Matcher { return NewMatcher(Seed()) }

// Search returns actions matching q, best score first; ties keep
// registration order. limit <= 0 means no limit. An empty query returns
// every action in registration order.
func (m *Matcher) Search(q string, limit int) []Action {
	type hit struct {
		a Action
		s int
	}
	var hits []hit
	for _, a := range m.registry.Actions() {
		if s := scoreAction(q, a); s > scoreNoMatch {
			hits = append(hits, hit{a: a, s: s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })

	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Action, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.a)
	}

	return out
}
