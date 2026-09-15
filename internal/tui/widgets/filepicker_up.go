// filepicker_up.go is the file picker's up leg (UAT round 9 F-9a/F-9b):
// the synthesized ".." row and the single climb helper every up afford
// (backspace, the u key, Enter/click on the row) shares. Split out of
// filepicker.go for the source-file line budget.
package widgets

import "path/filepath"

// parentEntry synthesizes the ".." row: os.ReadDir never yields
// "."/"..", so the parent is spelled by hand as a directory-kind entry
// pointing at the browsed dir's parent — selectEntry's ordinary descent
// then climbs with no new select code. The floor is the filesystem root
// (filepath.Dir("/") == "/"), not p.root: root anchors the display
// label, it is never a navigation wall.
func (p *FilePicker) parentEntry() (fileEntry, bool) {
	parent := filepath.Dir(p.dir)
	if parent == p.dir {
		return fileEntry{}, false
	}

	return fileEntry{name: "..", path: parent, isDir: true}, true
}

// goUp climbs one directory. Clamping the leg to p.root was the UAT
// round 9 F-9a trap (a picker opened with Start == Root could never
// leave its start dir); the floor is the filesystem root, and
// relLabel's basename fallback keeps absolute paths out of the label
// when a root-anchored owner climbs above its tree.
func (p *FilePicker) goUp() {
	if parent := filepath.Dir(p.dir); parent != p.dir {
		p.dir = parent
		p.refresh()
	}
}
