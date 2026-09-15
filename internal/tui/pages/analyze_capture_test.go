// analyze_capture_test.go covers the §J capture-step units (SCR-510):
// the candidate list's cursor/filter/typed-path behavior, the empty
// state and inline error lines, and the two-mode [f] browse gate (UAT
// round 8 finding 2 / D3, Task 5.2: navigate mode opens the root-side
// picker, edit mode types `f` literally into the draft). The shared
// fixture helpers live in analyze_test.go; the wizard state machine and
// async legs are root-side (root_analyze_test.go).
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

// TestAnalyzeCursorSurvivesResync UAT round 6: root's syncPages pushes
// SetState after every Update, and re-seeding the cursor onto the
// "current" row on every push snapped arrow moves back — the capture and
// spec arrows looked dead. The seed belongs to step ENTRY only.
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

// TestAnalyzeCaptureBrowseTracksEditMode pins the capture step's two-mode
// browse gate (UAT round 8 finding 2 / D3, Task 5.2): NAVIGATE mode sends
// `f` to the root-side picker as AnalyzeBrowseMsg; typing enters EDIT mode
// (the first printable types itself) and there `f` types literally into
// the draft — the picker must not reopen. The gate is the Editing() mode
// (the same predicate ClaimsKeyboard delegates to), mirroring the §G
// server-form pin rather than an ad-hoc "empty draft" check.
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
	if _, ok := cmdMsg(t, cmd).(AnalyzeBrowseMsg); !ok {
		t.Fatalf("navigate-mode f -> %T, want AnalyzeBrowseMsg", cmdMsg(t, cmd))
	}

	// Typing enters edit mode (and types itself, SCR-502 typeahead).
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
