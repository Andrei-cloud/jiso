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
	want := []string{"logs/", "specs/", "a.json", "b.txt", "z.pcap"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
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
	if got := names(t, p); strings.Join(got, ",") != "z.pcap" {
		t.Fatalf("filtered = %v, want z.pcap", got)
	}
	p.Update(special(tea.KeyEnter)) // keeps the filter, exits input
	if got := names(t, p); strings.Join(got, ",") != "z.pcap" {
		t.Fatalf("filter must persist after enter: %v", got)
	}
}

func TestFilePickerExtPredicate(t *testing.T) {
	t.Parallel()

	p := newPick(t, asciiTheme(t), pickFixture(t)) // 0 logs/ 1 specs/ 2 a.json 3 b.txt 4 z.pcap
	p.list.SetCursor(3)
	if visibleFileMust(t, p) != "b.txt" {
		t.Fatal("cursor must sit on b.txt")
	}
	if cmd := enter(p); cmd != nil {
		t.Fatal("unselectable file must emit no command")
	}
	p.list.SetCursor(2)
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
	if p.list.Len() != 1000 {
		t.Fatalf("entries = %d, want 1000", p.list.Len())
	}
	if n := len(lines(p.View())); n != 1+8+1 { // header + window + footer
		t.Fatalf("rendered lines = %d, want 10", n)
	}
}

func TestFilePickerRelativeLabel(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := newPick(t, asciiTheme(t), root)
	p.list.SetCursor(1)              // specs/
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

	p := NewFilePicker(asciiTheme(t), 30, 4, FilePickerOptions{Root: t.TempDir(), RootLabel: "empty/"})
	if body := p.View(); !strings.Contains(body, "empty directory") {
		t.Fatalf("empty state missing:\n%s", body)
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
	if visibleFileMust(t, p) != "logs" {
		t.Fatalf("gg = %q, want logs", visibleFileMust(t, p))
	}
	p.Update(ch('j')) // any other key drops the pending g
	p.Update(ch('g'))
	if visibleFileMust(t, p) != "specs" {
		t.Fatalf("pending g must reset: %q", visibleFileMust(t, p))
	}
}

func TestFilePickerDescendAndBackspace(t *testing.T) {
	t.Parallel()

	root := pickFixture(t)
	p := newPick(t, asciiTheme(t), root)
	p.list.SetCursor(1)
	enter(p) // descend specs/
	if filepath.Base(p.CurrentDir()) != "specs" {
		t.Fatalf("dir = %q", p.CurrentDir())
	}
	p.Update(special(tea.KeyBackspace))
	if p.CurrentDir() != root {
		t.Fatalf("backspace must ascend to root: %q", p.CurrentDir())
	}
	p.Update(special(tea.KeyBackspace)) // at root: inert
	if p.CurrentDir() != root {
		t.Fatalf("root must clamp: %q", p.CurrentDir())
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
