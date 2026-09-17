package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// root_wizard_sources.go is the wizard's read side: the candidate lists the
// modal's browse steps show (specs, remembered tx files, a directory, the tx
// file's transaction entries) and the two pure helpers they need. Nothing here
// touches bubbletea state, the model or the send path -- which is why the split
// is safe: the state machine in root_wizard.go asks for a []pages.WizardItem and
// gets one.
// wizardSpecItems lists spec candidates: the current spec first, then the
// other *.json files in its directory (one level, capped).
func wizardSpecItems(current string) []pages.WizardItem {
	return wizardDirItems(current, current)
}

// wizardFileItems lists tx candidates: the remembered files (newest first)
// and the current file, then the *.json files beside the current one.
func wizardFileItems(th *theme.Theme, current string, recents []string) []pages.WizardItem {
	seen := map[string]bool{}
	items := make([]pages.WizardItem, 0, 16)
	add := func(path string, current bool) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		items = append(items, pages.WizardItem{
			Label: filepath.Base(path), Path: path, Current: current,
		})
	}
	for _, r := range recents {
		add(r, r == current)
	}
	add(current, true)
	// the dir of the current file offers siblings; listed candidates
	// must actually carry transactions (spec/lock files are noise —
	// UAT: picking one led to an empty send step).
	for _, it := range wizardDirItems(current, current) {
		if len(wizardTemplates(th, it.Path)) > 0 {
			add(it.Path, it.Current)
		}
	}
	// Nothing known at all (fresh session, no tx file yet): offer the
	// working directory's tx files rather than an empty step.
	if len(items) == 0 {
		for _, it := range wizardDirItems(".", current) {
			if len(wizardTemplates(th, it.Path)) > 0 {
				add(it.Path, it.Current)
			}
		}
	}

	return items
}

// wizardDirItems enumerates *.json files in path's directory (sorted by
// name, capped) tagging the current file.
func wizardDirItems(path, current string) []pages.WizardItem {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	items := make([]pages.WizardItem, 0, 16)
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), jsonExt) {
			continue
		}
		full := filepath.Join(dir, e.Name())
		items = append(items, pages.WizardItem{
			Label: e.Name(), Path: full, Current: full == current,
		})
	}
	// Current first so Enter with no navigation keeps the live value;
	// ReadDir order (alphabetical) otherwise.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Current != items[j].Current {
			return items[i].Current
		}

		return items[i].Label < items[j].Label
	})
	if len(items) > wizardTemplateLimit {
		items = items[:wizardTemplateLimit]
	}

	return items
}

// wizardTemplates extracts the send-step rows from a tx file: name, MTI
// (field 0), masked PAN (field 2) and amount (field 4) when present;
// entries that carry none of them fall back to the description. A bad file
// yields an empty list (the step renders "no templates").
func wizardTemplates(th *theme.Theme, path string) []pages.WizardTemplate {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []struct {
		Type        string         `json:"type"`
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Fields      map[string]any `json:"fields"`
	}
	if json.Unmarshal(data, &entries) != nil {
		return nil
	}
	tmpl := make([]pages.WizardTemplate, 0, len(entries))
	for _, e := range entries {
		if !strings.EqualFold(e.Type, "transaction") {
			continue
		}
		t := pages.WizardTemplate{Name: e.Name, Description: e.Description}
		t.MTI = txFieldString(e.Fields, "0")
		if pan := txFieldString(e.Fields, "2"); pan != "" {
			t.PAN = wizardMaskPAN(th, pan)
		}
		t.Amount = txFieldString(e.Fields, "4")
		tmpl = append(tmpl, t)
		if len(tmpl) >= wizardTemplateLimit {
			break
		}
	}

	return tmpl
}

// txFieldString reads one field value as a trimmed string ("" when absent
// or unparsable; JSON numbers are rendered without scientific notation
// via the %v default, which is fine for the small integers tx files use).
func txFieldString(fields map[string]any, key string) string {
	v, ok := fields[key]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(
			strings.ReplaceAll(strconv.FormatFloat(x, 'f', -1, 64), " ", "")), "0"), ".")
	default:
		return strings.TrimSpace(strings.Trim(fmt.Sprint(x), "[]"))
	}
}

// wizardMaskPAN keeps the first six and last four digits of a long PAN
// ("411111~1111" under the ASCII set), passing short test PANs through (the
// Masked column). It used to hardcode "…", which wrote a non-ASCII
// byte into ASCII mode -- nothing in the goldens exercised it, so nothing noticed.
func wizardMaskPAN(th *theme.Theme, pan string) string {
	r := []rune(pan)
	if len(r) <= 10 {
		return pan
	}

	return th.ElideMiddle(pan, 6, 4)
}

// dedupePaths drops later duplicates, preserving order.
func dedupePaths(paths []string) []string {
	seen := map[string]bool{}
	out := paths[:0]
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}

	return out
}
