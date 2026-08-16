package base2

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/db"
)

func TestCalculateLuhn(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"7992739871", 3},
		{"4992739871", 6},
		{"7400129263060000000001", 0},
	}

	for _, tt := range tests {
		result := CalculateLuhn(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestGenerateARN(t *testing.T) {
	txTime := time.Date(2026, 11, 2, 14, 23, 38, 0, time.UTC)
	arn := GenerateARN("400129", txTime, 1)

	assert.Equal(t, 23, len(arn))
	assert.True(t, strings.HasPrefix(arn, "7400129"))
	checkDigit := CalculateLuhn(arn[:22])
	assert.Equal(t, string(arn[22]), string(rune('0'+checkDigit)))
}

func TestRecordFormattingLengths(t *testing.T) {
	r := NewRecord()
	assert.Equal(t, RecordLength, len(r.String()))

	r.Set(1, 2, "05", true, '0')
	r.Set(5, 20, "4000001234560001", false, ' ')
	r.Set(62, 73, "000000010000", true, '0')
	assert.Equal(t, RecordLength, len(r.String()))
}

func TestGenerateCTF_ApprovedTransactionsAndTrailers(t *testing.T) {
	session := &db.SessionRecord{
		SessionID: "test-session-visa-1",
		SpecName:  "visa.json",
	}

	respJSONSuccess := `{"mti":"0110","fields":{"38":"123456","39":"00"}}`
	respJSONDeclined := `{"mti":"0110","fields":{"38":"000000","39":"05"}}`

	txs := []*db.EnrichedTransactionRecord{
		{
			ID:           1,
			SessionID:    session.SessionID,
			Timestamp:    time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
			TxName:       "Purchase Approved 1",
			Success:      true,
			ResponseCode: "00",
			RequestJSON:  `{"mti":"0100","fields":{"2":"4000001234560001","3":"000000","4":"000000015000","41":"TERM0001","42":"MERCH0000000001","43":"TEST MERCHANT 1          TEST CITY    840","49":"840"}}`,
			ResponseJSON: &respJSONSuccess,
		},
		{
			ID:           2,
			SessionID:    session.SessionID,
			Timestamp:    time.Date(2026, 11, 2, 10, 5, 0, 0, time.UTC),
			TxName:       "Cash Disbursement Approved",
			Success:      true,
			ResponseCode: "00",
			RequestJSON:  `{"mti":"0100","fields":{"2":"4111111234560002","3":"010000","4":"000000025000","41":"TERM0002","42":"MERCH0000000002","43":"ATM WITHDRAWAL 2         TEST CITY    840","49":"840"}}`,
			ResponseJSON: &respJSONSuccess,
		},
		{
			ID:           3,
			SessionID:    session.SessionID,
			Timestamp:    time.Date(2026, 11, 2, 10, 10, 0, 0, time.UTC),
			TxName:       "Declined Transaction",
			Success:      false,
			ResponseCode: "05",
			RequestJSON:  `{"mti":"0100","fields":{"2":"4000001234560003","3":"000000","4":"000000050000","49":"840"}}`,
			ResponseJSON: &respJSONDeclined,
		},
	}

	opts := GeneratorOptions{
		CIB:            "400129",
		BatchNumber:    1,
		GenerationTime: time.Date(2026, 11, 2, 14, 23, 38, 0, time.UTC),
	}

	result, err := GenerateCTF(session, txs, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// 2 approved transactions: each generates TCR 0, TCR 1, TCR 5 (= 6 TCRs)
	// Plus TC 91 (Batch Trailer) and TC 92 (File Trailer) = 8 total lines
	assert.Equal(t, 2, result.MonetaryTxCount)
	assert.Equal(t, int64(40000), result.DestinationAmountSum)
	assert.Equal(t, int64(40000), result.SourceAmountSum)
	assert.Equal(t, 1, result.SkippedCount)
	assert.Equal(t, 8, len(result.Records))

	// Validate that every record is strictly 168 characters
	lines := strings.Split(strings.TrimRight(string(result.RawContent), "\n"), "\n")
	require.Equal(t, 8, len(lines))
	for idx, line := range lines {
		assert.Equal(t, 168, len(line), "Line %d length mismatch: %s", idx+1, line)
	}

	// Verify TCR codes for tx 1 (Purchase -> TC 05)
	assert.Equal(t, "0500", lines[0][:4])
	assert.Equal(t, "0501", lines[1][:4])
	assert.Equal(t, "0505", lines[2][:4])

	// Verify TCR codes for tx 2 (Cash -> TC 07)
	assert.Equal(t, "0700", lines[3][:4])
	assert.Equal(t, "0701", lines[4][:4])
	assert.Equal(t, "0705", lines[5][:4])

	// Verify TC 91 & TC 92 Trailers
	assert.Equal(t, "9100", lines[6][:4])
	assert.Equal(t, "9200", lines[7][:4])

	// TC 91 total TCRs count check (6 data TCRs + 1 batch trailer = 7 TCRs in batch)
	assert.Equal(t, "000000000007", lines[6][48:60])
	// TC 91 monetary count check (2 transactions)
	assert.Equal(t, "000000000002", lines[6][30:42])
}

func TestGenerateCTF_BINFilter(t *testing.T) {
	session := &db.SessionRecord{
		SessionID: "test-session-visa-bin",
		SpecName:  "visa.json",
	}

	respJSONSuccess := `{"mti":"0110","fields":{"38":"123456","39":"00"}}`

	txs := []*db.EnrichedTransactionRecord{
		{
			ID:           1,
			SessionID:    session.SessionID,
			Timestamp:    time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
			TxName:       "BIN 400000 tx",
			Success:      true,
			ResponseCode: "00",
			RequestJSON:  `{"mti":"0100","fields":{"2":"4000001234560001","3":"000000","4":"000000010000"}}`,
			ResponseJSON: &respJSONSuccess,
		},
		{
			ID:           2,
			SessionID:    session.SessionID,
			Timestamp:    time.Date(2026, 11, 2, 10, 5, 0, 0, time.UTC),
			TxName:       "BIN 411111 tx",
			Success:      true,
			ResponseCode: "00",
			RequestJSON:  `{"mti":"0100","fields":{"2":"4111111234560002","3":"000000","4":"000000020000"}}`,
			ResponseJSON: &respJSONSuccess,
		},
	}

	// Filter by BIN 400000
	opts := GeneratorOptions{
		BINFilter:      "400000",
		CIB:            "400129",
		GenerationTime: time.Date(2026, 11, 2, 14, 23, 38, 0, time.UTC),
	}

	result, err := GenerateCTF(session, txs, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 1, result.MonetaryTxCount)
	assert.Equal(t, int64(10000), result.DestinationAmountSum)
	assert.Equal(t, 1, result.SkippedCount)

	// Filter by non-existent BIN -> returns error
	optsNonExistent := GeneratorOptions{
		BINFilter:      "999999",
		GenerationTime: time.Date(2026, 11, 2, 14, 23, 38, 0, time.UTC),
	}
	_, err = GenerateCTF(session, txs, optsNonExistent)
	assert.Error(t, err)
}

func TestGenerateCTF_CompositeSubfieldsAndCleanTID(t *testing.T) {
	session := &db.SessionRecord{
		SessionID: "test-session-subfields",
		SpecName:  "visa.json",
	}

	respStructuredJSON := `{"mti":"0110","fields":{"38":"930216","39":"00","62":{"2":"466215320236000"}}}`
	respRawBinaryJSON := `{"mti":"0110","fields":{"38":"930216","39":"00","62":"@\u0000\u0000\u0000\u0000\u0000\u0000\u0000\u0003\ufffd!S't\u0000\u0000"}}`

	txs := []*db.EnrichedTransactionRecord{
		{
			ID:           1,
			SessionID:    session.SessionID,
			Timestamp:    time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
			TxName:       "Structured Subfields Tx",
			Success:      true,
			ResponseCode: "00",
			RequestJSON: `{
				"mti": "0100",
				"fields": {
					"2": "4085652009074000",
					"3": "000000",
					"4": "4598",
					"34": {
						"02": {
							"C1": "APPLE.COM/BILL",
							"C2": "ITUNES.COM",
							"C3": "DUBAI",
							"C6": "AE"
						}
					},
					"41": "99999999",
					"42": "212963000200925",
					"49": "784"
				}
			}`,
			ResponseJSON: &respStructuredJSON,
		},
		{
			ID:           2,
			SessionID:    session.SessionID,
			Timestamp:    time.Date(2026, 11, 2, 10, 5, 0, 0, time.UTC),
			TxName:       "Raw Binary Field 62 Fallback Tx",
			Success:      true,
			ResponseCode: "00",
			RequestJSON: `{
				"mti": "0100",
				"fields": {
					"2": "4085658930133000",
					"3": "000000",
					"4": "12996",
					"41": "99999999",
					"42": "212963000200925",
					"43": "APPLE.COM/BILL           ITUNES.COM   IE",
					"49": "784"
				}
			}`,
			ResponseJSON: &respRawBinaryJSON,
		},
	}

	opts := GeneratorOptions{
		CIB:            "400129",
		GenerationTime: time.Date(2026, 11, 2, 14, 23, 38, 0, time.UTC),
	}

	result, err := GenerateCTF(session, txs, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	lines := strings.Split(strings.TrimRight(string(result.RawContent), "\n"), "\n")
	require.Equal(t, 8, len(lines))

	// Validate that EVERY character across all 8 lines is strictly printable ASCII (32-126)
	for lineIdx, line := range lines {
		require.Equal(t, 168, len(line), "Line %d length mismatch", lineIdx+1)
		for colIdx, b := range []byte(line) {
			assert.True(t, b >= 32 && b <= 126, "Non-printable byte 0x%02x found at Line %d, Col %d", b, lineIdx+1, colIdx+1)
		}
	}

	// Verify TCR 5 for tx 1 (TID extracted from structured subfield 62.2)
	tcr5Tx1 := lines[2]
	assert.Equal(t, "0505", tcr5Tx1[:4])
	assert.Equal(t, "466215320236000", tcr5Tx1[4:19], "TID should match structured 62.2 subfield value")

	// Verify TCR 5 for tx 2 (TID clean numeric from fallback)
	tcr5Tx2 := lines[5]
	assert.Equal(t, "0505", tcr5Tx2[:4])
	for _, c := range tcr5Tx2[4:19] {
		assert.True(t, c >= '0' && c <= '9', "TID in TCR 5 must be all digits, got %c", c)
	}
}

