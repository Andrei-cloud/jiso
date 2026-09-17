// root_analyze_unparsable.go maps the collected failure samples into the §J
// reviewer roster (offset, length, reason, head bytes, stop byte, parsed
// fields). Root only maps data — the page renders and styles.
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
