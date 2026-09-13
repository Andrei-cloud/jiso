package tui

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// newMaskTxApp builds a real app over a caller-supplied tx file (the
// newTxFileApp idiom with a custom fixture; never run in parallel).
func newMaskTxApp(t *testing.T, txJSON string) *app.App {
	t.Helper()

	txFile := t.TempDir() + "/mask.json"
	if err := os.WriteFile(txFile, []byte(txJSON), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	cfg.SetSpec("../../specs/spec.json")
	cfg.SetFile(txFile)

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	time.Sleep(20 * time.Millisecond) // let app construction goroutines settle

	return a
}

// TestIsPANFieldExtended: the masking set mirrors moov's DefaultFilters
// (field_filter.go at iso8583 v0.26.0: 2/20/35/36/45/52/55) plus
// name-based matches (account number / track / PIN / EMV as a word).
func TestIsPANFieldExtended(t *testing.T) {
	for _, tc := range []struct {
		n    int
		name string
		want bool
	}{
		{2, "Primary Account Number", true},
		{20, "PAN Extended Country Code", true},
		{35, "Track 2 Data", true},
		{36, "Track 3 Data", true},
		{45, "Track 1 Data", true},
		{52, "PIN Data", true},
		{55, "ICC Data – EMV Having Multiple Tags", true},
		{2, "", true},
		{55, "", true},
		{101, "Account Number Suffix", true},
		{48, "Additional Data – Track Equivalent Data", true},
		{11, "Systems Trace Audit Number", false},
		{3, "Processing Code", false},
		{38, "Authorization Identification Response", false},
		{62, "Mapping Key", false},
		{64, "Message Authentication Code", false},
		{0, "", false},
	} {
		if got := isPANField(tc.n, tc.name); got != tc.want {
			t.Errorf("isPANField(%d, %q) = %v, want %v", tc.n, tc.name, got, tc.want)
		}
	}
}

// TestExchangeRowsSensitiveHexMasked: for the sensitive fields (2, 52,
// 55) BOTH columns derive from the masked display — the `h` toggle can
// never leak the raw value — while a non-sensitive row's hex stays the
// true value encoding.
func TestExchangeRowsSensitiveHexMasked(t *testing.T) {
	m := NewRootModel(newTxFileApp(t))
	spec := m.app.Service().GetSpec()

	msg := mkMsg(t, spec, "0210", map[int]string{
		2: "4242424242424242", 11: "041822",
		52: "1234567890123456", 55: "9F0206000000000000009F0306000000000000",
	})
	rows, err := exchangeRows(msg)
	if err != nil {
		t.Fatalf("exchangeRows: %v", err)
	}

	byNum := map[string]pages.ExchangeRow{}
	for _, r := range rows {
		byNum[r.Num] = r
	}

	// Describe's DefaultFilters are the canonical masking now: the
	// §D panes render the Describe output verbatim.
	panMasked := "4242****4242"
	if pan := byNum["2"]; pan.Display != panMasked {
		t.Errorf("PAN display = %q, want %q", pan.Display, panMasked)
	}
	if pan := byNum["2"]; pan.Hex != hex.EncodeToString([]byte(panMasked)) {
		t.Errorf("PAN hex = %q, want hex of the masked display", pan.Hex)
	}
	if pan := byNum["2"]; pan.Hex == hex.EncodeToString([]byte("4242424242424242")) {
		t.Error("PAN hex still carries the raw value")
	}

	pinMasked := "12****56"
	if pin := byNum["52"]; pin.Display != pinMasked || pin.Hex != hex.EncodeToString([]byte(pinMasked)) {
		t.Errorf("PIN row = %+v, want display+hex of %q", byNum["52"], pinMasked)
	}

	emvMasked := "9F02 ... 0000"
	if emvRow := byNum["55"]; emvRow.Display != emvMasked || emvRow.Hex != hex.EncodeToString([]byte(emvMasked)) {
		t.Errorf("EMV row = %+v, want display+hex masked", emvRow)
	}

	if st := byNum["11"]; st.Display != "041822" || st.Hex != "303431383232" {
		t.Errorf("non-sensitive STAN row = %+v, want raw display+hex", st)
	}
}

// TestInspectorRawJSONMasksParsedSurfaces: the §C raw json tab carries
// the MASKED parsed view (declared fields and parsed message), while
// the packed wire dump stays raw BY DESIGN — the documented E5-A5
// contract (identical to `jiso inspect --json`'s packed hex).
func TestInspectorRawJSONMasksParsedSurfaces(t *testing.T) {
	m := NewRootModel(newMaskTxApp(t, `[
	 {"type":"transaction","name":"Purchase","description":"d",
	  "fields":{"0":"0200","2":"4242424242424242","11":"041822","52":"1234567890123456"}}
	]`))

	st := m.inspectorStateFor("Purchase")
	if st.RawJSON == "" || len(st.PackedHex) == 0 {
		t.Fatalf("empty snapshot: %+v", st)
	}
	if strings.Contains(st.RawJSON, "4242424242424242") {
		t.Errorf("raw PAN crossed to the page in RawJSON:\n%s", st.RawJSON)
	}
	if strings.Contains(st.RawJSON, "1234567890123456") {
		t.Errorf("raw PIN crossed to the page in RawJSON:\n%s", st.RawJSON)
	}
	if !strings.Contains(st.RawJSON, "4242••••••••4242") {
		t.Errorf("masked PAN missing from RawJSON:\n%s", st.RawJSON)
	}

	// PackedHex is the raw wire dump: the PAN's true bytes must be
	// present, unmasked (the contract, pinned).
	joined := strings.Join(st.PackedHex, "")
	if !strings.Contains(joined, "34323432343234323432343234323432") {
		t.Errorf("PackedHex no longer carries the raw wire dump:\n%s", joined)
	}
	// The view inside the JSON: parsed surfaces masked, packed_hex raw.
	var view app.InfoView
	if err := json.Unmarshal([]byte(st.RawJSON), &view); err != nil {
		t.Fatalf("RawJSON is not the inspect view: %v", err)
	}
	if !strings.Contains(view.ParsedMessage, "4242••••••••4242") {
		t.Errorf("parsed_message lacks the masked PAN:\n%s", view.ParsedMessage)
	}
	if got, _ := view.Fields["2"].(string); got != "4242••••••••4242" {
		t.Errorf("declared field 2 = %q, want masked", got)
	}
	// utils.HexDump is an offset-columned dump (bytes spaced, PAN split
	// across lines), so strip separators and look for 8 of the PAN's
	// true bytes in one run (masked data could never produce it).
	viewHex := strings.NewReplacer(" ", "", "\n", "").Replace(view.PackedHEX)
	if !strings.Contains(viewHex, "3432343234323432") {
		t.Error("view packed_hex entry lost the raw wire dump")
	}
}

// TestInspectorAutoPoolMaskedForSensitive: the `r` pool for a sensitive
// auto row renders INSTEAD of Display, so root masks every pool entry
// (field 2 "random" from the dataset: no raw value in the pool).
func TestInspectorAutoPoolMaskedForSensitive(t *testing.T) {
	m := NewRootModel(newMaskTxApp(t, `[
	 {"type":"transaction","name":"Purchase","description":"d",
	  "fields":{"0":"0200","2":"random"},
	  "dataset":[{"2":"4242424242424242"},{"2":"4111111111111111"}]}
	]`))

	st := m.inspectorStateFor("Purchase")
	var row *pages.FieldRow
	for i := range st.Fields {
		if st.Fields[i].Num == "2" {
			row = &st.Fields[i]
		}
	}
	if row == nil || !row.Auto || !row.Masked {
		t.Fatalf("field 2 row = %+v, want auto+masked", row)
	}
	if len(row.AutoPreviews) == 0 {
		t.Fatal("field 2 random row carries no preview pool")
	}
	for _, p := range row.AutoPreviews {
		if !strings.Contains(p, "•") {
			t.Errorf("pool entry %q is not masked", p)
		}
	}
}

// fakeComposite exposes GetSubfields (the seam subfieldRows walks) over
// canned subfield fields, isolating the masking pass from moov's
// composite spec validation.
type fakeComposite struct {
	field.Field
	subs map[string]field.Field
}

func (f *fakeComposite) GetSubfields() map[string]field.Field { return f.subs }

// TestSubfieldRowsNameMasked: subfields carry names, not numbers — the
// account-number-bearing subfield is masked, a benign one is not.
func TestSubfieldRowsNameMasked(t *testing.T) {
	pan := field.NewString(&field.Spec{
		Length: 19, Description: "Primary Account Number",
		Enc: encoding.ASCII, Pref: prefix.ASCII.LL,
	})
	pan.SetValue("4242424242424242")
	exp := field.NewString(&field.Spec{
		Length: 4, Description: "Expiration Date",
		Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed,
	})
	exp.SetValue("3012")
	track := &fakeComposite{
		Field: field.NewString(&field.Spec{
			Length: 37, Description: "Track 2 Data",
			Enc: encoding.ASCII, Pref: prefix.ASCII.LL,
		}),
		subs: map[string]field.Field{"1": pan, "3": exp},
	}

	rows := subfieldRows(track, 1)
	byNum := map[string]pages.FieldRow{}
	for _, r := range rows {
		byNum[r.Num] = r
	}
	if r := byNum["1"]; !r.Masked || r.Display != "4242••••••••4242" {
		t.Errorf("PAN subfield row = %+v, want masked", r)
	}
	if r := byNum["3"]; r.Masked || r.Display != "3012" {
		t.Errorf("expiration subfield = %+v, want raw", r)
	}
}
