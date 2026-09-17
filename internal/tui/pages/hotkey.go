// hotkey.go carries the convention that every actionable key glyph in
// body copy renders the Theme.Key badge (bold accent), matching the
// footer's accented keys. An inner reset drops the outer tint, so the
// helpers style non-key spans explicitly rather than relying on a later
// whole-line Render.
package pages

import (
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// keyGlyph renders one actionable key glyph in the Theme.Key badge for
// splicing into body hint strings.
func keyGlyph(th *theme.Theme, key string) string {
	return th.Key(key)
}

// keySpan renders one "[Key] action" hotkey span — base-styled brackets
// and action word with a Theme.Key badge ("[Enter] connect") — for
// inline hint and dialog-footer lines.
func keySpan(th *theme.Theme, base lipgloss.Style, key, action string) string {
	span := base.Render("[") + th.Key(key)
	if action != "" {
		span += base.Render("] " + action)
	} else {
		span += base.Render("]")
	}

	return span
}
