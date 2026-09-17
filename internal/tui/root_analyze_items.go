// root_analyze_items.go owns the §J generated-item selection. Root keeps
// the roster (built once per run attach, with each item's file form) and
// the deselection keys; the write leg persists exactly
// app.AnalyzeOutput.SelectedItems().
package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// analyzeItemRows builds the picker roster from a run's generated items
// (all included on a fresh attach; the JSON preview is what the write
// stores).
func analyzeItemRows(out *app.AnalyzeOutput) []pages.AnalyzeItemRow {
	items := out.GeneratedItems()
	rows := make([]pages.AnalyzeItemRow, 0, len(items))
	for _, it := range items {
		preview := "{}"
		if b, err := config.MarshalItemPreview(it, "", "  "); err == nil {
			preview = string(b)
		}
		rows = append(rows, pages.AnalyzeItemRow{
			Key:      app.ItemKey(it),
			Name:     it.Name,
			Kind:     string(it.Type),
			Group:    itemGroup(it),
			Included: true,
			Preview:  preview,
		})
	}

	return rows
}

// itemGroup is the toggle-coupling key: a transaction and its dataset share
// one group, so selecting one selects the other. Items with no partner have
// an empty group and toggle alone.
func itemGroup(it config.Item) string {
	switch it.Type {
	case config.TypeTransaction:
		return it.DatasetName // "" when the transaction has no dataset
	case config.TypeDataset:
		return it.Name
	default:
		return ""
	}
}

// analyzeRunSummary is the run-step result line: counts plus the two next
// actions.
func analyzeRunSummary(out *app.AnalyzeOutput) string {
	n := len(out.GeneratedItems())

	return strconv.Itoa(n) + " generated ConfigItem(s) · [x] pick which to write · w writes the included set"
}

// handleAnalyzeItemsApply folds the picker's Enter: store the
// deselection on the run output (the write leg persists exactly
// SelectedItems) and report the pending selection on the toast line.
func (m *RootModel) handleAnalyzeItemsApply(msg pages.AnalyzeItemsApplyMsg) (tea.Model, tea.Cmd) {
	if m.analyzeOutput == nil {
		return m, nil
	}
	m.analyzeExcluded = msg.Excluded
	m.analyzeOutput.SetExcluded(msg.Excluded)
	sel, total := len(m.analyzeOutput.SelectedItems()), len(m.analyzeOutput.GeneratedItems())
	m.analyzeWriteLine = "selected " + strconv.Itoa(sel) + " of " + strconv.Itoa(total) +
		" items - w writes to " + m.analyzeOutput.OutputFile
	m.analyzeWriteOK = true
	m.debug.logf("analyze items selection applied: %d of %d", sel, total)

	return m, nil
}

// analyzeItemIncluded reports whether a roster row is in the pending
// write set (the excluded keys are the picker's deselections).
func (m *RootModel) analyzeItemIncluded(key string) bool {
	for _, k := range m.analyzeExcluded {
		if k == key {
			return false
		}
	}

	return true
}

// analyzeItemsView re-stamps each roster row's Included from the
// pending deselection; the picker's local set re-seeds from this when
// it opens or closes without applying.
func (m *RootModel) analyzeItemsView() []pages.AnalyzeItemRow {
	if len(m.analyzeItemRows) == 0 {
		return nil
	}
	rows := make([]pages.AnalyzeItemRow, len(m.analyzeItemRows))
	copy(rows, m.analyzeItemRows)
	for i := range rows {
		rows[i].Included = m.analyzeItemIncluded(rows[i].Key)
	}

	return rows
}
