// analyze_capture_test.go covers the §J capture-step units: candidate
// list cursor/filter/typed-path, empty state and inline errors, and the
// two-mode [f] browse gate. Shared fixture helpers: analyze_test.go.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestAnalyzeCaptureListCursorAndFilter(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	a := analyzePage(t, st, 120, 32)

	// The cursor places on the current pick, and the list shows labels.
	if a.ListCursor() != 1 {
		t.Errorf("cursor = %d, want the current item at 1", a.ListCursor())
	}
	body := ansi.Strip(a.View().Content)
	if !strings.Contains(body, "capture.pcap") || !strings.Contains(body, "night.pcap") {
		t.Errorf("candidate list missing labels:\n%s", body)
	}

	// Typing filters (j/k become text once typing).
	_, _ = a.Update(ch('n'))
	draft, editing := a.Draft()
	if !editing || draft != "n" {
		t.Fatalf("draft = %q/%v, want n editing", draft, editing)
	}
	body = ansi.Strip(a.View().Content)
	if !strings.Contains(body, "night.pcap") || strings.Contains(body, "capture.pcap") {
		t.Errorf("filter n must keep only night:\n%s", body)
	}

	// Enter commits the filtered candidate under the cursor.
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeCommitCaptureMsg)
	if !ok || msg.Value != "/captures/night.pcap" {
		t.Errorf("Enter -> %T %+v, want CommitCapture /captures/night.pcap", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}
}

// A SetState re-seed must not snap the cursor back onto the "current" row.
func TestAnalyzeCaptureTypedPathBeatsList(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	a := analyzePage(t, st, 120, 32)

	for _, r := range "/tmp/x.pcap" {
		_, _ = a.Update(ch(r))
	}
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeCommitCaptureMsg)
	if !ok || msg.Value != "/tmp/x.pcap" {
		t.Errorf("typed path Enter -> %T %+v, want CommitCapture /tmp/x.pcap", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}
}

func TestAnalyzeCaptureEmptyStateAndBrowse(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	st.CaptureItems = nil
	st.CapturePath = ""
	a := analyzePage(t, st, 120, 32)

	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "no .pcap") {
		t.Errorf("empty-state line missing:\n%s", body)
	}

	// [f] opens the shared picker (root-side).
	_, cmd := a.Update(ch('f'))
	if _, ok := cmdMsg(t, cmd).(AnalyzeBrowseMsg); !ok {
		t.Errorf("f -> %T, want AnalyzeBrowseMsg", cmdMsg(t, cmd))
	}

	// Enter with an empty list and empty draft still commits "":
	// root intercepts it to the file picker.
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(AnalyzeCommitCaptureMsg)
	if !ok || msg.Value != "" {
		t.Errorf("empty Enter -> %T %+v, want CommitCapture \"\"", cmdMsg(t, cmd), cmdMsg(t, cmd))
	}
}

func TestAnalyzeCaptureInlineError(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	st.CaptureError = "no such file: /tmp/gone.pcap"
	a := analyzePage(t, st, 120, 32)
	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "no such file") {
		t.Errorf("inline field error missing:\n%s", body)
	}
}

// Two-mode browse gate: navigate-mode f opens the root-side picker;
// typing enters edit mode where f types literally into the draft.
func TestAnalyzeCaptureBrowseTracksEditMode(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepCapture
	a := analyzePage(t, st, 120, 32)

	// Navigate mode: f is the picker key.
	if a.Editing() {
		t.Fatal("a fresh capture step must be navigate mode")
	}
	_, cmd := a.Update(ch('f'))
	msg, ok := cmdMsg(t, cmd).(AnalyzeBrowseMsg)
	if !ok {
		t.Fatalf("navigate-mode f -> %T, want AnalyzeBrowseMsg", cmdMsg(t, cmd))
	}
	if msg.IsSpec {
		t.Fatal("capture-step browse must carry IsSpec: false")
	}

	// Typing enters edit mode (and types itself).
	_, _ = a.Update(ch('n'))
	if !a.Editing() {
		t.Fatal("typing must enter edit mode")
	}

	// Edit mode: f types literally into the draft; no second browse.
	_, cmd = a.Update(ch('f'))
	if got := cmdMsg(t, cmd); got != nil {
		t.Fatalf("f while editing must not browse, got %#v", got)
	}
	if d, _ := a.Draft(); d != "nf" {
		t.Fatalf("draft = %q, want the typed suffix \"nf\"", d)
	}

	// Esc clears the draft back to navigate mode, where f browses again.
	_, _ = a.Update(special(tea.KeyEscape))
	if a.Editing() {
		t.Fatal("esc must leave edit mode")
	}
	_, cmd = a.Update(ch('f'))
	if _, ok := cmdMsg(t, cmd).(AnalyzeBrowseMsg); !ok {
		t.Fatalf("f after leaving edit mode -> %T, want AnalyzeBrowseMsg", cmdMsg(t, cmd))
	}
}
