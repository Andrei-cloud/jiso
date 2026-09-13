// root_analyze_unparsable.go builds the §J unparsable-message reviewer
// roster (UAT round 6: the tester must see WHERE framing breaks and
// WHAT the analyzer choked on, not just a black-box count). Each
// collected sample becomes a row carrying its stream offset, byte
// length, unpack reason, the raw head bytes, the byte where parsing
// stopped, and the fields that unpacked before the failure. Root only
// maps data — the page renders the describe panel and paints the
// unparsed bytes, so no styling happens here.
package tui

import (
	"strconv"

	"jiso/internal/analyzer"
	"jiso/internal/tui/pages"
)

// analyzeUnparsableRows turns collected failure samples into reviewer
// rows; an empty slice (no unparsable messages) yields nil so the run
// step shows no affordance.
func analyzeUnparsableRows(samples []analyzer.UnparsableSample) []pages.AnalyzeUnparsableRow {
	if len(samples) == 0 {
		return nil
	}
	rows := make([]pages.AnalyzeUnparsableRow, 0, len(samples))
	for _, s := range samples {
		rows = append(rows, pages.AnalyzeUnparsableRow{
			Offset:   strconv.FormatInt(s.Offset, 10),
			Length:   strconv.Itoa(s.Length),
			Reason:   s.Reason,
			Head:     s.Head,
			FailedAt: s.FailedAt,
			Fields:   unparsableFieldViews(s.Fields),
		})
	}

	return rows
}

// unparsableFieldViews maps the analyzer's parsed-field describe rows
// into the page's view model; nil stays nil so the pane shows no panel.
func unparsableFieldViews(fields []analyzer.SampleField) []pages.UnparsableField {
	if len(fields) == 0 {
		return nil
	}
	views := make([]pages.UnparsableField, 0, len(fields))
	for _, f := range fields {
		views = append(views, pages.UnparsableField{ID: f.ID, Name: f.Name, Value: f.Value})
	}

	return views
}
