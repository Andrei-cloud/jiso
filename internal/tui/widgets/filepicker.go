package widgets

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// File-picker messages (the selection callback contract): the picker
// never blocks; decisions arrive as tea.Cmd results like the confirm
// dialog's. Path is the real filesystem path for the OWNER to use;
// Label is the virtual display path (RootLabel-prefixed, never the
// absolute root) for the OWNER to echo.
type (
	// FilePickedMsg is the enter-on-selectable-file result.
	FilePickedMsg struct{ Path, Label string }
	// FilePickerCanceledMsg is the esc (or esc-from-filter) result.
	FilePickerCanceledMsg struct{}
)

// FilePickerOptions configures NewFilePicker. Selectable filters which
// FILES are pickable (an extension predicate, e.g. ".pcap"/".json");
// nil accepts any file. Directories are always enterable. RootLabel is
// the virtual root shown in View (e.g. "fixture/"); empty falls back to
// Root, so callers rendering goldens MUST pass a stable label.
// PickDirKey (UAT round 8 finding 6) opts an owner into an extra bound
// key that commits the CURRENTLY BROWSED directory through
// FilePickedMsg — for owners picking a folder to write into rather
// than an existing file to read. Empty (every reading owner) binds
// nothing and the footer stays byte-identical.
type FilePickerOptions struct {
	Root       string
	RootLabel  string
	Start      string
	Selectable func(name string) bool
	ShowHidden bool
	PickDirKey string
}

// fileEntry is one browsable directory entry.
type fileEntry struct {
	name, path string
	isDir      bool
	sel        bool
}

// FilePicker is the shared directory browser (TUI-406b): os.ReadDir on
// open/descend/back, dirs first then files by name, a `/` name filter,
// an `h` hidden toggle, gg/G + j/k/↑/↓ navigation (the List
// conventions), enter selects (directories descend; only Selectable
// files emit FilePickedMsg), esc cancels. Row rendering delegates to
// the virtualized List, so a 10k-entry directory still renders
// `height` lines. View NEVER shows the absolute root — paths display
// as RootLabel + relative remainder. Long names truncate, never wrap.
//
// The picker is the widgets package's declared exception to "Update is
// pure": directory reads happen synchronously in Update (a browser
// that deferred every stat into cmds would be unusable), and read
// failures degrade to an inline error state instead of a message.
type FilePicker struct {
	theme  *theme.Theme
	list   *List
	width  int
	height int // list rows; View adds header + footer lines

	root, rootLabel string
	sel             func(name string) bool

	dir     string
	entries []fileEntry
	err     string

	hidden   bool
	filterOn bool
	filter   string
	pendingG bool

	pickDirKey string // footer text for the write-target key ("" = unbound)

	nav struct {
		Up, Down, Filter, Hidden, Back, Enter, Cancel, PickDir key.Binding
	}
}

// NewFilePicker opens the browser at opts.Root (width x height; height
// counts list rows). A failed initial read lands in the error state.
func NewFilePicker(th *theme.Theme, width, height int, opts FilePickerOptions) *FilePicker {
	if height < 1 {
		height = 1
	}
	label := opts.RootLabel
	if label == "" {
		label = opts.Root
	}
	if opts.Start == "" {
		opts.Start = opts.Root
	}
	p := &FilePicker{
		theme: th, width: max(width, 8), height: height,
		root: opts.Root, rootLabel: label, sel: opts.Selectable,
		hidden: opts.ShowHidden, dir: opts.Start,
	}
	p.list = NewList(th, p.width, height)
	p.list.SetEmptyMessage("empty directory")
	p.nav.Up = key.NewBinding(key.WithKeys("up", "k"))
	p.nav.Down = key.NewBinding(key.WithKeys("down", "j"))
	p.nav.Filter = key.NewBinding(key.WithKeys("/"))
	p.nav.Hidden = key.NewBinding(key.WithKeys("h"))
	p.nav.Back = key.NewBinding(key.WithKeys("backspace"))
	p.nav.Enter = key.NewBinding(key.WithKeys(theme.KeyEnter))
	p.nav.Cancel = key.NewBinding(key.WithKeys(theme.KeyEsc))
	if opts.PickDirKey != "" {
		p.pickDirKey = opts.PickDirKey
		p.nav.PickDir = key.NewBinding(key.WithKeys(opts.PickDirKey))
	}
	p.refresh()

	return p
}

// SetSize resizes the widget (header/footer stay; height = list rows).
func (p *FilePicker) SetSize(width, height int) {
	p.width, p.height = max(width, 8), max(height, 1)
	p.list.SetSize(p.width, p.height)
	p.refresh()
}

// CurrentDir reports the browsed directory (real path; for owners).
func (p *FilePicker) CurrentDir() string { return p.dir }

// Err reports the read-failure text ("" when healthy).
func (p *FilePicker) Err() string { return p.err }

// relLabel renders path relative to the root under the virtual label
// ("fixture/sub/x.json"); paths outside the root fall back to the
// basename so no absolute temp path can leak into View.
func (p *FilePicker) relLabel(path string) string {
	if p.root == string(filepath.Separator) {
		return path
	}
	base := filepath.Base(p.root)
	if rel, err := filepath.Rel(p.root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return joinLabel(p.rootLabel, filepath.ToSlash(rel))
	}

	return base + "/" + filepath.Base(path)
}

// withinTree reports whether dir is inside root (root "/" is the whole
// filesystem; backspace may climb anywhere below it).
func withinTree(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}

	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dirLabel guarantees the trailing "/" that marks a directory path.
func dirLabel(label string) string {
	if !strings.HasSuffix(label, "/") {
		return label + "/"
	}

	return label
}

// joinLabel glues label and relative remainder with exactly one "/".
func joinLabel(label, rel string) string {
	if rel == "." || rel == "" {
		return label
	}
	if strings.HasSuffix(label, "/") {
		return label + rel
	}

	return label + "/" + rel
}

// refresh re-reads the current directory into entries (dirs first,
// then files, each by name) and pushes the filtered window into the
// list. A read failure keeps the previous listing replaced by the
// error state (never a stale directory).
func (p *FilePicker) refresh() {
	p.err = ""
	ds, err := os.ReadDir(p.dir)
	if err != nil {
		p.err = readDirErrorText(err)
		p.list.SetItems(nil)

		return
	}
	var dirs, files []fileEntry
	for _, d := range ds {
		name := d.Name()
		if !p.hidden && strings.HasPrefix(name, ".") {
			continue
		}
		e := fileEntry{name: name, path: filepath.Join(p.dir, name), isDir: d.IsDir()}
		e.sel = !e.isDir && (p.sel == nil || p.sel(name))
		if e.isDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	byName := func(a, b fileEntry) bool { return a.name < b.name }
	sort.Slice(dirs, func(i, j int) bool { return byName(dirs[i], dirs[j]) })
	sort.Slice(files, func(i, j int) bool { return byName(files[i], files[j]) })
	// A fresh slice, not dirs' backing array: entries is held across renders while
	// the picker appends to it, and an alias of the sorted local would let a later
	// append write through memory the sort above already laid out.
	p.entries = make([]fileEntry, 0, len(dirs)+len(files))
	p.entries = append(p.entries, dirs...)
	p.entries = append(p.entries, files...)

	items := make([]Item, 0, len(p.entries))
	for _, e := range p.entries {
		if p.filter != "" && !strings.Contains(strings.ToLower(e.name), strings.ToLower(p.filter)) {
			continue
		}
		label := e.name
		if e.isDir {
			label += "/"
		}
		if !e.sel && !e.isDir {
			label = p.theme.Dim.Render(label) // unselectable files stay visible but faint
		}
		items = append(items, Item{Label: label, Data: e})
	}
	p.list.SetItems(items)
}

// readDirErrorText names a read failure WITHOUT the absolute path
// (fs.PathError carries the real path; goldens must never see it).
func readDirErrorText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}

	return err.Error()
}

// visibleEntry fetches the entry under the list cursor.
func (p *FilePicker) visibleEntry() (fileEntry, bool) {
	item, ok := p.list.Selected()
	if !ok {
		return fileEntry{}, false
	}
	e, ok := item.Data.(fileEntry)

	return e, ok
}

// Update is the state machine: `/` opens filter input, esc closes it
// (or cancels the picker when empty), h toggles hidden, backspace
// ascends, gg/G and the List nav keys move, enter descends or selects.
// Any other message is ignored with a nil command.
func (p *FilePicker) Update(msg tea.Msg) (*FilePicker, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	if p.filterOn {
		return p.updateFilter(km)
	}

	text := km.Text
	switch {
	case key.Matches(km, p.nav.Cancel):
		return p, func() tea.Msg { return FilePickerCanceledMsg{} }
	case key.Matches(km, p.nav.Filter):
		p.filterOn, p.filter = true, ""
		p.refresh()
	case key.Matches(km, p.nav.Hidden):
		p.hidden = !p.hidden
		p.refresh()
	case p.pickDirKey != "" && key.Matches(km, p.nav.PickDir):
		// The write-target leg: the browsed directory itself is the
		// selection (the §J output browse), label trailing "/" and all.
		return p, func() tea.Msg { return FilePickedMsg{Path: p.dir, Label: dirLabel(p.relLabel(p.dir))} }
	case key.Matches(km, p.nav.Back):
		if parent := filepath.Dir(p.dir); p.dir != p.root && withinTree(p.root, p.dir) && parent != p.dir {
			p.dir = parent
			p.refresh()
		}
	case text == "g":
		if p.pendingG {
			p.list.SetCursor(0)
			p.pendingG = false
		} else {
			p.pendingG = true
		}

		return p, nil
	case text == "G":
		p.list.SetCursor(p.list.Len() - 1)
	case key.Matches(km, p.nav.Enter):
		return p.selectEntry()
	default:
		pending := p.pendingG
		_, _ = p.list.Update(msg)
		p.pendingG = pending && text == "g"
	}
	p.pendingG = p.pendingG && text != "G"

	return p, nil
}

// updateFilter edits the `/` substring: printable/backspace edit,
// enter keeps the filter, esc clears it (a non-empty filter exits
// first; only an empty one cancels the picker).
func (p *FilePicker) updateFilter(km tea.KeyPressMsg) (*FilePicker, tea.Cmd) {
	switch {
	case key.Matches(km, p.nav.Enter):
		p.filterOn = false
	case key.Matches(km, p.nav.Cancel):
		if p.filter == "" {
			p.filterOn = false

			return p, func() tea.Msg { return FilePickerCanceledMsg{} }
		}
		p.filterOn, p.filter = false, ""
		p.refresh()
	case key.Matches(km, p.nav.Back):
		p.filter = dropLastRune(p.filter)
		p.refresh()
	default:
		if r, ok := printable(km.Text); ok {
			p.filter += string(r)
			p.refresh()
		}
	}

	return p, nil
}

// selectEntry descends into directories and emits FilePickedMsg for
// selectable files; unselectable files and empty lists are ignored.
func (p *FilePicker) selectEntry() (*FilePicker, tea.Cmd) {
	e, ok := p.visibleEntry()
	if !ok {
		return p, nil
	}
	if e.isDir {
		p.dir = e.path
		// The filter matched the directory itself; carrying it into the
		// directory usually leaves it looking empty (live UAT).
		p.filterOn, p.filter = false, ""
		p.refresh()

		return p, nil
	}
	if !e.sel {
		return p, nil
	}
	path, label := e.path, p.relLabel(e.path)

	return p, func() tea.Msg { return FilePickedMsg{Path: path, Label: label} }
}

// SelectRow moves the list cursor to index and runs the entry selection
// Enter runs (Task 8.3 click-select): directories descend, selectable
// files commit through FilePickedMsg, unselectable entries only move the
// cursor. An out-of-range index changes nothing.
func (p *FilePicker) SelectRow(index int) tea.Cmd {
	if index < 0 || index >= p.list.Len() {
		return nil
	}
	p.list.SetCursor(index)
	_, cmd := p.selectEntry()

	return cmd
}

// RowHits reports the DRAWN entry rows relative to the picker's own View
// origin: below the header line (two lines while the `/` filter is open)
// and above the footer line, one cell tall per visible list entry, each
// carrying its absolute entry index. The error state draws no entry
// rows. The root translates these into absolute cells the same way it
// centers the box in View (Task 8.3 click-select).
func (p *FilePicker) RowHits() []RowHit {
	if p.err != "" {
		return nil
	}
	head := 1
	if p.filterOn {
		head++
	}
	top, count := p.list.Window()
	rows := make([]RowHit, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, RowHit{
			Rect:  geom.Rect{X: 0, Y: head + i, W: p.width, H: 1},
			Index: top + i,
		})
	}

	return rows
}

// View renders header (virtual current path + filter echo), the
// virtualized list window (or the error line), and the keymap footer —
// header + height rows + footer, every line at most width cells.
func (p *FilePicker) View() string {
	head := p.theme.Accent.Render(clip(dirLabel(p.relLabel(p.dir)), p.width, truncateTail(p.theme)))
	body := p.list.View()
	if p.err != "" {
		body = p.theme.Status(theme.KindError, clip("cannot read "+p.relLabel(p.dir)+": "+p.err, p.width, truncateTail(p.theme)))
	}
	if p.filterOn {
		head += "\n" + p.theme.Deemphasized.Render(clip("/"+p.filter, p.width, truncateTail(p.theme)))
	}
	seps := p.theme.Separator()
	// Hotkey convention (UAT round 4): the actionable key glyphs carry
	// the bold-accent HotKey style; the action words keep the dim base
	// explicitly (lipgloss only wraps a whole-string Render, so the
	// spans between glyphs must carry the base themselves).
	base := p.theme.Dim
	hk := func(k string) string { return p.theme.Key(k) }
	// The dir-pick hint leads the tail segments (right after "move"):
	// the modal is ~58 cells and this footer already clips there, so an
	// appended hint would never reach the operator's eyes. Owners that
	// did not bind the key render the byte-identical default footer.
	tail := hk("gg/G") +
		base.Render(" top"+seps+" ") + hk("/") +
		base.Render(" filter"+seps+" ") + hk("h") +
		base.Render(" hidden"+seps+" ") + hk("enter") +
		base.Render(" open"+seps+" ") + hk("esc") + base.Render(" cancel")
	if p.pickDirKey != "" {
		tail = hk(p.pickDirKey) + base.Render(" set folder"+seps+" ") + tail
	}
	hints := hk("j/k") + base.Render(" move"+seps+" ") + tail

	return strings.Join(append([]string{head}, linesOf(body)...), "\n") + "\n" +
		clip(hints, p.width, truncateTail(p.theme))
}

// linesOf splits a rendered block into lines (empty block = no lines).
func linesOf(s string) []string {
	if s == "" {
		return nil
	}

	return strings.Split(s, "\n")
}

// printable reports whether text is a single printable rune (the
// widgets' own copy so the leaf stays independent of pages).
func printable(text string) (rune, bool) {
	rs := []rune(text)
	if len(rs) != 1 || !unicode.IsPrint(rs[0]) {
		return 0, false
	}

	return rs[0], true
}

// dropLastRune removes the final rune from s ("" stays "").
func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}

	return string(r[:len(r)-1])
}
