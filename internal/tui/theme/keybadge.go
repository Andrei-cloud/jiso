package theme

// Key renders a hotkey token in the single canonical key-badge style. Use
// it for EVERY key glyph (footer, §M help, body copy, palette) so a key
// looks highlighted identically on every screen and modal. It is a thin
// wrapper over HotKey — the bold-accent affordance — so the badge can
// never drift from the style theme_test.go pins. Under a colorless/ASCII
// profile HotKey is an identity style, so this returns the key unchanged
// (keeps *ascii*.golden free of SGR escapes).
func (t *Theme) Key(k string) string { return t.HotKey.Render(k) }
