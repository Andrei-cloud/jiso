// filepicker_up.go is the file picker's up leg: the synthesized ".."
// row and the single goUp helper every up afford shares.
package widgets

import "path/filepath"

// parentEntry synthesizes the ".." row: os.ReadDir never yields
// "."/"..", so the parent is spelled by hand as a directory-kind entry
// and selectEntry's ordinary descent climbs with no new select code.
// The floor is the filesystem root (Dir(dir) == dir there), not p.root.
func (p *FilePicker) parentEntry() (fileEntry, bool) {
	parent := filepath.Dir(p.dir)
	if parent == p.dir {
		return fileEntry{}, false
	}

	return fileEntry{name: "..", path: parent, isDir: true}, true
}

// goUp climbs one directory for backspace, `u`, and Enter/click on the
// ".." row. The floor is the filesystem root, never p.root; relLabel's
// basename fallback (distorted only above a non-`/` root) keeps absolute
// paths out of the label.
func (p *FilePicker) goUp() {
	if parent := filepath.Dir(p.dir); parent != p.dir {
		p.dir = parent
		p.refresh()
	}
}
