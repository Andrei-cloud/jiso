package tui

import (
	"strings"

	key "charm.land/bubbles/v2/key"

	"jiso/internal/tui/pages"
)

// helpGroupTitle is the §M "global" group label (the router's own keys;
// pages never contribute to it).
const helpGroupGlobal = "global"

// helpContextName maps a page ID to the §M context label
// ("HELP — context: <page name>"). Unknown IDs (test fakes, deep pages)
// fall back to the raw ID so the label always names the CURRENT page.
func helpContextName(id string) string {
	switch id {
	case pages.DashboardPageID:
		return "Dashboard page"
	case pages.TransactionsPageID:
		return "Transactions page"
	case pages.InspectorPageID:
		return "Inspector page"
	case pages.SendPageID:
		return "Send exchange page"
	case pages.ScenariosPageID:
		return "Scenarios page"
	case pages.ServerPageID:
		return "Mock server page"
	case pages.WorkersPageID:
		return "Workers page"
	case pages.SessionsPageID:
		return "Sessions page"
	case pages.AnalyzePageID:
		return "PCAP analyze page"
	case pages.CtfPageID:
		return "CTF export page"
	case pages.SettingsPageID:
		return "Settings page"
	default:
		return id + " page"
	}
}

// helpGroup is one rendered §M section: a column label plus its entries,
// navigation / page actions contributed by the CURRENT page through
// pages.HelpProvider, global derived from the router keymap itself.
type helpGroup struct {
	Title   string
	Entries []pages.HelpEntry
}

// helpKeysOf renders a binding's key strings for display ("ctrl+p/:").
func helpKeysOf(b key.Binding) string { return strings.Join(b.Keys(), "/") }

// helpPageJumpKeys compresses the 1..8 jump bindings to their digit range
// ("1-8"), derived from the first and last binding in the keymap.
func helpPageJumpKeys(km *globalKeyMap) string {
	first, last := km.PageJumps[0].Keys(), km.PageJumps[pageCount-1].Keys()
	if len(first) == 0 || len(last) == 0 {
		return ""
	}

	return first[0] + "-" + last[0]
}

// globalHelpGroup derives the §M global section from the router keymap —
// the same bindings updateKey matches on, never a hand-copied list.
func globalHelpGroup(km *globalKeyMap) helpGroup {
	pane := helpKeysOf(km.PaneFocus) + "/" + helpKeysOf(km.PaneFocusBack)

	entries := []pages.HelpEntry{
		{Group: helpGroupGlobal, Keys: helpPageJumpKeys(km), Note: "pages"},
		{Group: helpGroupGlobal, Keys: helpKeysOf(km.Palette), Note: "palette"},
		{Group: helpGroupGlobal, Keys: helpKeysOf(km.Connect), Note: "connect"},
		{Group: helpGroupGlobal, Keys: pane, Note: "focus pane"},
		{Group: helpGroupGlobal, Keys: helpKeysOf(km.Help), Note: "this"},
		{Group: helpGroupGlobal, Keys: helpKeysOf(km.Quit), Note: "quit / back"},
		{Group: helpGroupGlobal, Keys: helpKeysOf(km.GracefulExit), Note: "graceful quit"},
	}

	return helpGroup{Title: helpGroupGlobal, Entries: entries}
}

// helpGroupsFor composes the overlay sections for one page: its registered
// entries (grouped in registration order) plus the global group. Pages
// without a registry (placeholders, fakes) show only the global group.
func helpGroupsFor(p Page, km *globalKeyMap) []helpGroup {
	var groups []helpGroup

	if hp, ok := p.(pages.HelpProvider); ok {
		for _, e := range hp.HelpEntries() {
			if len(groups) > 0 && groups[len(groups)-1].Title == e.Group {
				groups[len(groups)-1].Entries = append(groups[len(groups)-1].Entries, e)

				continue
			}
			groups = append(groups, helpGroup{Title: e.Group, Entries: []pages.HelpEntry{e}})
		}
	}

	return append(groups, globalHelpGroup(km))
}
