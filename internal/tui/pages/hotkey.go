// hotkey.go carries the convention that every actionable key glyph in
// body copy renders the Theme.Key badge (bold accent), matching the
// footer's accented keys. An inner reset drops the outer tint, so the
// helpers style non-key spans explicitly rather than relying on a later
// whole-line Render.
package pages

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
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

// hintSpans composes one in-body hotkey line from key/action pairs: every
// key carries the Theme.Key badge (the footer's own style), every action
// the base style, theme separators between pairs. An empty action leaves
// the bare badge. The plain bytes equal the old plain-rendered line, so
// only highlighting changes.
func hintSpans(th *theme.Theme, base lipgloss.Style, keyAction ...string) string {
	sep := base.Render(th.Separator())

	var b strings.Builder
	for i := 0; i+1 < len(keyAction); i += 2 {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(th.Key(keyAction[i]))
		if action := keyAction[i+1]; action != "" {
			b.WriteString(base.Render(" " + action))
		}
	}

	return b.String()
}

// hintsMinus drops the footer hint entries whose key an overlay's
// in-body hint line already shows — while the overlay is open its own
// badged line is the single hotkey surface, so the strip never repeats.
func hintsMinus(hints []frame.KeyHint, keys ...string) []frame.KeyHint {
	out := make([]frame.KeyHint, 0, len(hints))
	for _, h := range hints {
		drop := false
		for _, k := range keys {
			if h.Key == k {
				drop = true
			}
		}
		if !drop {
			out = append(out, h)
		}
	}

	return out
}
