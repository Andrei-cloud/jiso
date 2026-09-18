// error_modal_zorder_test.go pins the screen's z-order over the other
// root overlays: it owns the keyboard over an open §E dialog and a
// pending §N3 confirm, and draws as the topmost overlay over both.
package tui

import (
	"errors"
	"testing"

	"github.com/charmbracelet/colorprofile"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/widgets"
)

// double-open pin: the screen is the topmost overlay, so it owns the
// keyboard over the others too — with the §E dialog underneath, esc closes
// only the screen; the dialog stays open and receives keys again after.
func TestErrorModalOwnsKeysOverOpenDialog(t *testing.T) {
	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, _ = m.Update(ch('c'))
	if m.dlg == nil {
		t.Fatal("fixture: the connect dialog must be open")
	}
	m.openErrorModal("cannot load transaction file", errors.New("bad spec"))

	for _, d := range []rune{'1', '4'} {
		_, _ = m.Update(ch(d))
		if got := m.Current().ID(); got != "dashboard" {
			t.Fatalf("%c jumped pages under the modal: now %q", d, got)
		}
	}

	_, _ = m.Update(special(tea.KeyEsc))
	if m.errModal != nil {
		t.Fatal("esc must close the screen")
	}
	if m.dlg == nil {
		t.Fatal("esc must close only the screen: the dialog underneath stays open")
	}

	_, _ = m.Update(special(tea.KeyEsc)) // keys reach the dialog again
	if m.dlg != nil {
		t.Fatal("after the close the dialog must receive esc")
	}
}

// enter over an open dialog closes the screen without starting the
// connect attempt the dialog would run.
func TestErrorModalEnterOverDialogClosesOnlyModal(t *testing.T) {
	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, _ = m.Update(ch('c'))
	m.openErrorModal("cannot connect", errors.New("dial refused"))

	_, _ = m.Update(special(tea.KeyEnter))
	if m.errModal != nil {
		t.Fatal("enter must close the screen")
	}
	if m.dlg == nil {
		t.Fatal("enter closed through the dialog underneath")
	}
	if m.connectRun != nil {
		t.Fatal("enter must not start the connect attempt under the screen")
	}
}

// confirmModalRoot co-opens a pending §N3 confirm and the error screen
// on a fresh 80×24 root (the same harness as the dialog double-open pins).
func confirmModalRoot(t *testing.T, body string) *RootModel {
	t.Helper()

	m := NewRootModel(nil)
	m.theme = helpGoldenTheme(colorprofile.ASCII)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.workersConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "stop all workers?")
	m.openErrorModal("cannot load transaction file", errors.New(body))
	if m.errModal == nil || m.workersConfirm == nil || !m.workersConfirm.Pending() {
		t.Fatal("fixture: the screen and the confirm must both be open")
	}

	return m
}

// drawn z-order mirrors keyboard z-order: the screen is the topmost
// overlay (View draws it last), so with a pending confirm underneath its
// box overwrites the question line; toasts still compose above the screen.
func TestErrorModalDrawsOverPendingConfirm(t *testing.T) {
	m := confirmModalRoot(t, errModalFixture(25))

	frame := m.View().Content
	mustShow(t, frame, "cannot load transaction file", "line 01", "line 10", "enter ok")
	mustHide(t, frame, "stop all workers?")
}

// esc over the co-opened pair closes the screen only: the confirm
// survives and receives keys again after.
func TestErrorModalEscOverConfirmClosesOnlyModal(t *testing.T) {
	m := confirmModalRoot(t, "connection refused")

	_, _ = m.Update(special(tea.KeyEsc))
	if m.errModal != nil {
		t.Fatal("esc must close the screen")
	}
	if m.workersConfirm == nil || !m.workersConfirm.Pending() {
		t.Fatal("esc must close only the screen: the confirm underneath stays pending")
	}

	_, _ = m.Update(special(tea.KeyEsc)) // keys reach the confirm again
	if m.workersConfirm.Pending() {
		t.Fatal("after the close the confirm must receive esc")
	}
}

// enter over the co-opened pair closes the screen without deciding the
// pending confirm underneath (default No must not fire).
func TestErrorModalEnterOverConfirmClosesOnlyModal(t *testing.T) {
	m := confirmModalRoot(t, "connection refused")

	_, _ = m.Update(special(tea.KeyEnter))
	if m.errModal != nil {
		t.Fatal("enter must close the screen")
	}
	if m.workersConfirm == nil || !m.workersConfirm.Pending() {
		t.Fatal("enter must close the screen, not decide the confirm")
	}
}
