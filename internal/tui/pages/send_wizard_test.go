// send_wizard_test.go pins the wizard's input routing:
// step shapes (four offline, three online), the embedded connect form's
// Enter/Esc messages, list filtering with the path override, step back
// semantics and the template pick. Rendering is pinned separately by the
// goldens.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// wizardState is the §B wizard fixture. The masked PAN is derived through the
// theme (the same call root makes), not typed as "411111…1111": a typed ellipsis
// is a non-ASCII byte in a fixture that only survived in ASCII goldens because the
// page ran it through a ReplaceAll afterwards.
func wizardState(th *theme.Theme) WizardState {
	return WizardState{
		Steps: []string{WizardStepConnect, WizardStepSpec, WizardStepFile, WizardStepSend},
		SpecItems: []WizardItem{
			{Label: "flex.json", Path: "/specs/flex.json", Current: true},
			{Label: "mastercard.json", Path: "/specs/mastercard.json"},
		},
		FileItems: []WizardItem{
			{Label: "transaction.json", Path: "/tx/transaction.json", Hint: "recents", Current: true},
			{Label: "purchase.json", Path: "/tx/purchase.json"},
		},
		Templates: []WizardTemplate{
			{Name: "Purchase", MTI: "0200", PAN: th.ElideMiddle("4111111111111111", 6, 4), Amount: "100.00"},
			{Name: "Refund", MTI: "0200", PAN: th.ElideMiddle("4111111111111111", 6, 4), Amount: "10.00"},
			{Name: "Sign On", Description: "Network Management: Sign On"},
		},
		Target:   "127.0.0.1:9999",
		TargetOK: false,
	}
}

func newWizard(t *testing.T, st WizardState, withForm bool) *SendWizard {
	t.Helper()

	w := NewSendWizard(asciiTheme(t))
	if withForm {
		w.SetConnectForm(connectTestState())
	}
	w.SetState(st)
	_, _ = w.Update(windowSize(120, 32))

	return w
}

func wizardMsg(t *testing.T, w *SendWizard, msg tea.Msg) tea.Msg {
	t.Helper()

	_, cmd := w.Update(msg)
	if cmd == nil {
		return nil
	}

	return cmd()
}

func TestWizardOfflineHasConnectStepAndEmitsAttempt(t *testing.T) {
	t.Parallel()

	w := newWizard(t, wizardState(asciiTheme(t)), true)
	if got := w.currentStep(); got != WizardStepConnect {
		t.Fatalf("offline first step %q, want connect", got)
	}
	view := w.View()
	for _, want := range []string{"SEND", "1 connect", "2 spec", "3 file", "4 send", "Mode", "[Enter] connect", "[Esc] cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("offline wizard lacks %q:\n%s", want, view)
		}
	}

	if _, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardConnectAttemptMsg); !ok {
		t.Fatal("Enter on the connect step must emit WizardConnectAttemptMsg")
	}
	if _, ok := wizardMsg(t, w, special(tea.KeyEsc)).(WizardCancelMsg); !ok {
		t.Fatal("Esc on the connect step must emit WizardCancelMsg")
	}
}

func TestWizardOnlineSkipsConnectStepAndLists(t *testing.T) {
	t.Parallel()

	st := wizardState(asciiTheme(t))
	st.Steps = []string{WizardStepSpec, WizardStepFile, WizardStepSend}
	w := newWizard(t, st, false)

	view := w.View()
	// ascii theme (the wizard's golden profile): the current tag is "<- current".
	for _, want := range []string{"1 spec", "flex.json", "<- current", "filter:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("spec step lacks %q:\n%s", want, view)
		}
	}

	_, _ = w.Update(special(tea.KeyDown))
	if got, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardChooseSpecMsg); !ok || got.Path != "/specs/mastercard.json" {
		t.Fatalf("Enter on row 2: %#v, want mastercard.json", got)
	}
}

func TestWizardFilterNarrowsAndPathOverrides(t *testing.T) {
	t.Parallel()

	st := wizardState(asciiTheme(t))
	st.Steps = []string{WizardStepFile}
	w := newWizard(t, st, false)

	for _, r := range "pur" {
		_, _ = w.Update(pressKey(r, string(r)))
	}
	view := w.View()
	if !strings.Contains(view, "purchase.json") || strings.Contains(view, "transaction.json") {
		t.Fatalf("filter 'pur' did not narrow:\n%s", view)
	}
	// A path-shaped filter is offered as the value on Enter.
	_, _ = w.Update(special(tea.KeyEsc)) // esc clears the filter first
	for _, r := range "/tmp/x.json" {
		_, _ = w.Update(pressKey(r, string(r)))
	}
	got, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardChooseFileMsg)
	if !ok || got.Path != "/tmp/x.json" {
		t.Fatalf("path override: %#v %v", got, ok)
	}
}

func TestWizardBrowseKeyOnlyOnEmptyFilter(t *testing.T) {
	t.Parallel()

	st := wizardState(asciiTheme(t))
	st.Steps = []string{WizardStepSpec, WizardStepFile, WizardStepSend}
	w := newWizard(t, st, false)

	if got, ok := wizardMsg(t, w, pressKey('f', "f")).(WizardBrowseMsg); !ok || !got.IsSpec {
		t.Fatalf("f on spec step: %#v %v, want WizardBrowseMsg{IsSpec:true}", got, ok)
	}
	w.AdvanceStep() // → file step
	if got, ok := wizardMsg(t, w, pressKey('f', "f")).(WizardBrowseMsg); !ok || got.IsSpec {
		t.Fatalf("f on file step: %#v %v, want IsSpec:false", got, ok)
	}
	// Once a filter is active, f types into it (no second browse).
	w.filterList(pressKey('x', "x"))
	if got := wizardMsg(t, w, pressKey('f', "f")); got != nil {
		t.Fatalf("f while filtering must not browse, got %#v", got)
	}
	if !strings.Contains(w.View(), "xf") {
		t.Fatal("f must land in the filter")
	}
}

// Two-mode browse on the spec/file steps: navigate-mode f opens the picker;
// typing enters edit mode where f types literally into the filter.
func TestWizardFileStepBrowseTracksEditMode(t *testing.T) {
	t.Parallel()

	st := wizardState(asciiTheme(t))
	st.Steps = []string{WizardStepSpec, WizardStepFile, WizardStepSend}
	w := newWizard(t, st, false)

	// Navigate mode on the spec step: f is the picker key.
	if w.Editing() {
		t.Fatal("a fresh spec step must be navigate mode")
	}
	if got, ok := wizardMsg(t, w, ch('f')).(WizardBrowseMsg); !ok || !got.IsSpec {
		t.Fatalf("spec-step f in navigate mode: %#v %v, want WizardBrowseMsg{IsSpec:true}", got, ok)
	}

	// The file step opens the same way (f means the same thing on every step).
	w.AdvanceStep() // → file step
	if w.Editing() {
		t.Fatal("the file step must open in navigate mode")
	}
	if got, ok := wizardMsg(t, w, ch('f')).(WizardBrowseMsg); !ok || got.IsSpec {
		t.Fatalf("file-step f in navigate mode: %#v %v, want IsSpec:false", got, ok)
	}

	// Typing enters edit mode (and types itself).
	_, _ = w.Update(ch('x'))
	if !w.Editing() {
		t.Fatal("typing must enter edit mode")
	}

	// Edit mode: f types literally into the filter; no second browse.
	if got := wizardMsg(t, w, ch('f')); got != nil {
		t.Fatalf("f while editing must not browse, got %#v", got)
	}
	if !strings.Contains(w.View(), "xf") {
		t.Fatalf("f must land in the filter:\n%s", w.View())
	}

	// Esc clears the draft back to navigate mode, where f browses again.
	_, _ = w.Update(special(tea.KeyEsc))
	if w.Editing() {
		t.Fatal("esc must leave edit mode")
	}
	if got, ok := wizardMsg(t, w, ch('f')).(WizardBrowseMsg); !ok || got.IsSpec {
		t.Fatalf("f after leaving edit mode: %#v %v, want WizardBrowseMsg{IsSpec:false}", got, ok)
	}
}

func TestWizardBackStepsThenCancels(t *testing.T) {
	t.Parallel()

	st := wizardState(asciiTheme(t))
	st.Steps = []string{WizardStepSpec, WizardStepFile, WizardStepSend}
	w := newWizard(t, st, false)

	w.AdvanceStep() // → file
	if _, ok := wizardMsg(t, w, special(tea.KeyEsc)).(WizardCancelMsg); ok {
		if w.Step() == 0 {
			t.Fatal("Esc from step 1 must land on step 0, not cancel")
		}
	}
	if w.Step() != 0 {
		t.Fatalf("after Esc step %d, want 0", w.Step())
	}
	if _, ok := wizardMsg(t, w, special(tea.KeyEsc)).(WizardCancelMsg); !ok {
		t.Fatal("Esc on step 0 must emit WizardCancelMsg")
	}
}

func TestWizardTemplateStepPicksAndShowsTarget(t *testing.T) {
	t.Parallel()

	st := wizardState(asciiTheme(t))
	st.Steps = []string{WizardStepSend}
	st.TargetOK = true
	w := newWizard(t, st, false)

	view := w.View()

	// The masked PAN is asked of the theme rather than spelled here: what this
	// assert is about is that the column shows a masked PAN, not which ellipsis
	// the profile uses. The literal it replaced ("411111...1111") was the
	// ReplaceAll's spelling, a marker no glyph policy owns.
	mask := asciiTheme(t).ElideMiddle("4111111111111111", 6, 4)
	for _, want := range []string{"Purchase", "0200", mask, "100.00", "target 127.0.0.1:9999", "connected", "Sign On"} {
		if !strings.Contains(view, want) {
			t.Fatalf("send step lacks %q:\n%s", want, view)
		}
	}
	_, _ = w.Update(special(tea.KeyDown))
	got, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardSendMsg)
	if !ok || got.Name != "Refund" {
		t.Fatalf("Enter on row 2: %#v %v, want Refund", got, ok)
	}
}

func TestWizardOnConnectedAdvancesAndStamps(t *testing.T) {
	t.Parallel()

	w := newWizard(t, wizardState(asciiTheme(t)), true)
	st := w.dlg.State()
	st.InFlight = true
	w.dlg.SetState(st)

	w.OnConnected("127.0.0.1:9999")
	if w.Step() != 1 {
		t.Fatalf("after connect step %d, want 1 (spec)", w.Step())
	}
	if !w.State().TargetOK || w.State().Target != "127.0.0.1:9999" {
		t.Fatalf("target not stamped: %+v", w.State())
	}
}

// The pre-selection homes the send step's cursor onto the chosen
// transaction: Enter with no navigation commits it, arrows still move off
// it (a cursor start, never a commit), and every earlier step keeps its
// own Enter leg.
func TestWizardPresetHomesSendStepCursor(t *testing.T) {
	t.Parallel()

	w := newWizard(t, wizardState(asciiTheme(t)), true) // the offline four-step shape
	w.SetPreset("Refund")

	if _, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardConnectAttemptMsg); !ok {
		t.Fatal("the connect step keeps its own Enter after a pre-selection")
	}
	w.AdvanceStep() // spec
	w.AdvanceStep() // file
	w.AdvanceStep() // send
	if got, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardSendMsg); !ok || got.Name != "Refund" {
		t.Fatalf("send step cursor: %#v, want WizardSendMsg{Refund}", got)
	}

	_, _ = w.Update(special(tea.KeyDown))
	if got, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardSendMsg); !ok || got.Name != "Sign On" {
		t.Fatalf("navigation off the pre-selection: %#v, want Sign On", got)
	}
}

// The root advances and then pushes the re-derived template list (a tx
// file pick loads fresh templates); the arrival re-homes the preset
// against the list that is live now, not the one at advance time.
func TestWizardPresetRehomesAfterFreshTemplateList(t *testing.T) {
	t.Parallel()

	st := wizardState(asciiTheme(t))
	st.Steps = []string{WizardStepFile, WizardStepSend}
	w := newWizard(t, st, false)
	w.SetPreset("Refund")

	w.AdvanceStep() // → send, homed on the previous listing
	fresh := st
	fresh.Templates = []WizardTemplate{
		{Name: "Sign On"}, {Name: "Refund", MTI: "0200"}, {Name: "Purchase", MTI: "0200"},
	}
	w.SetState(fresh)
	w.HomeCursor()

	if got, ok := wizardMsg(t, w, special(tea.KeyEnter)).(WizardSendMsg); !ok || got.Name != "Refund" {
		t.Fatalf("cursor after the fresh listing: %#v, want Refund", got)
	}
}
