// filepicker_position_test.go proves PositionFile seats the list
// cursor on a path without executing anything: hits, misses,
// filter interaction, and the untouched selection.
package widgets

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// PositionFile seats the list cursor on the entry for path WITHOUT
// executing it: SelectRow runs the entry's enter (dirs descend, files
// commit), PositionFile only moves the cursor — the owner's enter still
// executes what the position armed.
func TestFilePickerPositionFileSeatsCursorNoExecute(t *testing.T) {
	t.Parallel()

	root := pickFixture(t) // 0 ../ 1 logs/ 2 specs/ 3 a.json 4 b.txt 5 z.pcap
	p := newPick(t, asciiTheme(t), root)
	if !p.PositionFile(filepath.Join(root, "a.json")) {
		t.Fatal("a.json is listed; PositionFile must report the hit")
	}
	if got := p.list.Cursor(); got != 3 {
		t.Fatalf("cursor = %d, want the a.json row", got)
	}
	if p.CurrentDir() != root {
		t.Fatalf("positioning must not descend: %q", p.CurrentDir())
	}
	if body := ansi.Strip(p.View()); !strings.Contains(body, "> a.json") {
		t.Fatalf("the cursor marker must render on the positioned row:\n%s", body)
	}
	// The selection was armed, not executed: the next enter emits.
	cmd := enter(p)
	if cmd == nil {
		t.Fatal("enter after a position must still emit the pick")
	}
	if _, ok := cmd().(FilePickedMsg); !ok {
		t.Fatalf("enter must emit FilePickedMsg, got %T", cmd())
	}
}

// A directory entry positions too, and still without descending.
func TestFilePickerPositionFileDirEntry(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := newPick(t, asciiTheme(t), root)
	if !p.PositionFile(filepath.Join(root, "specs")) {
		t.Fatal("specs/ is listed; PositionFile must report the hit")
	}
	if got := p.list.Cursor(); got != 2 {
		t.Fatalf("cursor = %d, want the specs/ row", got)
	}
	if p.CurrentDir() != root {
		t.Fatalf("positioning a dir must not descend: %q", p.CurrentDir())
	}
	if got := p.CursorName(); got != "specs" {
		t.Fatalf("CursorName = %q, want the specs row", got)
	}
}

// A relative path resolves against the browsed directory — the shape a
// typed path arrives in.
func TestFilePickerPositionFileResolvesRelative(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := NewFilePicker(asciiTheme(t), 44, 6, FilePickerOptions{
		Root: root, RootLabel: "fixture/", Start: filepath.Join(root, "specs"),
	})
	// rows: ../, visa.json
	if !p.PositionFile("visa.json") {
		t.Fatal("a relative path must resolve against the browsed dir")
	}
	if got := p.list.Cursor(); got != 1 {
		t.Fatalf("cursor = %d, want the visa.json row", got)
	}
	if !p.PositionFile(filepath.Join(root, "specs", "visa.json")) {
		t.Fatal("the same entry's absolute path must hit identically")
	}
}

// An entry the `/` filter hid cannot be positioned; the same path hits
// when the filter lets it through, landing on its filtered-window row.
func TestFilePickerPositionFileFilteredOut(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := newPick(t, asciiTheme(t), root)
	p.Update(ch('/'))
	for _, c := range "zz" {
		p.Update(ch(c))
	}
	if p.PositionFile(filepath.Join(root, "a.json")) {
		t.Fatal("a filtered-out entry must not position")
	}
	if got := p.list.Cursor(); got != 0 {
		t.Fatalf("a miss must leave the cursor alone: %d", got)
	}
	p.Update(special(tea.KeyEscape)) // clears the non-empty filter
	p.Update(ch('/'))
	for _, c := range "b." {
		p.Update(ch(c))
	}
	// visible rows now: ../, b.txt
	if !p.PositionFile(filepath.Join(root, "b.txt")) {
		t.Fatal("a visible entry must position through the filter")
	}
	if got := p.list.Cursor(); got != 1 {
		t.Fatalf("cursor = %d, want b.txt's filtered-window row", got)
	}
}

// A path no listed entry carries changes nothing.
func TestFilePickerPositionFileMiss(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := newPick(t, asciiTheme(t), root)
	p.list.SetCursor(4)
	if p.PositionFile(filepath.Join(root, "gone.json")) {
		t.Fatal("an unlisted file must report false")
	}
	if p.PositionFile("logs/deeper.dat") {
		t.Fatal("a path below a listed dir is not an entry")
	}
	if p.PositionFile("") {
		t.Fatal("the empty path must report false")
	}
	if got := p.list.Cursor(); got != 4 {
		t.Fatalf("misses must not move the cursor: %d", got)
	}
	if p.CurrentDir() != root {
		t.Fatalf("misses must not move the directory: %q", p.CurrentDir())
	}
}
