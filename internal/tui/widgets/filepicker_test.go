package widgets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

// pickFixture builds a deterministic fixture tree under t.TempDir and
// returns its root: dirs specs/ logs/, files a.json b.txt z.pcap,
// hidden .dot.
func pickFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{"specs/visa.json", "logs", "a.json", "b.txt", "z.pcap", ".dot"} {
		full := filepath.Join(root, p)
		if p == "logs" || p == ".dot" {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}

			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

// newPick builds a picker over fixture/ with a json predicate.
func newPick(t *testing.T, th *theme.Theme, root string) *FilePicker {
	t.Helper()

	return NewFilePicker(th, 44, 6, FilePickerOptions{
		Root: root, RootLabel: "fixture/",
		Selectable: func(name string) bool { return strings.HasSuffix(name, ".json") },
	})
}

// names lists the rendered entry names (selector/style stripped).
func names(t *testing.T, p *FilePicker) []string {
	t.Helper()
	var out []string
	for _, l := range lines(p.View())[1:] {
		for _, pre := range []string{"▸ ", "> ", "  "} {
			l = strings.TrimPrefix(l, pre)
		}
		l = strings.TrimRight(ansi.Strip(l), " ")
		if l == "" || strings.HasPrefix(l, "j/k") || strings.HasPrefix(l, "/") {
			continue
		}
		out = append(out, l)
	}

	return out
}

func TestFilePickerSortDirsFirstThenFiles(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t))
	got := names(t, p)
	// The synthesized ".." parent row leads the dirs (UAT round 9 F-9b);
	// ReadDir's own dirs-first/files-first order follows unchanged.
	want := []string{"../", "logs/", "specs/", "a.json", "b.txt", "z.pcap"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestFilePickerParentRowClimbs: the ".." row leads any dir with a
// parent, and Enter on it climbs exactly like the u key and backspace
// (one shared goUp leg). Root "/" mirrors the production owners (§B/
// §J/§L/§G/§H): the whole filesystem is browsable above the start.
func TestFilePickerParentRowClimbs(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	root := pickFixture(t)
	start := filepath.Join(root, "specs")
	open := func() *FilePicker {
		return NewFilePicker(th, 44, 6, FilePickerOptions{Root: "/", Start: start})
	}

	p := open()
	if got := names(t, p); len(got) == 0 || got[0] != "../" {
		t.Fatalf("first row = %v, want the ../ row to lead", got)
	}
	if cmd := enter(p); cmd != nil {
		t.Fatalf("the .. row must descend like a dir, cmd %v", cmd)
	}
	if p.CurrentDir() != root {
		t.Fatalf("enter on .. = %q, want %q", p.CurrentDir(), root)
	}

	p = open()
	p.Update(ch('u'))
	if p.CurrentDir() != root {
		t.Fatalf("u = %q, want %q", p.CurrentDir(), root)
	}

	p = open()
	p.Update(special(tea.KeyBackspace))
	if p.CurrentDir() != root {
		t.Fatalf("backspace = %q, want %q", p.CurrentDir(), root)
	}
}

// TestFilePickerParentRowAbsentAtRoot: "/" has no parent, so the floor
// carries no .. row and both up keys stay inert (the row's "absent at
// the floor" half).
func TestFilePickerParentRowAbsentAtRoot(t *testing.T) {
	t.Parallel()

	p := NewFilePicker(asciiTheme(t), 40, 6, FilePickerOptions{Root: "/"})
	if p.CurrentDir() != "/" {
		t.Fatalf("dir = %q, want /", p.CurrentDir())
	}
	for _, l := range names(t, p) {
		if l == "../" {
			t.Fatalf("the filesystem root must carry no .. row: %v", names(t, p))
		}
	}
	p.Update(ch('u'))
	p.Update(special(tea.KeyBackspace))
	if p.CurrentDir() != "/" {
		t.Fatalf("both up legs must be inert at the floor: %q", p.CurrentDir())
	}
}

// TestFilePickerParentRowSurvivesFilter: the `/` filter may hide every
// real entry, but never the escape hatch.
func TestFilePickerParentRowSurvivesFilter(t *testing.T) {
	t.Parallel()

	start := filepath.Join(pickFixture(t), "specs")
	p := NewFilePicker(asciiTheme(t), 44, 6, FilePickerOptions{Root: "/", Start: start})
	p.Update(ch('/'))
	for _, c := range "zz" {
		p.Update(ch(c))
	}
	if got := names(t, p); strings.Join(got, ",") != "../" {
		t.Fatalf("filtered rows = %v, want only ../", got)
	}
}

// TestFilePickerParentRowEscapesUnreadable: a directory that cannot be
// read must not be a trap (UAT round 9): the up leg still climbs out.
func TestFilePickerParentRowEscapesUnreadable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewFilePicker(asciiTheme(t), 40, 4, FilePickerOptions{Root: root, RootLabel: "bad/", Start: file})
	if body := p.View(); !strings.Contains(body, "cannot read") {
		t.Fatalf("error state missing:\n%s", body)
	}
	p.Update(ch('u'))
	if p.CurrentDir() != root {
		t.Fatalf("u must escape the unreadable dir: %q", p.CurrentDir())
	}
	if body := p.View(); strings.Contains(body, "cannot read") {
		t.Fatalf("the parent dir must read clean:\n%s", body)
	}
}

func TestFilePickerFilterSlashInput(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t))
	p.Update(ch('/'))
	for _, c := range "pc" {
		p.Update(ch(c))
	}
	if body := p.View(); !strings.Contains(body, "/pc") {
		t.Fatalf("filter echo missing:\n%s", body)
	}
	// The .. row is exempt from the filter (the escape hatch cannot be
	// filtered away); the match follows it.
	if got := names(t, p); strings.Join(got, ",") != "../,z.pcap" {
		t.Fatalf("filtered = %v, want ../ then z.pcap", got)
	}
	p.Update(special(tea.KeyEnter)) // keeps the filter, exits input
	if got := names(t, p); strings.Join(got, ",") != "../,z.pcap" {
		t.Fatalf("filter must persist after enter: %v", got)
	}
}

func TestFilePickerExtPredicate(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t)) // 0 ../ 1 logs/ 2 specs/ 3 a.json 4 b.txt 5 z.pcap
	p.list.SetCursor(4)
	if visibleFileMust(t, p) != "b.txt" {
		t.Fatal("cursor must sit on b.txt")
	}
	if cmd := enter(p); cmd != nil {
		t.Fatal("unselectable file must emit no command")
	}
	p.list.SetCursor(3)
	cmd := enter(p)
	if cmd == nil {
		t.Fatal("selectable file must emit a command")
	}
	picked, ok := cmd().(FilePickedMsg)
	if !ok {
		t.Fatalf("msg = %T", cmd())
	}
	if !strings.HasSuffix(picked.Path, "a.json") {
		t.Fatalf("path = %q", picked.Path)
	}
	if picked.Label != "fixture/a.json" {
		t.Fatalf("label = %q, want fixture/a.json", picked.Label)
	}
	if body := p.View(); !strings.Contains(ansi.Strip(body), "b.txt") {
		t.Fatal("unselectable files stay visible (dim), not hidden")
	}
}

// TestFilePickerDirPickKey: an owner choosing a WRITE target instead of
// an existing file opts into PickDirKey, which commits the CURRENTLY
// BROWSED directory through FilePickedMsg (Path = the dir, Label = its
// virtual label with the trailing "/"). Unbound owners never bind the
// key and their footer never advertises it.
func TestFilePickerDirPickKey(t *testing.T) {
	t.Parallel()

	th := asciiTheme(t)
	root := pickFixture(t)

	plain := newPick(t, th, root)
	if _, cmd := plain.Update(ch('s')); cmd != nil {
		t.Fatal("the default picker must not bind s")
	}
	if strings.Contains(plain.View(), "set folder") {
		t.Fatal("the default footer must not advertise the dir-pick key")
	}

	p := NewFilePicker(th, 44, 6, FilePickerOptions{
		Root: root, RootLabel: "fixture/", Start: filepath.Join(root, "specs"),
		PickDirKey: "s",
	})
	_, cmd := p.Update(ch('s'))
	if cmd == nil {
		t.Fatal("s must commit the browsed directory")
	}
	picked, ok := cmd().(FilePickedMsg)
	if !ok {
		t.Fatalf("msg = %T", cmd())
	}
	if picked.Path != filepath.Join(root, "specs") {
		t.Errorf("dir pick path = %q, want the browsed specs dir", picked.Path)
	}
	if picked.Label != "fixture/specs/" {
		t.Errorf("dir pick label = %q, want fixture/specs/", picked.Label)
	}
	if body := ansi.Strip(p.View()); !strings.Contains(body, "set folder") {
		t.Errorf("the footer must advertise the bound key:\n%s", body)
	}
}

func visibleFile(t *testing.T, p *FilePicker) (string, bool) {
	t.Helper()
	item, ok := p.list.Selected()
	if !ok {
		return "", false
	}
	e, _ := item.Data.(fileEntry)

	return e.name, !e.isDir
}

func visibleFileMust(t *testing.T, p *FilePicker) string {
	t.Helper()
	name, _ := visibleFile(t, p)

	return name
}

func enter(p *FilePicker) tea.Cmd {
	_, cmd := p.Update(special(tea.KeyEnter))

	return cmd
}

func TestFilePickerHiddenToggle(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t))
	if strings.Contains(strings.Join(names(t, p), ","), ".dot") {
		t.Fatal("hidden must start hidden")
	}
	p.Update(ch('h'))
	if !strings.Contains(strings.Join(names(t, p), ","), ".dot") {
		t.Fatal("h must reveal dotfiles")
	}
	p.Update(ch('h'))
	if strings.Contains(strings.Join(names(t, p), ","), ".dot") {
		t.Fatal("h must re-hide dotfiles")
	}
}

func TestFilePickerVirtualization1k(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for i := range 1000 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%04d.dat", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := NewFilePicker(asciiTheme(t), 40, 8, FilePickerOptions{Root: root, RootLabel: "big/"})
	if p.list.Len() != 1001 { // 1000 files + the synthesized .. parent row
		t.Fatalf("entries = %d, want 1001", p.list.Len())
	}
	if n := len(lines(p.View())); n != 1+8+1 { // header + window + footer
		t.Fatalf("rendered lines = %d, want 10", n)
	}
}

func TestFilePickerRelativeLabel(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := newPick(t, asciiTheme(t), root)
	p.list.SetCursor(2)              // specs/ (the .. row leads at 0)
	if cmd := enter(p); cmd != nil { // descend specs/
		t.Fatal("descend must not emit a msg")
	}
	body := p.View()
	if !strings.Contains(body, "fixture/specs/") {
		t.Fatalf("relative label missing:\n%s", body)
	}
	if strings.Contains(body, root) {
		t.Fatalf("absolute temp path leaked:\n%s", body)
	}
}

func TestFilePickerEmptyDir(t *testing.T) {
	t.Parallel()

	// An empty dir still leads with the ".." parent row (UAT round 9):
	// the escape hatch is never hidden behind a bare empty message.
	root := t.TempDir()
	p := NewFilePicker(asciiTheme(t), 30, 4, FilePickerOptions{Root: root, RootLabel: "empty/"})
	if got := names(t, p); strings.Join(got, ",") != "../" {
		t.Fatalf("rows = %v, want only the ../ row", got)
	}
	if cmd := enter(p); cmd != nil {
		t.Fatalf("enter on .. must not emit, got %v", cmd)
	}
	if p.CurrentDir() != filepath.Dir(root) {
		t.Fatalf("enter on an empty dir's .. must climb: %q", p.CurrentDir())
	}
}

func TestFilePickerUnreadableDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewFilePicker(asciiTheme(t), 40, 4, FilePickerOptions{Root: file, RootLabel: "bad/"})
	body := p.View()
	if !strings.Contains(body, "cannot read bad/: not a directory") {
		t.Fatalf("error state missing:\n%s", body)
	}
	if strings.Contains(body, root) {
		t.Fatalf("absolute path leaked:\n%s", body)
	}
	if cmd := enter(p); cmd != nil {
		t.Fatal("enter in error state must be inert")
	}
}

func TestFilePickerAsciiFallback(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t))
	body := p.View()
	if !strings.HasPrefix(lines(body)[1], "> ") {
		t.Fatalf("ascii selector missing:\n%s", body)
	}
	if strings.ContainsAny(body, "▸·…") {
		t.Fatalf("unicode glyph in ascii mode:\n%s", body)
	}
}

func TestFilePickerGGAndG(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t))
	p.Update(ch('G'))
	if got := visibleFileMust(t, p); got != "z.pcap" {
		t.Fatalf("G = %q, want z.pcap", got)
	}
	p.Update(ch('g'))
	if visibleFileMust(t, p) != "z.pcap" { // first g arms, does not move
		t.Fatal("single g must not move")
	}
	p.Update(ch('g'))
	if visibleFileMust(t, p) != ".." {
		t.Fatalf("gg = %q, want the .. row (the list's first row)", visibleFileMust(t, p))
	}
	p.Update(ch('j')) // any other key drops the pending g
	p.Update(ch('g'))
	if visibleFileMust(t, p) != "logs" {
		t.Fatalf("pending g must reset: %q", visibleFileMust(t, p))
	}
}

func TestFilePickerDescendAndBackspace(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := newPick(t, asciiTheme(t), root)
	p.list.SetCursor(2)
	enter(p) // descend specs/ (past the .. row leading at 0)
	if filepath.Base(p.CurrentDir()) != "specs" {
		t.Fatalf("dir = %q", p.CurrentDir())
	}
	p.Update(special(tea.KeyBackspace))
	if p.CurrentDir() != root {
		t.Fatalf("backspace must ascend to root: %q", p.CurrentDir())
	}
	// UAT round 9: the up leg is the filesystem, not the root — a root
	// passed as a plain dir (this owner shape) still climbs out (the
	// relLabel basename fallback keeps the absolute path out of View).
	p.Update(special(tea.KeyBackspace))
	if want := filepath.Dir(root); p.CurrentDir() != want {
		t.Fatalf("backspace must climb past the root to %q: %q", want, p.CurrentDir())
	}
}

func TestFilePickerEscCancels(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t))
	_, cmd := p.Update(special(tea.KeyEscape))
	if cmd == nil {
		t.Fatal("esc must emit")
	}
	if _, ok := cmd().(FilePickerCanceledMsg); !ok {
		t.Fatal("esc must cancel")
	}
	p.Update(ch('/')) // empty filter: esc cancels the picker
	_, cmd = p.Update(special(tea.KeyEscape))
	if _, ok := cmd().(FilePickerCanceledMsg); !ok {
		t.Fatal("esc from empty filter must cancel")
	}
	p.Update(ch('/'))
	p.Update(ch('x'))
	_, cmd = p.Update(special(tea.KeyEscape))
	if cmd != nil {
		t.Fatalf("first esc must clear a non-empty filter, not cancel: %v", cmd())
	}
}

func TestFilePickerTruncateNoWrap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	long := strings.Repeat("verylongname", 8) + ".json"
	if err := os.WriteFile(filepath.Join(root, long), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewFilePicker(asciiTheme(t), 24, 4, FilePickerOptions{Root: root, RootLabel: "t/"})
	for i, l := range lines(p.View()) {
		if w := ansi.StringWidth(l); w > 24 {
			t.Errorf("line %d width %d > 24: %q", i, w, l)
		}
	}
}
