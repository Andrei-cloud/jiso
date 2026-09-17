// settings_view.go renders the §L body: the title line
// "SETTINGS (session config - edits apply live)" with the right-aligned
// "[w] save to <path>" slot, the two-column key/value grid (label
// column padded; markers ●/✓/✗ after the value; env/session
// provenance as a muted tag so default rows stay verbatim),
// and the stacked single-column fallback below frame.FullWidth. An
// invalid committed value renders as inline red text under its row.
// The save overlay replaces the grid while open (target path +
// changed-keys diff; w confirms, Esc cancels). The footer states the
// live-apply scope: "applies to next operation".
package pages

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

const (
	titleSettings = "SETTINGS"

	// settingsLabelCol is the padded label column of both grid
	// columns; settingsGridCols is the two-column split threshold.
	settingsLabelCol = 22
)

// View renders the §L body for the frame's content area.
func (s *Settings) View() tea.View {
	w, h := frame.ContentSize(s.width, s.height)

	return tea.NewView(s.render(w, h))
}

// render composes header + (grid | save overlay) + footer, clipped to
// exactly h lines of at most w cells.
func (s *Settings) render(w, h int) string {
	head := s.headerLine(w)
	if line := s.noteLine(w); line != "" {
		head += "\n" + line
	}

	footer := s.footerLines(w)

	if s.state.Save != nil {
		return clipBlockStyled(s.th, head+"\n"+s.overlayBody()+"\n"+footer, h, w)
	}

	if w >= frame.FullWidth {
		return clipBlockStyled(s.th, head+"\n"+s.gridWide(w)+"\n"+footer, h, w)
	}

	return clipBlockStyled(s.th, head+"\n"+s.gridStacked(w)+"\n"+footer, h, w)
}

// headerLine is the title row with the right-aligned save
// slot naming the XDG target.
func (s *Settings) headerLine(w int) string {
	dash := "\u2014"
	if s.th.ASCII {
		dash = "-"
	}
	left := titleLine(s.th, titleSettings+" (session config "+dash+" edits apply live)")
	right := s.th.Deemphasized.Render("[w] save to " + dashIf(s.th, s.state.ConfigPath))

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return clipCells(left, w, clipTail(s.th))
	}

	return left + strings.Repeat(" ", gap) + right
}

// noteLine is the root-stamped page-level text (no changes, malformed
// config file, ...), rendered as a warn line under the header.
func (s *Settings) noteLine(w int) string {
	if s.state.Note == "" {
		return ""
	}

	return clipCells(s.th.Status(theme.KindWarn, s.state.Note), w, clipTail(s.th))
}

// gridWide renders the two-column grid: rows snake
// (0,1), (2,3), ... left cell then right cell, exactly the §L pairing.
func (s *Settings) gridWide(w int) string {
	valueCol := max((w-settingsLabelCol*2)/2, 12)
	var b strings.Builder

	for i := 0; i < len(s.state.Rows); i += 2 {
		left := s.cell(i, valueCol)
		right := ""
		if i+1 < len(s.state.Rows) {
			right = s.cell(i+1, valueCol)
		}
		b.WriteString(left + right + "\n")

		if errs := s.errorLines(i, i+1, w); errs != "" {
			b.WriteString(errs)
		}
	}

	return strings.TrimSuffix(b.String(), "\n")
}

// gridStacked is the narrow fallback: one row per line, §L order.
func (s *Settings) gridStacked(w int) string {
	lines := make([]string, 0, len(s.state.Rows))
	for i := range s.state.Rows {
		lines = append(lines, clipCells(s.cell(i, max(w-settingsLabelCol, 10)), w, clipTail(s.th)))
		if errs := s.errorLines(i, -1, w); errs != "" {
			lines = append(lines, strings.TrimSuffix(errs, "\n"))
		}
	}

	return strings.Join(lines, "\n")
}

// cell renders one "label value marker tag" grid cell; the focused
// row's label takes the accent and the edited field carries the caret.
func (s *Settings) cell(i, valueCol int) string {
	row, ok := s.row(i)
	if !ok {
		return ""
	}
	focused := s.cursor == i
	labelStyle, valueStyle := s.th.Deemphasized, s.th.TextPrimary
	if focused {
		labelStyle, valueStyle = s.th.Accent, s.th.TextPrimary
	}

	value := s.RowValue(i)
	shown := dashIf(s.th, value)
	if value != "" {
		shown = value + settingsMarker(s.th, row.Marker) + settingsSourceTag(s.th, row.Source)
	} else if row.Marker != "" {
		shown = dashIf(s.th, "") + settingsMarker(s.th, row.Marker)
	}
	if s.editing && s.editRow == i {
		shown += cursorGlyph(s.th)
	}

	return labelStyle.Render(padRight(row.Label, settingsLabelCol)) +
		valueStyle.Render(padRight(shown, valueCol))
}

// errorLines renders the inline red validation text under a grid row
// (or row pair), one line per side that carries an error.
func (s *Settings) errorLines(left, right, w int) string {
	var b strings.Builder
	for _, i := range []int{left, right} {
		row, ok := s.row(i)
		if !ok || row.Error == "" {
			continue
		}
		b.WriteString(strings.Repeat(" ", settingsLabelCol) +
			clipCells(s.th.Status(theme.KindError, row.Error), max(w-settingsLabelCol, 8), clipTail(s.th)) + "\n")
	}

	return b.String()
}

// footerLines is the action line (or the last save result)
// plus the live-apply scope line.
func (s *Settings) footerLines(w int) string {
	sep := s.th.Separator()
	var line string
	if s.state.SavedLine != "" {
		kind := theme.KindError
		if s.state.SavedOK {
			kind = theme.KindOK
		}
		line = s.th.Status(kind, s.state.SavedLine)
	} else {
		line = s.th.Dim.Render("Enter edit field" + sep + "w persists user defaults (YAML)" + sep + "Esc discards")
	}

	return clipCells(line, w, clipTail(s.th)) + "\n" +
		clipCells(s.th.Dim.Render("applies to next operation"), w, clipTail(s.th))
}

// overlayBody renders the save-confirm: target path, the changed-keys
// diff, and the confirm/cancel hint (the overlay owns the keyboard).
func (s *Settings) overlayBody() string {
	sep := s.th.Separator()
	var b strings.Builder
	b.WriteString(titleLine(s.th, "SAVE USER CONFIG (YAML)") + "\n")
	b.WriteString(s.th.Deemphasized.Render("target: "+dashIf(s.th, s.state.Save.Path)) + "\n")
	for _, d := range s.state.Save.Diff {
		b.WriteString(s.th.TextPrimary.Render("  "+d) + "\n")
	}
	b.WriteString(s.th.Deemphasized.Render("[w] confirm" + sep + "Esc cancel (nothing is written)"))

	return b.String()
}

// settingsMarker maps the row marker glyph, ASCII-safe (goldens pin
// 7-bit bytes): ● -> "*", ✓ -> "[ok]", ✗ -> "[x]".
func settingsMarker(th *theme.Theme, marker string) string {
	if marker == "" {
		return ""
	}
	suffix := marker
	if th.ASCII {
		suffix = map[string]string{"\u25cf": "*", "\u2713": "[ok]", "\u2717": "[x]"}[marker]
		if suffix == "" {
			suffix = marker
		}
	}

	return " " + th.Deemphasized.Render(suffix)
}

// settingsSourceTag decorates non-obvious provenance (env overrides
// and live session edits) as a muted tag; config/default rows stay
// verbatim.
func settingsSourceTag(th *theme.Theme, source string) string {
	if source != appSourceEnv && source != appSourceSession {
		return ""
	}

	return th.Dim.Render("  " + source)
}

// Provenance tokens mirrored from the app façade's source labels (the
// page fence forbids importing internal/app; the root bridge stamps
// these strings into SettingsRow.Source).
const (
	appSourceEnv     = "env"
	appSourceSession = "session"
)
