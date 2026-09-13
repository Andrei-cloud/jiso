package widgets

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/tui/theme"
)

// tcTheme/asciiTheme pin the two golden modes; NewWith reads no env,
// so a developer environment cannot flip glyph sets mid-test.
func tcTheme(t *testing.T) *theme.Theme {
	t.Helper()
	return theme.NewWith(colorprofile.TrueColor, true)
}

func asciiTheme(t *testing.T) *theme.Theme {
	t.Helper()
	return theme.NewWith(colorprofile.ASCII, true)
}

// ch builds a text key press (matches key bindings "j", "y", ...).
func ch(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Text: string(c)} }

// special builds a code-only key press (up, pgup, esc, ...).
func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// lines splits a View into its rendered lines.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
