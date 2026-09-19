// filepicker_position.go is the file picker's cursor-positioning leg:
// seat the list cursor on a known path without executing anything, and
// report what the cursor sits on.
package widgets

import (
	"path/filepath"
)

// PositionFile moves the list cursor onto the entry for path (resolved
// against the browsed directory) WITHOUT executing its selection:
// SelectRow runs the entry's Enter — dirs descend, selectable files
// commit — this only moves the cursor, leaving the hit for the operator
// to confirm. ok is false when no VISIBLE row matches: an unlisted path
// or one the `/` filter hid.
func (p *FilePicker) PositionFile(path string) bool {
	target := filepath.Clean(path)
	if !filepath.IsAbs(target) {
		target = filepath.Join(p.dir, path)
	}
	for i, item := range p.list.items {
		if e, ok := item.Data.(fileEntry); ok && e.path == target {
			p.list.SetCursor(i)

			return true
		}
	}

	return false
}

// CursorName reports the entry name under the list cursor ("" when the
// list holds nothing or the read failed) — how owners verify what
// PositionFile seated without reaching into the list.
func (p *FilePicker) CursorName() string {
	e, ok := p.visibleEntry()
	if !ok {
		return ""
	}

	return e.name
}
