package theme

// Key renders a hotkey token in the single canonical key-badge style (a
// thin wrapper over HotKey, the style theme_test.go pins). Under the
// ASCII profile HotKey is an identity style.
func (t *Theme) Key(k string) string { return t.HotKey.Render(k) }
