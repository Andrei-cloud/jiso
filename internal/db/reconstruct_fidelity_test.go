package db

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"jiso/internal/utils"
)

// A session configured on a flex spec whose transactions were composed from
// stamped Visa templates must still show up for the CTF export: the per-row
// spec_name records the dialect the message spoke.
func TestVisaSessionsMatchOnStampedSpecName(t *testing.T) {
	dbPath := t.TempDir() + "/stamped.db"
	require.NoError(t, InitDB(dbPath))
	defer func() { _ = Close() }()

	require.NoError(t, UpsertSession("sess-flex-stamped", "specs/flex.json", "flex.json",
		"vis_capture.json", "vis_capture.json", "127.0.0.1", "9999", "CLIENT", "binary2", "active", false))

	resp00 := `{"mti":"0110","fields":{"39":"00"}}`
	require.NoError(t, InsertTransactionEnriched(&EnrichedTransactionRecord{
		SessionID:    "sess-flex-stamped",
		TxName:       "Tx 0100 DE3=012000 #102",
		Success:      true,
		ResponseCode: "00",
		SpecName:     "ISO8583_VISA",
		RequestJSON:  `{"mti":"0100","fields":{"4":"000000008300"}}`,
		ResponseJSON: &resp00,
	}))

	sessions, err := GetVisaSessions()
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "sess-flex-stamped", sessions[0].SessionID)
	require.Equal(t, 1, sessions[0].SuccessCount)

	txs, err := GetApprovedVisaTransactions("sess-flex-stamped")
	require.NoError(t, err)
	require.Len(t, txs, 1)
}

// Composite field values must render as a nested readable tree, never as a
// Go map dump, even when the resolved spec does not define the composite.
func TestReconstructCompositeTreeNotMapDump(t *testing.T) {
	msgJSON := `{"mti":"0100","fields":{"3":"000000","4":"000000083500","62":{"2":{"04":"4829","05":"02"}},"63":{"1":"1234"}}}`

	res, err := Reconstruct(msgJSON, "", "specs/flex.json")
	require.NoError(t, err)

	require.Contains(t, res.DescribeText, "F62 ")
	require.Contains(t, res.DescribeText, "SUBFIELDS:")
	require.Contains(t, res.DescribeText, "04 ")
	require.NotContains(t, res.DescribeText, "map[", "composite values must never stringify as Go maps")
}

// When the row carries recorded wire bytes, the HEX pane shows those bytes
// verbatim: no repack, no spec-mismatch failure, no ParseError — even if the
// recorded message could never pack under the resolved spec.
func TestReconstructPrefersRecordedHex(t *testing.T) {
	// Wire bytes of a visa-dialect 0100 (packed BCD DE7); flex's fixed ASCII
	// spec cannot repack these values, which used to surface as a pack error.
	recordedHex := "0100F220008000000000000000000000000920160705009913"
	msgJSON := `{"mti":"0100","fields":{"7":"0920160705","11":"009913"}}`

	res, err := Reconstruct(msgJSON, recordedHex, "specs/flex.json")
	require.NoError(t, err)

	require.NotEmpty(t, res.HEX)
	require.NotContains(t, res.HEX, "Failed to pack")
	require.Empty(t, res.ParseError)
	require.Contains(t, res.DescribeText, "0920160705")
}

// Legacy rows stored a formatted HexDump blob in the hex column; the parser
// must recover the original bytes from it.
func TestParseStoredHexLegacyFormattedDump(t *testing.T) {
	original := []byte{0x01, 0x00, 0xF2, 0x20, 0x09, 0x20, 0x16, 0x07, 0x05}
	formatted := utils.HexDump(original)

	raw, ok := parseStoredHex(formatted)
	require.True(t, ok)
	require.Equal(t, original, raw)

	raw, ok = parseStoredHex(strings.ToUpper("0100f2200920160705"))
	require.True(t, ok)
	require.Equal(t, []byte{0x01, 0x00, 0xF2, 0x20, 0x09, 0x20, 0x16, 0x07, 0x05}, raw)

	_, ok = parseStoredHex("   ")
	require.False(t, ok)
}
