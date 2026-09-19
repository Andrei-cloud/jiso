// footer_dedup_test.go pins the one-hotkey-surface rule at the footerHints
// choke point: while a keyboard-owning overlay lists its keys in-body (the
// form dialogs, the wizards, the picker) or documents every key in its box
// (§M help), the strip keeps only the global legend; a pending §N3 confirm
// badges its decision keys INTO the strip and drops its old dim body line;
// closing any of them restores the page's context hints.
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/widgets"
)

// footerRootT is a fresh ASCII root settled on §B (the transactions page:
// distinctive context keys to watch).
func footerRootT(t *testing.T) *RootModel {
	t.Helper()

	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, _ = m.Update(ch('2'))

	return m
}

// pumpMsgs feeds msgs through root Update, replaying each returned
// command's message, until the queue drains (bounded).
func pumpMsgs(t *testing.T, m *RootModel, msgs ...tea.Msg) {
	t.Helper()

	queue := append([]tea.Msg(nil), msgs...)
	for i := 0; i < 16 && len(queue) > 0; i++ {
		_, cmd := m.Update(queue[0])
		queue = append(queue[1:], flattenMsgs(cmd)...)
	}
}

// assertLegend pins every global entry (page jumps, trio) in the list.
func assertLegend(t *testing.T, hints []frameKeyHintLite) {
	t.Helper()

	for _, key := range []string{"1", "2", "3", "4", "5", "6", "7", "8", ":", "?", "q"} {
		found := false
		for _, h := range hints {
			if h.key == key {
				found = true
			}
		}
		if !found {
			t.Errorf("global legend entry %q dropped from the footer: %+v", key, hints)
		}
	}
}

// lite mirrors frame.KeyHint without the import ceremony the assertions
// repeat per case.
type frameKeyHintLite struct {
	key     string
	desc    string
	primary bool
}

func lite(hints []frame.KeyHint) []frameKeyHintLite {
	out := make([]frameKeyHintLite, 0, len(hints))
	for _, h := range hints {
		out = append(out, frameKeyHintLite{h.Key, h.Desc, h.Primary})
	}

	return out
}

// TestPageModalsSuppressPageHintsInFooter: for every root overlay that
// already draws its own (badged) hint line — the connect dialog, the send
// wizard, the server form, the worker wizard, the file picker, the §M box —
// the footer keeps the legend and nothing of the page; esc restores the
// page hints.
func TestPageModalsSuppressPageHintsInFooter(t *testing.T) {
	cases := []struct {
		name   string
		open   func(t *testing.T, m *RootModel)
		isOpen func(m *RootModel) bool
	}{
		{"dlg", func(t *testing.T, m *RootModel) { _, _ = m.openConnect() }, func(m *RootModel) bool { return m.dlg != nil }},
		{"wizard", func(t *testing.T, m *RootModel) { _, _ = m.openWizard() }, func(m *RootModel) bool { return m.wizard != nil }},
		{"serverDlg", func(t *testing.T, m *RootModel) { _, _ = m.openServerForm() }, func(m *RootModel) bool { return m.serverDlg != nil }},
		{"workerWiz", func(t *testing.T, m *RootModel) { _, _ = m.openWorkerWizard("bgsend") }, func(m *RootModel) bool { return m.workerWiz != nil }},
		{"filePick", func(t *testing.T, m *RootModel) {
			_, _ = m.openFilePicker(OpenFilePickerMsg{Root: t.TempDir(), RootLabel: "fixture/"})
		}, func(m *RootModel) bool { return m.filePick != nil }},
		{"help", func(t *testing.T, m *RootModel) { m.openHelp() }, func(m *RootModel) bool { return m.help != nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := footerRootT(t)
			pageHints := m.Current().Hints()
			if len(pageHints) == 0 {
				t.Fatal("fixture: §B must carry context hints")
			}

			tc.open(t, m)
			if !tc.isOpen(m) {
				t.Fatal("fixture: the overlay did not open")
			}

			hints := lite(m.footerHints())
			assertLegend(t, hints)
			for _, ph := range lite(pageHints) {
				for _, h := range hints {
					if h == ph {
						t.Errorf("page hint %+v leaked while the %s is open", ph, tc.name)
					}
				}
			}

			pumpMsgs(t, m, special(tea.KeyEscape))
			if tc.isOpen(m) {
				t.Fatalf("esc must close the %s (or the harness must be extended to its real close)", tc.name)
			}
			if got, want := len(m.footerHints()), len(globalFooterHints(&m.keys))+len(m.Current().Hints()); got != want {
				t.Fatalf("after closing the %s the footer must be legend + page hints again, got %d entries", tc.name, got)
			}
		})
	}
}

// TestConfirmPendingFooterStaysGlobal: a module window owns its hotkeys
// (UAT finding). The confirm box renders its decision keys IN-BODY; the
// strip keeps only the global legend — never the box's keys, never the
// frozen page's; cancel restores the page hints.
func TestConfirmPendingFooterStaysGlobal(t *testing.T) {
	m := footerRootT(t)
	pageHints := m.Current().Hints()
	m.workersConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "stop all workers?")

	hints := lite(m.footerHints())
	assertLegend(t, hints)
	for _, no := range []frameKeyHintLite{
		{key: "y", desc: "confirm", primary: true},
		{key: "n", desc: "cancel", primary: true},
		{key: "esc", desc: "cancel", primary: true},
	} {
		for _, h := range hints {
			if h == no {
				t.Errorf("footer smuggles the confirm entry %+v: the box must own its keys", no)
			}
		}
	}
	for _, ph := range lite(pageHints) {
		for _, h := range hints {
			if h == ph {
				t.Errorf("page hint %+v leaked while a confirm is pending", ph)
			}
		}
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"y confirm", "n cancel", "esc cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("the confirm box must carry %q in-body:\n%s", want, view)
		}
	}
	if strings.Contains(view, "default: no") {
		t.Error("the confirm box must not repeat the old dim hint line")
	}

	pumpMsgs(t, m, special(tea.KeyEscape))
	if m.workersConfirm != nil {
		t.Fatal("esc must cancel the pending confirm")
	}
	if got, want := len(m.footerHints()), len(globalFooterHints(&m.keys))+len(m.Current().Hints()); got != want {
		t.Fatalf("after the decision the footer must return to legend + page hints, got %d entries", got)
	}
}

// TestConfirmBoxKeysAreBadged: the dialog's decision line renders y/n/esc
// in the Theme.Key badge (the §14 complaint: unhighlighted hotkeys) —
// badged inside the box, where the keys are actually acted on.
func TestConfirmBoxKeysAreBadged(t *testing.T) {
	m := footerRootT(t)
	m.theme = helpGoldenTheme(colorprofile.TrueColor)
	m.workersConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "stop all workers?")

	const boldOpen = "\x1b[1;38;2;68;147;248m"
	const reset = "\x1b[m"

	frame := m.View().Content
	for _, k := range []string{"y", "n", "esc"} {
		if want := boldOpen + k + reset; !strings.Contains(frame, want) {
			t.Errorf("the box lacks the bold-accent badge for %q (%q)", k, want)
		}
	}
	for _, want := range []string{"y confirm", "n cancel", "esc cancel"} {
		if !strings.Contains(ansi.Strip(frame), want) {
			t.Errorf("the box must read %q:\n%s", want, ansi.Strip(frame))
		}
	}
}
