// inspector_fixture_test.go: the §C test fixture (sample
// values) shared by the behaviour and golden tests.
package pages

import (
	"strings"
	"testing"
)

// inspPurchase is the §C sample: MTI, masked PAN, plain
// fields, TS/STAN auto rows with preview pools, a nested composite
// (55 → 95 → 01), and a field above the bitmap extension bit.
func inspPurchase() InspectorState {
	return InspectorState{
		TxID: "Purchase", TxName: "Purchase", MsgIndex: 1, MsgTotal: 12,
		Fields: []FieldRow{
			{Num: "0", Name: "MTI", Display: "0200"},
			{Num: "2", Name: "PAN", Display: "4567••••••••3456", Masked: true},
			{Num: "3", Name: "Proc Code", Display: "000000"},
			{
				Num: "7", Name: "Transmission TS", Display: "0906120411",
				Auto: true, RawPreview: "auto",
				AutoPreviews: []string{"0906120411", "0906120511", "0906120611"},
			},
			{
				Num: "11", Name: "STAN", Display: "041822",
				Auto: true, RawPreview: "stan",
				AutoPreviews: []string{"041822", "041823"},
			},
			{Num: "55", Name: "EMV", Children: []FieldRow{
				{
					Num: "95", Name: "Terminal caps", Display: "0000000000",
					Children: []FieldRow{{Num: "01", Name: "Nested", Display: "aa"}},
				},
			}},
			{Num: "70", Name: "NMIC", Display: "301"},
		},
		DescribeText: []string{
			"ISO8583 Message:",
			"MTI..........: 0200",
			"F0   Message Type Indicator..: 0200",
			"F2   Primary Account Number..: 4567****3456",
			"F70  Network Info Code.......: 301",
		},
		PackedDump: []string{
			"00000000  00 79 32 30 30 00 30 00  30 00 30 00 32 30 00 32  |.y200.0.0.0.20.2|",
			"00000010  35 31 32                                         |512|",
		},
		PackedHex:  []string{"0079323030003000", "3000300032300032", "353132"},
		HeaderNote: "len hdr: binary2 - msg 19 bytes",
		Validation: []ValidationRow{{Text: "ok", OK: true}, {Text: "subfield lengths sum", OK: true}},
		RawJSON:    "{\n  \"name\": \"Purchase\",\n  \"mti\": \"0200\"\n}",
	}
}

// inspTreePurchase is the same snapshot WITHOUT the Describe output:
// the tree interaction tests (expand/enter/pool/scroll) exercise the
// interpolated-tree fallback that renders when root has no Describe
// text (raw messages, pre-wire states).
func inspTreePurchase() InspectorState {
	st := inspPurchase()
	st.DescribeText = nil

	return st
}

// inspBroken is a failing-validation snapshot: one red-lined field row
// plus a failed validation line.
func inspBroken() InspectorState {
	st := inspTreePurchase()
	st.Fields[2].Error = "field 3 (Processing Code) must be numeric, got: x"
	st.Validation = []ValidationRow{{Text: "field 3 (Processing Code) must be numeric, got: x"}}

	return st
}

// inspPage builds an ascii-themed inspector with the fixture at size.
func inspPage(t *testing.T, state InspectorState, w, h int) *Inspector {
	t.Helper()

	in := NewInspector(asciiTheme(t))
	in.SetState(state)
	_, _ = in.Update(windowSize(w, h))

	return in
}

// inspBody renders the page body lines (no frame chrome).
func inspBody(t *testing.T, in *Inspector) []string {
	t.Helper()

	return strings.Split(strings.TrimRight(in.View().Content, "\n"), "\n")
}

// inspToTab cycles tabs until tab == want (fails if it cycles past it).
func inspToTab(t *testing.T, in *Inspector, want int) {
	t.Helper()

	for range ViewTabCount + 1 {
		if in.Tab() == want {
			return
		}
		_, _ = in.Update(PaneFocusMsg{})
	}
	t.Fatalf("tab never reached %d", want)
}

// cursorOn moves the cursor onto the field number (top-level walk).
func cursorOn(t *testing.T, in *Inspector, num string) {
	t.Helper()

	for range 32 {
		if vr, ok := in.rowAt(in.Cursor()); ok && vr.row.Num == num {
			return
		}

		_, cmd := in.Update(press('j'))
		if cmd != nil {
			t.Fatalf("cursor move ran a cmd")
		}
	}
	t.Fatalf("cursor never reached field %s", num)
}
