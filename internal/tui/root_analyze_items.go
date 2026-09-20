// root_analyze_items.go owns the §J generated-item selection. Root keeps
// the roster (built once per run attach, with each item's file form, its
// route response code and its scenario links) and the deselection keys;
// the write leg persists exactly app.AnalyzeOutput.SelectedItems.
package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	json "github.com/goccy/go-json"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// analyzeItemRows builds the picker roster from a run's generated items
// (all included on a fresh attach; the JSON preview is what the write
// stores). Route rows carry the response code they answer with, and every
// row carries the keys it belongs with, so the picker can complete a
// scenario from any single pick.
func analyzeItemRows(out *app.AnalyzeOutput) []pages.AnalyzeItemRow {
	items := out.GeneratedItems()
	links := analyzeItemLinks(items)
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
			RC:       itemResponseCode(it),
			Links:    links[app.ItemKey(it)],
			Included: true,
			Preview:  preview,
		})
	}

	return rows
}

// itemResponseCode is the DE39 a mock route answers with ("" for rows
// that answer nothing) - the picker's RC column.
func itemResponseCode(it config.Item) string {
	if it.Type != config.TypeMockRoute {
		return ""
	}
	if c, ok := it.ResponseFields["39"].(string); ok {
		return c
	}

	return ""
}

// The scaffold's template naming, used to pair a reversal with the
// request it reverses: "Tx <shape> #N" and "Reversal for <shape> #N"
// carry the same shape, so the name itself is the pairing key.
const (
	scaffoldTxPrefix       = "Tx "
	scaffoldReversalPrefix = "Reversal for "
)

// analyzeItemLinks wires the picker's scenario closure: a transaction
// links the mock routes whose match its template fields satisfy, and a
// scaffolded reversal links the request pair it reverses. The page turns
// these into an include-only closure, so selecting a purchase selects
// its routes and (when captured) its reversal and the reversal's own
// routes - the complete scenario for further testing (UAT).
func analyzeItemLinks(items []config.Item) map[string][]string {
	links := map[string][]string{}
	link := func(a, b string) {
		for _, have := range links[a] {
			if have == b {
				return
			}
		}
		links[a] = append(links[a], b)
		links[b] = append(links[b], a)
	}

	type txView struct {
		key    string
		fields map[string]any
	}
	var txs []txView
	shapes := map[string]string{} // scaffold shape -> first tx key with it

	for _, it := range items {
		if it.Type != config.TypeTransaction {
			continue
		}
		var f map[string]any
		_ = json.Unmarshal(it.Fields, &f)
		key := app.ItemKey(it)
		txs = append(txs, txView{key: key, fields: f})
		if shape, ok := strings.CutPrefix(it.Name, scaffoldReversalPrefix); ok {
			if partner, found := shapes[shape]; found {
				link(key, partner) // the reversal pairs with the request it reverses
			}
			continue
		}
		if shape, ok := strings.CutPrefix(it.Name, scaffoldTxPrefix); ok {
			if shapes[shape] == "" {
				shapes[shape] = key
			}
		}
	}

	for _, it := range items {
		if it.Type != config.TypeMockRoute {
			continue
		}
		routeKey := app.ItemKey(it)
		for _, tx := range txs {
			if routeMatchesTemplate(it.MatchFields, tx.fields) {
				link(routeKey, tx.key)
			}
		}
	}

	return links
}

// routeMatchesTemplate reports whether every route match field is
// carried, with the same value, by the transaction template's fields -
// the same subset rule the mock server applies at match time.
func routeMatchesTemplate(match, fields map[string]any) bool {
	if len(match) == 0 {
		return false
	}
	for k, mv := range match {
		fv, ok := fields[k]
		if !ok || fmt.Sprintf("%v", mv) != fmt.Sprintf("%v", fv) {
			return false
		}
	}

	return true
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
