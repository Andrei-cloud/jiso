// analyze_unparsable_test.go pins the unparsable-message
// reviewer: the run step offers [u] review when samples exist, the
// viewer opens on demand (never auto), walks samples with j/k, closes
// with Esc, ignores [u] with no samples, and re-seats its cursor when a
// fresh enumeration bumps UnparsableID.
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// unparsableRowsFixture is a two-sample reviewer roster.
func unparsableRowsFixture() []AnalyzeUnparsableRow {
	return []AnalyzeUnparsableRow{
		{
			Offset: "56", Length: "44", Reason: "field 2: unexpected EOF",
			Head:     []byte("0200\xc0\xa8\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00AB"),
			FailedAt: 4,
			Fields:   []UnparsableField{{ID: "0", Name: "Message Type Indicator", Value: "0200"}},
		},
		{
			Offset: "120", Length: "60", Reason: "invalid BCDDigits length",
			Head:     []byte("\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
			FailedAt: 2,
		},
	}
}

func analyzeDoneWithUnparsable() AnalyzeState {
	st := analyzeFixtureState()
	st.Unparsable = 2
	st.UnparsableRows = unparsableRowsFixture()
	st.UnparsableID = 1

	return st
}

func TestAnalyzeUnparsableAffordance(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeDoneWithUnparsable(), 120, 32)
	body := ansi.Strip(a.View().Content)
	if !strings.Contains(body, "[u] review") {
		t.Errorf("flows line must offer [u] review:\n%s", body)
	}
	if a.ClaimsKeyboard() {
		t.Fatal("the viewer must NOT auto-open (unlike the item picker)")
	}
}

func TestAnalyzeUnparsableViewerOpensAndWalks(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeDoneWithUnparsable(), 170, 32)
	_, _ = a.Update(ch('u'))
	if !a.ClaimsKeyboard() {
		t.Fatal("[u] must open the viewer and claim the keyboard")
	}
	body := ansi.Strip(a.View().Content)
	for _, want := range []string{"UNPARSABLE MESSAGES", "field 2: unexpected EOF", "0200"} {
		if !strings.Contains(body, want) {
			t.Errorf("viewer body lacks %q:\n%s", want, body)
		}
	}

	// j walks to the second sample: its reason shows in the pane.
	_, _ = a.Update(ch('j'))
	body = ansi.Strip(a.View().Content)
	if !strings.Contains(body, "invalid BCDDigits length") {
		t.Errorf("after j the pane must show the second sample:\n%s", body)
	}

	// Esc closes without emitting.
	_, cmd := a.Update(special(tea.KeyEscape))
	if a.ClaimsKeyboard() {
		t.Fatal("esc must close the viewer")
	}
	if cmd != nil {
		t.Fatalf("closing the viewer must not emit, got %v", cmd)
	}
}

func TestAnalyzeUnparsableNoRowsKeyIgnored(t *testing.T) {
	t.Parallel()

	// The default fixture has Unparsable: 3 but no collected samples.
	a := analyzePage(t, analyzeFixtureState(), 120, 32)
	_, _ = a.Update(ch('u'))
	if a.ClaimsKeyboard() {
		t.Fatal("[u] with no collected samples must not open the viewer")
	}
}

func TestAnalyzeUnparsableCursorReseatsOnNewID(t *testing.T) {
	t.Parallel()

	st := analyzeDoneWithUnparsable()
	a := analyzePage(t, st, 120, 32)
	_, _ = a.Update(ch('u')) // open
	_, _ = a.Update(ch('j')) // walk to the second sample
	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "invalid BCDDigits length") {
		t.Fatalf("precondition: second sample shown:\n%s", body)
	}

	st.UnparsableID = 2 // a fresh enumeration
	a.SetState(st)
	// The viewer stays open but its cursor re-seats onto the first row.
	if !a.ClaimsKeyboard() {
		t.Fatal("an open viewer stays open across a new enumeration")
	}
	if body := ansi.Strip(a.View().Content); !strings.Contains(body, "SAMPLE AT 56") {
		t.Errorf("a new enumeration must re-seat the cursor on the first sample:\n%s", body)
	}
}

// TestAnalyzeUnparsableDescribesParsedFields: the sample pane
// describes the fields that unpacked before the failure and names the byte
// where the unparsed region begins.
func TestAnalyzeUnparsableDescribesParsedFields(t *testing.T) {
	t.Parallel()

	a := analyzePage(t, analyzeDoneWithUnparsable(), 170, 40)
	_, _ = a.Update(ch('u'))
	body := ansi.Strip(a.View().Content)
	for _, want := range []string{
		"PARSED BEFORE FAILURE", "Message Type Indicator", "0200",
		"HEXDUMP", "unparsed from byte 4",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sample pane lacks %q:\n%s", want, body)
		}
	}

	// The second sample parsed no field: the pane says so rather than
	// showing an empty panel.
	_, _ = a.Update(ch('j'))
	body = ansi.Strip(a.View().Content)
	if !strings.Contains(body, "no field unpacked before the failure") {
		t.Errorf("a sample with no parsed fields must say so:\n%s", body)
	}
}

// TestUnparsableHexLinesMarksUnparsed pins the red region: bytes before the
// stop offset render unstyled, and the first styled byte is exactly the
// first unparsed one (TrueColor so the error colour emits SGR).
func TestUnparsableHexLinesMarksUnparsed(t *testing.T) {
	t.Parallel()

	a := NewAnalyze(testTheme(t, colorprofile.TrueColor))
	row := AnalyzeUnparsableRow{Offset: "56", Length: "44", FailedAt: 4, Head: []byte("0200ABCD")}

	lines := a.unparsableHexLines(row, 120)
	if len(lines) != 1 {
		t.Fatalf("8 bytes = %d line, want 1", len(lines))
	}
	line := lines[0]

	if plain := ansi.Strip(line); !strings.HasPrefix(plain, "00000038  30 32 30 30 41 42 43 44 ") ||
		!strings.HasSuffix(plain, "|0200ABCD|") {
		t.Errorf("hexdump grid wrong: %q", plain)
	}

	// Address (10 cells) + four unstyled bytes ("30 32 30 30 ", 12 cells)
	// precede the first marked byte, so the first SGR must land at cell 22.
	const firstUnparsed = 10 + 4*3
	if i := strings.Index(line, "\x1b["); i != firstUnparsed {
		t.Errorf("first marked byte must start at cell %d, got %d:\n%q", firstUnparsed, i, line)
	}
}

// TestUnparsableHexLinesUnstyledWhenNoStop a sample with no reliable stop
// offset renders the whole head unstyled (nothing is marked).
func TestUnparsableHexLinesUnstyledWhenNoStop(t *testing.T) {
	t.Parallel()

	a := NewAnalyze(testTheme(t, colorprofile.TrueColor))
	row := AnalyzeUnparsableRow{Offset: "0", Length: "4", FailedAt: -1, Head: []byte("0200")}

	lines := a.unparsableHexLines(row, 120)
	if len(lines) != 1 {
		t.Fatalf("4 bytes = %d line, want 1", len(lines))
	}
	if strings.Contains(lines[0], "\x1b[") {
		t.Errorf("an unknown stop offset must mark nothing: %q", lines[0])
	}
}
