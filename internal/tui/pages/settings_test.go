// settings_test.go pins the §L page contract (SCR-512): the grid
// renders root-pushed rows; Enter opens the focused field as a text
// input (printable/backspace edit, Enter commits, Esc reverts the
// field); a committed-but-rejected draft stays visible beside its
// inline error until the snapshot agrees; the save overlay keys route
// confirm/cancel; and the keyboard is claimed while editing or the
// overlay is open. No App, no clock, no filesystem above the seam.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// settingsFixtureState is the wireframe §L data in root-pushed shape
// (fixed display strings; no clock, no terminal paths).
func settingsFixtureState(th *theme.Theme) SettingsState {
	return SettingsState{
		ConfigPath: "./user/config.yaml",
		Rows: []SettingsRow{
			{Key: "reconnect-attempts", Label: "reconnect-attempts", Value: "3", Source: "config"},
			{Key: "connect-timeout", Label: "connect-timeout", Value: "5s", Source: "default"},
			{Key: "total-connect-timeout", Label: "total-connect-timeout", Value: "10s", Source: "default"},
			{Key: "response-timeout", Label: "response-timeout", Value: "5s", Source: "default"},
			{Key: "listen-timeout", Label: "listen-timeout", Value: "5m", Source: "default"},
			{Key: "hex", Label: "hex output", Value: "off", Source: "config", Marker: "\u25cf"},
			{Key: "visa-station-id", Label: "visa-station-id", Value: "001234", Source: "session"},
			{Key: "tls-config", Label: "tls-config", Value: "./testdata/certs/tls_config.json", Source: "config", Marker: "\u2713"},
			{Key: "spec", Label: "spec", Value: "./specs/visa.json", Source: "config"},
			{Key: "tx-file", Label: "tx file", Value: "./transactions/pool.json", Source: "config"},
			{Key: "db", Label: "db", Value: "./sessions.db", Source: "session"},
			{Key: "output", Label: "output", Value: "text", Source: "default"},
		},
	}
}

func newSettingsFixture(t *testing.T, state SettingsState, w, h int) *Settings {
	t.Helper()

	s := NewSettings(asciiTheme(t))
	s.SetState(state)
	_, _ = s.Update(windowSize(w, h))

	return s
}

func sendKey(s *Settings, msg tea.Msg) []tea.Msg {
	_, cmd := s.Update(msg)
	if cmd == nil {
		return nil
	}
	if m := cmd(); m != nil {
		return []tea.Msg{m}
	}

	return nil
}

// typeInto replays \x08 as backspace and every other rune as a
// printable key (the §L edit buffer is prefilled with the field value,
// so tests clear it first).
func typeInto(s *Settings, text string) {
	for _, r := range text {
		if r == '\x08' {
			sendKey(s, tea.KeyPressMsg{Code: tea.KeyBackspace})

			continue
		}
		sendKey(s, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestSettingsGridRendersRows(t *testing.T) {
	t.Parallel()

	s := newSettingsFixture(t, settingsFixtureState(nil), 124, 32)
	body := s.View().Content

	for _, want := range []string{
		"SETTINGS", "edits apply live", "[w] save to ./user/config.yaml",
		"reconnect-attempts", "3", "connect-timeout", "5s",
		"hex output", "off", "tls-config", "[ok]", "tx file",
		"applies to next operation",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if s.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", s.Cursor())
	}
}

func TestSettingsEnterOpensEditAndCommits(t *testing.T) {
	t.Parallel()

	s := newSettingsFixture(t, settingsFixtureState(nil), 124, 32)

	sendKey(s, press('j'))
	sendKey(s, press('j')) // row 2: total-connect-timeout
	if s.Cursor() != 2 {
		t.Fatalf("cursor = %d, want 2", s.Cursor())
	}
	sendKey(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !s.Editing() || s.EditBuffer() != "10s" {
		t.Fatalf("edit state: %v %q", s.Editing(), s.EditBuffer())
	}
	if !s.ClaimsKeyboard() {
		t.Fatal("editing must claim the keyboard")
	}

	typeInto(s, "\x08\x08\x0830s") // clear the prefilled "10s", type "30s"
	msgs := sendKey(s, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(msgs) != 1 {
		t.Fatalf("commit msgs = %v", msgs)
	}
	commit, ok := msgs[0].(SettingsCommitMsg)
	if !ok || commit.Key != "total-connect-timeout" || commit.Value != "30s" {
		t.Fatalf("commit = %+v", msgs[0])
	}
	if s.Editing() {
		t.Fatal("Enter must close the edit mode")
	}
	// The draft stays visible until the snapshot agrees.
	if s.RowValue(2) != "30s" {
		t.Fatalf("draft = %q, want the committed 30s", s.RowValue(2))
	}

	st := settingsFixtureState(nil)
	st.Rows[2].Value = "30s"
	s.SetState(st) // snapshot caught up; the draft drops out
	if s.RowValue(2) != "30s" {
		t.Fatalf("RowValue = %q, want 30s after refresh", s.RowValue(2))
	}
}

func TestSettingsEscRevertsField(t *testing.T) {
	t.Parallel()

	s := newSettingsFixture(t, settingsFixtureState(nil), 124, 32)

	sendKey(s, tea.KeyPressMsg{Code: tea.KeyEnter}) // row 0 = reconnect-attempts
	sendKey(s, tea.KeyPressMsg{Code: '9', Text: "9"})
	msgs := sendKey(s, tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(msgs) != 0 {
		t.Fatalf("edit-Esc must not pop: %v", msgs)
	}
	if s.Editing() || s.RowValue(0) != "3" {
		t.Fatalf("revert failed: editing=%v value=%q", s.Editing(), s.RowValue(0))
	}
}

func TestSettingsInvalidCommitShowsInlineError(t *testing.T) {
	t.Parallel()

	s := newSettingsFixture(t, settingsFixtureState(nil), 124, 32)

	sendKey(s, press('j')) // row 1 = connect-timeout
	sendKey(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	typeInto(s, "\x08\x08bogus") // clear the prefilled "5s", type "bogus"
	sendKey(s, tea.KeyPressMsg{Code: tea.KeyEnter})

	st := settingsFixtureState(nil)
	st.Rows[1].Error = "invalid duration \"bogus\""
	s.SetState(st)

	body := s.View().Content
	if !strings.Contains(body, "invalid duration") {
		t.Fatalf("inline error missing:\n%s", body)
	}
	if s.RowValue(1) != "bogus" {
		t.Fatalf("attempted value must stay visible, got %q", s.RowValue(1))
	}
}

func TestSettingsSaveOverlayKeys(t *testing.T) {
	t.Parallel()

	st := settingsFixtureState(nil)
	st.Save = &SettingsSaveOverlay{
		Path: "./user/config.yaml",
		Diff: []string{"connect-timeout: 5s -> 8s"},
	}
	s := newSettingsFixture(t, st, 120, 32)

	if !s.ClaimsKeyboard() {
		t.Fatal("the overlay must claim the keyboard")
	}
	body := s.View().Content
	for _, want := range []string{"SAVE USER CONFIG", "target: ./user/config.yaml", "connect-timeout: 5s -> 8s"} {
		if !strings.Contains(body, want) {
			t.Errorf("overlay lacks %q", want)
		}
	}

	if msgs := sendKey(s, press('w')); len(msgs) != 1 {
		if _, ok := msgs[0].(SettingsSaveConfirmMsg); !ok {
			t.Fatalf("overlay w = %T, want SettingsSaveConfirmMsg", msgs[0])
		}
	}
	if msgs := sendKey(s, tea.KeyPressMsg{Code: tea.KeyEscape}); len(msgs) != 1 {
		if _, ok := msgs[0].(SettingsSaveCancelMsg); !ok {
			t.Fatalf("overlay Esc = %T, want SettingsSaveCancelMsg", msgs[0])
		}
	}
}

func TestSettingsGridKeys(t *testing.T) {
	t.Parallel()

	s := newSettingsFixture(t, settingsFixtureState(nil), 124, 32)

	if msgs := sendKey(s, press('w')); len(msgs) == 0 {
		t.Fatal("w must ask root to save")
	} else if _, ok := msgs[0].(SettingsSaveMsg); !ok {
		t.Fatalf("w = %T", msgs[0])
	}
	if msgs := sendKey(s, press('r')); len(msgs) == 0 {
		t.Fatal("r must ask root to reload")
	}
	if msgs := sendKey(s, tea.KeyPressMsg{Code: tea.KeyEscape}); len(msgs) == 0 {
		t.Fatal("Esc must ask the router to pop")
	}

	// Cursor clamps at both ends (k at top stays 0, j/G walk down).
	for range 20 {
		sendKey(s, press('j'))
	}
	if s.Cursor() != len(settingsFixtureState(nil).Rows)-1 {
		t.Fatalf("cursor = %d, want last row", s.Cursor())
	}
}

func TestSettingsNarrowStacksSingleColumn(t *testing.T) {
	t.Parallel()

	s := newSettingsFixture(t, settingsFixtureState(nil), 80, 40)
	lines := strings.Split(s.View().Content, "\n")

	seen := map[string]int{}
	for i, line := range lines {
		for _, row := range s.state.Rows {
			if strings.HasPrefix(strings.TrimLeft(line, " "), row.Label) {
				seen[row.Key] = i
			}
		}
	}
	prev := -1
	for _, row := range s.state.Rows {
		i, ok := seen[row.Key]
		if !ok {
			t.Fatalf("stacked body lost row %s", row.Key)
		}
		if i <= prev {
			t.Fatalf("row %s out of §L order", row.Key)
		}
		prev = i
	}
}

func TestSettingsHints(t *testing.T) {
	t.Parallel()

	s := NewSettings(nil)
	hints := s.Hints()
	if len(hints) == 0 {
		t.Fatal("hints empty")
	}
	for _, h := range hints {
		if h.Key == "w" && !h.Primary {
			t.Error("w must be primary")
		}
	}
}
