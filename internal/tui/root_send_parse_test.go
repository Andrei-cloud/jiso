package tui

import (
	"encoding/hex"
	"strings"
	"testing"

	"jiso/internal/tui/pages"
)

// TestRCBadgeMapping: no RC label source exists in internal/app or
// internal/command (the CLI send describes codes verbatim), so §D maps
// the two wireframe codes and renders every other code code-only.
func TestRCBadgeMapping(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		rc    string
		label string
		ok    bool
	}{
		{"00", "APPROVED", true},
		{"96", "DECLINED", false},
		{"05", "", false},
		{"N7", "", false},
		{"", "", false},
	} {
		label, ok := rcBadge(tc.rc)
		if label != tc.label || ok != tc.ok {
			t.Errorf("rcBadge(%q) = %q/%v, want %q/%v", tc.rc, label, ok, tc.label, tc.ok)
		}
	}
}

// noteOn indexes rows[Num].(Note, NoteKind).
func noteOn(t *testing.T, rows []pages.ExchangeRow, num string) (string, pages.NoteKind) {
	t.Helper()

	for _, r := range rows {
		if r.Num == num {
			return r.Note, r.NoteKind
		}
	}
	t.Fatalf("no row %s in %+v", num, rows)

	return "", 0
}

// TestParseExchangeCorrelationNotes: echoed fields get "echo ✓", the STAN
// gets "STAN ✓" through the shared normalization (041822 vs 41822 is a
// match), response-only field 38 gets the "auth code" info note, and the
// MTI/RC rows stay unannotated (their own chrome).
func TestParseExchangeCorrelationNotes(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	spec := m.app.Service().GetSpec()

	parsed, err := parseExchange(cannedExchange(t, spec))
	if err != nil {
		t.Fatalf("parseExchange: %v", err)
	}
	if !parsed.correlationOK {
		t.Error("correlationOK = false, want true")
	}
	if got, kind := noteOn(t, parsed.response, "2"); got != "echo" || kind != pages.NotePass {
		t.Errorf("PAN note = %q/%v, want echo/pass", got, kind)
	}
	if got, kind := noteOn(t, parsed.response, "11"); got != "STAN" || kind != pages.NotePass {
		t.Errorf("STAN note = %q/%v, want STAN/pass", got, kind)
	}
	if got, kind := noteOn(t, parsed.response, "38"); got != "auth code" || kind != pages.NoteInfo {
		t.Errorf("field 38 note = %q/%v, want auth code/info", got, kind)
	}
	if got, kind := noteOn(t, parsed.response, "39"); got != "" || kind != pages.NoteNone {
		t.Errorf("RC row note = %q/%v, want none", got, kind)
	}
	if got, kind := noteOn(t, parsed.response, "0"); got != "" || kind != pages.NoteNone {
		t.Errorf("MTI row note = %q/%v, want none", got, kind)
	}
	if parsed.rc != "00" || parsed.rcLabel != "APPROVED" || !parsed.rcOK {
		t.Errorf("badge = %q/%q/%v", parsed.rc, parsed.rcLabel, parsed.rcOK)
	}
}

// TestParseExchangeMismatchNotes: a STAN mismatch (normalized) fails the
// correlation and marks the STAN row ✗; a field both messages carry with
// different values gets an echo ✗ note.
func TestParseExchangeMismatchNotes(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	spec := m.app.Service().GetSpec()

	req := cannedRequest(t, spec)
	resp := mkMsg(t, spec, "0210", map[int]string{
		2: "4111111111111111", 11: "999999", 39: "96", 49: "840",
	})

	parsed, err := parseExchange(&liveExchange{Request: req, Response: resp, Wrote: true})
	if err != nil {
		t.Fatalf("parseExchange: %v", err)
	}
	if parsed.correlationOK {
		t.Error("correlationOK = true, want false (STANs differ)")
	}
	if got, kind := noteOn(t, parsed.response, "11"); got != "STAN" || kind != pages.NoteFail {
		t.Errorf("STAN note = %q/%v, want STAN/fail", got, kind)
	}
	if got, kind := noteOn(t, parsed.response, "2"); got != "echo" || kind != pages.NoteFail {
		t.Errorf("PAN note = %q/%v, want echo/fail (values differ)", got, kind)
	}
	if got, kind := noteOn(t, parsed.response, "49"); got != "echo" || kind != pages.NotePass {
		t.Errorf("currency note = %q/%v, want echo/pass (values match)", got, kind)
	}
	if parsed.rcLabel != "DECLINED" || parsed.rcOK {
		t.Errorf("badge = %q/%v, want DECLINED/error", parsed.rcLabel, parsed.rcOK)
	}
}

// TestParseExchangeSTANNormalizedMatch: the shared NormalizeStan pads
// short trace numbers, so 41822 vs 041822 correlates (raw rows differ —
// only the note proves the check, not string equality).
func TestParseExchangeSTANNormalizedMatch(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	spec := m.app.Service().GetSpec()

	req := mkMsg(t, spec, "0200", map[int]string{2: "4242424242424242", 11: "41822"})
	resp := mkMsg(t, spec, "0210", map[int]string{2: "4242424242424242", 11: "041822", 39: "00"})

	parsed, err := parseExchange(&liveExchange{Request: req, Response: resp, Wrote: true})
	if err != nil {
		t.Fatalf("parseExchange: %v", err)
	}
	if !parsed.correlationOK {
		t.Error("correlationOK = false, want true (NormalizeStan pads to 6)")
	}
	if _, kind := noteOn(t, parsed.response, "11"); kind != pages.NotePass {
		t.Errorf("STAN note kind = %v, want pass", kind)
	}
}

// TestParseExchangeErrors: the parse stage fails honestly on missing
// messages (the Receive-success contract was violated).
func TestParseExchangeErrors(t *testing.T) {
	if _, err := parseExchange(&liveExchange{}); err == nil {
		t.Error("missing request: want error")
	}
	m := NewRootModel(newTxFileApp(t))
	spec := m.app.Service().GetSpec()
	if _, err := parseExchange(&liveExchange{Request: cannedRequest(t, spec)}); err == nil {
		t.Error("missing response: want error")
	}
}

// TestExchangeRowsHexAndMask: rows carry the value hex alongside the
// display column (the `h` toggle is pure display), and for the PAN BOTH
// columns derive from the masked display — root-side masking covers the
// hex too (E5-A5-1); non-sensitive rows keep the true value hex.
func TestExchangeRowsHexAndMask(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	spec := m.app.Service().GetSpec()

	rows, err := exchangeRows(cannedResponse(t, spec))
	if err != nil {
		t.Fatalf("exchangeRows: %v", err)
	}

	byNum := map[string]pages.ExchangeRow{}
	for _, r := range rows {
		byNum[r.Num] = r
	}
	panMasked := "4242****4242"
	if pan := byNum["2"]; pan.Display != panMasked {
		t.Errorf("PAN display = %q, want masked", pan.Display)
	}
	wantHex := "34323432" + strings.Repeat("2a", 4) + "34323432"
	if pan := byNum["2"]; pan.Hex != wantHex {
		t.Errorf("PAN hex = %q, want hex of the masked display", pan.Hex)
	}
	if st := byNum["11"]; st.Display != "041822" || st.Hex != "303431383232" {
		t.Errorf("STAN row = %+v", st)
	}
	if _, ok := byNum["39"]; !ok {
		t.Error("RC row missing")
	}
}

// TestExchangeRowsAreDescribeOutput pins the UAT contract: the §D pane
// rows must carry the utils.Describe output — spec-name header, MTI,
// Bitmap HEX/bits lines, and F<n> lines with the spec description —
// not a bare num/value list.
func TestExchangeRowsAreDescribeOutput(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	spec := m.app.Service().GetSpec()

	rows, err := exchangeRows(mkMsg(t, spec, "0800", map[int]string{
		7: "0910184709", 11: "000001", 70: "301",
	}))
	if err != nil {
		t.Fatalf("exchangeRows: %v", err)
	}

	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Text + "\n")
	}
	got := b.String()
	for _, want := range []string{
		"ISO8583 Message:",
		"MTI",
		"Bitmap HEX",
		"Bitmap bits",
		"[1-8]",
		"F7   Transmission Date & Time",
		"F11  Systems Trace Audit Number (STAN)",
		"F70  Network Management Information Code",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pane text missing %q\ngot:\n%s", want, got)
		}
	}

	byNum := map[string]pages.ExchangeRow{}
	for _, r := range rows {
		if r.Num != "" {
			byNum[r.Num] = r
		}
	}
	if r := byNum["70"]; r.Display != "301" || r.Hex != hex.EncodeToString([]byte("301")) {
		t.Errorf("F70 row = %+v, want value 301 + its hex", byNum["70"])
	}
	if r := byNum["0"]; r.Display != "0800" {
		t.Errorf("F0 row = %q, want 0800 (pane MTI title source)", r.Display)
	}
}
