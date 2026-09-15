// hotkey.go carries the UAT round-4 convention: every key glyph that is
// an actionable hotkey inside body copy renders the Theme.Key badge
// (bold accent), so inline hints visually match the frame footer's
// accented keys. The
// surrounding copy keeps its own base style — and because lipgloss only
// emits an outer Render's escape pair around the whole string (an inner
// reset drops the outer tint for everything after it), the helpers here
// style the non-key spans with the base explicitly instead of relying
// on a later whole-line Render.
package pages

import (
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// keyGlyph renders one actionable key glyph (b, Enter, space, …) in the
// Theme.Key badge for splicing into body hint strings; the caller keeps
// its own base-styled spans around it.
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
