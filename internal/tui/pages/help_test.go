package pages

import (
	"reflect"
	"strings"
	"testing"

	key "charm.land/bubbles/v2/key"

	"jiso/internal/tui/widgets"
)

// splitHelpTokens decomposes a Keys display string ("up/k or down/j")
// into its individual key tokens.
func splitHelpTokens(keys string) []string {
	var out []string
	for _, bind := range strings.Split(keys, " or ") {
		for _, k := range strings.Split(bind, "/") {
			// A lone "/" key (the filter trigger) survives the join/split
			// round-trip as an empty segment; restore the token.
			if k == "" {
				out = append(out, "/")

				continue
			}
			out = append(out, k)
		}
	}

	return out
}

// helpDriftCase pins one nav struct against the §M entries its
// constructor registered.
type helpDriftCase struct {
	name    string
	nav     any
	entries []HelpEntry
	// hidden names binding fields deliberately NOT listed in the overlay
	// (context-bound keys, e.g. filter-mode text editing) with the reason.
	hidden map[string]string
}

var filterModeBackspace = map[string]string{
	"Backspace": "filter-mode text editing, not a page-level key",
}

func TestHelpRegistryDriftPin(t *testing.T) {
	t.Parallel()

	cases := []helpDriftCase{
		{name: "dashboard", nav: newDashNav(), entries: newDashNav().help},
		{name: "transactions", nav: newTxNav(), entries: newTxNav().help, hidden: filterModeBackspace},
		{name: "inspector", nav: newInspNav(), entries: newInspNav().help},
		{name: "send", nav: newSendNav(), entries: newSendNav().help},
		{name: "scenarios", nav: newScenNav(), entries: newScenNav().help, hidden: filterModeBackspace},
		{name: "server", nav: newServerNav(), entries: newServerNav().help},
		{name: "workers", nav: newWorkersNav(), entries: newWorkersNav().help},
		{name: "sessions", nav: newSessionsNav(), entries: newSessionsNav().help, hidden: filterModeBackspace},
		{name: "analyze", nav: newAnalyzeNav(), entries: newAnalyzeNav().help, hidden: filterModeBackspace},
		{name: "ctf", nav: newCtfNav(), entries: newCtfNav().help, hidden: filterModeBackspace},
		{name: "settings", nav: newSettingsNav(), entries: newSettingsNav().help, hidden: filterModeBackspace},
		{name: "connect", nav: newConnectNav(), entries: newConnectNav().help},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driftPin(t, tc)
		})
	}
}

func driftPin(t *testing.T, tc helpDriftCase) {
	t.Helper()

	typ := reflect.TypeOf(tc.nav)
	bindingType := reflect.TypeOf(key.Binding{})

	bindingTokens := map[string]bool{}
	seenHidden := map[string]bool{}

	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type != bindingType {
			continue
		}

		bv := reflect.ValueOf(tc.nav).Field(i).Interface()
		b, ok := bv.(key.Binding)
		if !ok {
			t.Fatalf("nav field %s = %T, want key.Binding", f.Name, bv)
		}
		for _, k := range b.Keys() {
			if tc.hidden[f.Name] != "" {
				seenHidden[f.Name] = true
			}

			bindingTokens[k] = true
		}
	}

	for name, reason := range tc.hidden {
		if !seenHidden[name] {
			t.Errorf("hidden field %q (%s) is not a key.Binding field — stale entry", name, reason)
		}
	}

	// Forward pin: every binding key (except hidden fields) appears in
	// exactly one listed entry line.
	listed := map[string]int{}
	for _, e := range tc.entries {
		if e.Group != HelpGroupNavigation && e.Group != HelpGroupActions {
			t.Errorf("entry %q/%q: unknown group %q", e.Keys, e.Note, e.Group)
		}

		for _, k := range splitHelpTokens(e.Keys) {
			listed[k]++
		}
	}

	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type != bindingType || tc.hidden[f.Name] != "" {
			continue
		}

		bv := reflect.ValueOf(tc.nav).Field(i).Interface()
		b, ok := bv.(key.Binding)
		if !ok {
			t.Fatalf("nav field %s = %T, want key.Binding", f.Name, bv)
		}
		for _, k := range b.Keys() {
			if listed[k] == 0 {
				t.Errorf("binding %s key %q missing from §M registry", f.Name, k)
			}
		}
	}

	// Reverse pin: no listed key is stale — every token must exist as a
	// page binding or (navigation only) as a shared widgets nav key.
	widgetsTokens := map[string]bool{}
	for _, l := range widgets.NavHelp() {
		for _, k := range strings.Split(l.Keys, "/") {
			widgetsTokens[k] = true
		}
	}

	for _, e := range tc.entries {
		for _, k := range splitHelpTokens(e.Keys) {
			if bindingTokens[k] {
				continue
			}

			if e.Group == HelpGroupNavigation && widgetsTokens[k] {
				continue
			}

			t.Errorf("stale §M entry %q (%s): key %q is bound nowhere", e.Keys, e.Note, k)
		}
	}
}

// TestHelpRegistryNavigationFirst pins the group ORDER every §M renderer
// relies on: navigation before page actions, both before the router's
// global group.
func TestHelpRegistryNavigationFirst(t *testing.T) {
	t.Parallel()

	pages := []HelpProvider{
		NewDashboard(nil), NewTransactions(nil), NewInspector(nil), NewSend(nil),
		NewScenarios(nil), NewServer(nil), NewWorkers(nil), NewSessions(nil),
		NewAnalyze(nil), NewCtf(nil), NewSettings(nil), NewConnectDialog(nil),
	}

	for _, p := range pages {
		var seen []string

		for _, e := range p.HelpEntries() {
			if len(seen) == 0 || seen[len(seen)-1] != e.Group {
				seen = append(seen, e.Group)
			}
		}

		want := []string{}
		for _, g := range []string{HelpGroupNavigation, HelpGroupActions} {
			for _, e := range p.HelpEntries() {
				if e.Group == g {
					want = append(want, g)

					break
				}
			}
		}

		if strings.Join(seen, ",") != strings.Join(want, ",") {
			t.Errorf("%T: group order %v, want %v (each group contiguous)", p, seen, want)
		}
	}
}
