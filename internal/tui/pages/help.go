package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"

	"jiso/internal/tui/widgets"
)

// §M help-overlay group titles; the router adds its own "global" group.
const (
	HelpGroupNavigation = "navigation"
	HelpGroupActions    = "page actions"
)

// HelpEntry is one §M help-overlay line. Pages build their entries in the
// nav constructor from the SAME key.Bindings the page matches on, so the
// overlay can never advertise a key that does not exist.
type HelpEntry struct {
	Group string
	Keys  string
	Note  string
}

// HelpProvider is implemented by pages that contribute §M help entries.
// The router reads the CURRENT page through this interface; pages without
// it (placeholders, test fakes) contribute only the global group.
type HelpProvider interface {
	HelpEntries() []HelpEntry
}

// tableNavHelp derives the navigation group from the shared widgets
// List/Table bindings. Shadowed keys (keys a page claims for its own
// actions) are dropped so the overlay never lists a dead navigation key.
func tableNavHelp(shadowed ...string) []HelpEntry {
	out := make([]HelpEntry, 0, 6)
	for _, l := range widgets.NavHelp() {
		keys := dropKeys(l.Keys, shadowed)
		if keys == "" {
			continue
		}
		out = append(out, HelpEntry{Group: HelpGroupNavigation, Keys: keys, Note: l.Note})
	}

	return out
}

// dropKeys removes shadowed tokens from a "/"-joined key string.
func dropKeys(joined string, drop []string) string {
	toks := strings.Split(joined, "/")
	keep := toks[:0]

	for _, t := range toks {
		if !containsStr(drop, t) {
			keep = append(keep, t)
		}
	}

	return strings.Join(keep, "/")
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}

	return false
}

// helpKeys renders bindings for display: each binding's keys joined with
// "/", multiple bindings joined with " or " ("up/k or down/j").
func helpKeys(binds ...key.Binding) string {
	parts := make([]string, 0, len(binds))
	for _, b := range binds {
		parts = append(parts, strings.Join(b.Keys(), "/"))
	}

	return strings.Join(parts, " or ")
}

// navEntry builds a navigation-group entry from the page's own bindings.
func navEntry(note string, binds ...key.Binding) HelpEntry {
	return HelpEntry{Group: HelpGroupNavigation, Keys: helpKeys(binds...), Note: note}
}

// actEntry builds a page-actions entry from the page's own bindings.
func actEntry(note string, binds ...key.Binding) HelpEntry {
	return HelpEntry{Group: HelpGroupActions, Keys: helpKeys(binds...), Note: note}
}

// HelpEntries implements HelpProvider for every real screen: each list is
// built in the page's nav constructor from the very bindings the page
// matches on (drift pin).

// HelpEntries is the §M legend for the §A grid: page hotkeys, drill-downs
// and the connection verbs.
func (d *Dashboard) HelpEntries() []HelpEntry { return d.nav.help }

// HelpEntries is the §M legend for the transaction list: selecting,
// sending and the dataset picker.
func (t *Transactions) HelpEntries() []HelpEntry { return t.nav.help }

// HelpEntries is the §M legend for the §C reconstructed-message view and
// its pane focus.
func (i *Inspector) HelpEntries() []HelpEntry { return i.nav.help }

// HelpEntries is the §M legend for the §D send exchange and its field
// editor.
func (s *Send) HelpEntries() []HelpEntry { return s.nav.help }

// HelpEntries is the §M legend for the scenario list: choose, run, and
// inspect a step.
func (s *Scenarios) HelpEntries() []HelpEntry { return s.nav.help }

// HelpEntries is the §M legend for the §F embedded mock server: start,
// stop and the stats keys.
func (s *Server) HelpEntries() []HelpEntry { return s.nav.help }

// HelpEntries is the §M legend for the §K worker list: start, stop and
// open a worker's result.
func (w *Workers) HelpEntries() []HelpEntry { return w.nav.help }

// HelpEntries is the §M legend for the §I session browser: list, drill in,
// and the pane toggle.
func (s *Sessions) HelpEntries() []HelpEntry { return s.nav.help }

// HelpEntries is the §M legend for the §H analyze wizard: its step keys
// and the run controls.
func (a *Analyze) HelpEntries() []HelpEntry { return a.nav.help }

// HelpEntries is the §M legend for the CTF export page: pick a session,
// choose the records, write the file.
func (c *Ctf) HelpEntries() []HelpEntry { return c.nav.help }

// HelpEntries is the §M legend for the settings grid. Settings builds its
// nav on every key press, so its legend comes from the same fresh
// constructor as the keys it lists.
func (s *Settings) HelpEntries() []HelpEntry { return s.nav().help }

// HelpEntries is the §M legend for the connect form: field navigation,
// the dial/listen choice and the header step.
func (d *ConnectDialog) HelpEntries() []HelpEntry { return d.nav.help }
