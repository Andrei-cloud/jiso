package widgets

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// navKeys is the shared navigation binding set for List and Table:
// arrows always, vim aliases (j/k) where no text field owns input,
// pgup/pgdn, home/end (design contract: hybrid modeless + vim aliases).
// Bindings are verified against bubbletea v2.0.9 key names: KeyPressMsg
// .String yields "up"/"down"/"pgup"/"pgdown"/"home"/"end".
type navKeys struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding
}

func newNavKeys() navKeys {
	return navKeys{
		Up:       key.NewBinding(key.WithKeys("up", "k")),
		Down:     key.NewBinding(key.WithKeys("down", "j")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		Home:     key.NewBinding(key.WithKeys("home")),
		End:      key.NewBinding(key.WithKeys("end")),
	}
}

// HelpLine is one §M help-overlay navigation line: the display key string
// (binding keys joined with "/") plus the action note. Pages map these into
// their help-entry registry; NavHelp is the single registered source for
// the shared List/Table navigation keys (drift test: keys_test.go).
type HelpLine struct {
	Keys string
	Note string
}

// NavHelp derives the shared navigation help lines from the very bindings
// every List/Table resolves — never a hand-copied string list.
func NavHelp() []HelpLine {
	k := newNavKeys()

	return []HelpLine{
		{Keys: joinKeyStrings(k.Up), Note: "up"},
		{Keys: joinKeyStrings(k.Down), Note: "down"},
		{Keys: joinKeyStrings(k.PageUp), Note: "page up"},
		{Keys: joinKeyStrings(k.PageDown), Note: "page down"},
		{Keys: joinKeyStrings(k.Home), Note: "top"},
		{Keys: joinKeyStrings(k.End), Note: "bottom"},
	}
}

// joinKeyStrings renders one binding's keys for display ("up/k").
func joinKeyStrings(b key.Binding) string { return strings.Join(b.Keys(), "/") }

// navAction is the resolved navigation intent of a key press.
type navAction int

const (
	navNone navAction = iota
	navUp
	navDown
	navPageUp
	navPageDown
	navHome
	navEnd
)

// resolve maps a key press to a nav action (first match wins).
func (k navKeys) resolve(msg tea.KeyPressMsg) navAction {
	switch {
	case key.Matches(msg, k.Up):
		return navUp
	case key.Matches(msg, k.Down):
		return navDown
	case key.Matches(msg, k.PageUp):
		return navPageUp
	case key.Matches(msg, k.PageDown):
		return navPageDown
	case key.Matches(msg, k.Home):
		return navHome
	case key.Matches(msg, k.End):
		return navEnd
	default:
		return navNone
	}
}
