package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/specs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTSYSDHISpec(t *testing.T) *iso8583.MessageSpec {
	t.Helper()
	// Try relative paths depending on where test is run from
	candidates := []string{
		"../../specs/tsys_dhi.json",
		"specs/tsys_dhi.json",
		"../specs/tsys_dhi.json",
	}

	for _, path := range candidates {
		if raw, err := os.ReadFile(path); err == nil {
			spec, err := specs.ImportJSON(raw)
			require.NoError(t, err, "failed to import tsys_dhi.json from %s", path)
			require.NotNil(t, spec)
			return spec
		}
	}

	// Fallback to searching from working directory up
	cwd, err := os.Getwd()
	require.NoError(t, err)
	for i := 0; i < 4; i++ {
		p := filepath.Join(cwd, "specs", "tsys_dhi.json")
		if raw, err := os.ReadFile(p); err == nil {
			spec, err := specs.ImportJSON(raw)
			require.NoError(t, err, "failed to import tsys_dhi.json from %s", p)
			require.NotNil(t, spec)
			return spec
		}
		cwd = filepath.Dir(cwd)
	}

	t.Fatal("could not find specs/tsys_dhi.json")
	return nil
}

func TestTSYSDHI_SpecCompleteness(t *testing.T) {
	spec := loadTSYSDHISpec(t)
	assert.Equal(t, "ISO8583_DHI", spec.Name)

	// List of all data elements specified in DHI_ISO8583.md (Appendix A)
	expectedFields := []int{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
		11, 12, 13, 14, 15, 16, 18, 19, 21,
		22, 23, 24, 25, 26, 28, 29, 30, 32, 33,
		35, 37, 38, 39, 41, 42, 43, 44, 45, 48,
		49, 50, 51, 52, 53, 54, 55, 56, 60, 61,
		63, 64, 66, 70, 73, 74, 75, 76, 77, 86,
		87, 88, 89, 90, 91, 94, 95, 96, 97, 101,
		102, 103, 104, 117, 118, 120, 122, 126, 127, 128,
	}

	for _, f := range expectedFields {
		fieldDef, exists := spec.Fields[f]
		assert.True(t, exists, "Expected Field %d to exist in tsys_dhi.json", f)
		assert.NotNil(t, fieldDef, "Field %d definition should not be nil", f)
	}

	// Extra non-DHI generic ISO fields (17, 40) should not be present
	assert.Nil(t, spec.Fields[17], "Field 17 should not exist in DHI spec")
	assert.Nil(t, spec.Fields[40], "Field 40 should not exist in DHI spec")
}

func TestTSYSDHI_AuthorizationRequestAndResponse(t *testing.T) {
	spec := loadTSYSDHISpec(t)

	// 1. Build 0100 Authorization Request
	req := iso8583.NewMessage(spec)
	req.MTI("0100")
	require.NoError(t, req.Field(2, "4000123456789010"))

	// Field 3: Processing Code positional composite (000000)
	procCode := map[string]interface{}{
		"1": "00",
		"2": "00",
		"3": "00",
	}
	require.NoError(t, SetCompositeFieldValue(req, spec, 3, procCode))

	require.NoError(t, req.Field(4, "000000050000"))
	require.NoError(t, req.Field(7, "0816210459"))
	require.NoError(t, req.Field(11, "654321"))
	require.NoError(t, req.Field(12, "210459"))
	require.NoError(t, req.Field(13, "0816"))
	require.NoError(t, req.Field(14, "2812"))
	require.NoError(t, req.Field(18, "5411"))
	require.NoError(t, req.Field(19, "840"))
	require.NoError(t, req.Field(22, "0510"))
	require.NoError(t, req.Field(25, "00"))
	require.NoError(t, req.Field(26, "04")) // BCD PIN Capture Code
	require.NoError(t, req.Field(32, "12345678901"))
	require.NoError(t, req.Field(37, "622800654321"))
	require.NoError(t, req.Field(41, "TERM0001"))
	require.NoError(t, req.Field(42, "MERCHANT0000001"))
	field43Val := fmt.Sprintf("%-42s", "STORE #1234             ANYTOWN     NYUS")
	require.NoError(t, req.Field(43, field43Val))
	require.NoError(t, req.Field(49, "840"))
	require.NoError(t, req.Field(60, "55000800000000"))

	// Field 104: Large payload with LLLVAR (>255 chars)
	largeP2PData := "PP0290" + strings.Repeat("A", 280)
	require.NoError(t, req.Field(104, largeP2PData))

	// Field 126: Bitmap-governed composite
	f126Data := map[string]interface{}{
		"6":  "01",
		"7":  "02",
		"8":  "12345678901234567890", // 20-char XID
		"9":  "CAVV1234567890123456", // 20-char CAVV/AEVV
		"10": "10 123",               // 6-char CVV2
		"13": "C",                    // 1-char POS Environment ('C' Credential on File)
	}
	require.NoError(t, SetCompositeFieldValue(req, spec, 126, f126Data))

	// Pack request
	packedReq, err := req.Pack()
	require.NoError(t, err)
	require.NotEmpty(t, packedReq)

	// Unpack request
	unpackedReq := iso8583.NewMessage(spec)
	require.NoError(t, unpackedReq.Unpack(packedReq))

	mti, err := unpackedReq.GetMTI()
	require.NoError(t, err)
	assert.Equal(t, "0100", mti)

	pan, err := unpackedReq.GetString(2)
	require.NoError(t, err)
	assert.Equal(t, "4000123456789010", pan)

	amount, err := unpackedReq.GetString(4)
	require.NoError(t, err)
	assert.Equal(t, "50000", amount)

	stan, err := unpackedReq.GetString(11)
	require.NoError(t, err)
	assert.Equal(t, "654321", stan)

	cardAcceptorLoc, err := unpackedReq.GetString(43)
	require.NoError(t, err)
	assert.Equal(t, field43Val, cardAcceptorLoc)

	f104Unpacked, err := unpackedReq.GetString(104)
	require.NoError(t, err)
	assert.Equal(t, largeP2PData, f104Unpacked)

	// Extract message fields to verify composite structures
	extracted := ExtractMessageFields(unpackedReq, spec)
	require.NotNil(t, extracted)

	f3Extracted, ok := extracted["3"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "00", f3Extracted["1"])
	assert.Equal(t, "00", f3Extracted["2"])
	assert.Equal(t, "00", f3Extracted["3"])

	f126Extracted, ok := extracted["126"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "01", f126Extracted["6"])
	assert.Equal(t, "02", f126Extracted["7"])
	assert.Equal(t, "12345678901234567890", f126Extracted["8"])
	assert.Equal(t, "CAVV1234567890123456", f126Extracted["9"])
	assert.Equal(t, "10 123", f126Extracted["10"])
	assert.Equal(t, "C", f126Extracted["13"])

	// 2. Build 0110 Authorization Response
	resp := iso8583.NewMessage(spec)
	resp.MTI("0110")
	require.NoError(t, resp.Field(2, "4000123456789010"))
	require.NoError(t, SetCompositeFieldValue(resp, spec, 3, procCode))
	require.NoError(t, resp.Field(4, "000000050000"))
	require.NoError(t, resp.Field(7, "0816210459"))
	require.NoError(t, resp.Field(11, "654321"))
	require.NoError(t, resp.Field(37, "622800654321"))
	require.NoError(t, resp.Field(38, "AUTH01"))
	require.NoError(t, resp.Field(39, "00"))
	require.NoError(t, resp.Field(44, "5"))
	require.NoError(t, resp.Field(49, "840"))

	packedResp, err := resp.Pack()
	require.NoError(t, err)
	require.NotEmpty(t, packedResp)

	unpackedResp := iso8583.NewMessage(spec)
	require.NoError(t, unpackedResp.Unpack(packedResp))

	respCode, err := unpackedResp.GetString(39)
	require.NoError(t, err)
	assert.Equal(t, "00", respCode)

	authId, err := unpackedResp.GetString(38)
	require.NoError(t, err)
	assert.Equal(t, "AUTH01", authId)
}

func TestTSYSDHI_ReversalWithCompositeField90(t *testing.T) {
	spec := loadTSYSDHISpec(t)

	rev := iso8583.NewMessage(spec)
	rev.MTI("0420")
	require.NoError(t, rev.Field(2, "4000123456789010"))

	procCode := map[string]interface{}{
		"1": "00",
		"2": "00",
		"3": "00",
	}
	require.NoError(t, SetCompositeFieldValue(rev, spec, 3, procCode))

	require.NoError(t, rev.Field(4, "000000050000"))
	require.NoError(t, rev.Field(7, "0816210515"))
	require.NoError(t, rev.Field(11, "654322"))
	require.NoError(t, rev.Field(32, "12345678901"))
	require.NoError(t, rev.Field(37, "622800654321"))
	require.NoError(t, rev.Field(49, "840"))

	// Field 56: Original TIC (Transaction Identifier)
	require.NoError(t, rev.Field(56, "TIC98765432101234567890"))

	// Field 90: Original Data Elements (n42 composite)
	// 90.1: Original MTI (4)
	// 90.2: Original STAN (6)
	// 90.3: Original Tx Date/Time (10)
	// 90.4: Original Acquirer ID (11) + Forwarding ID (11) = 22 digits
	f90Data := map[string]interface{}{
		"1": "0100",
		"2": "654321",
		"3": "0816210459",
		"4": "0001234567800098765432",
	}
	require.NoError(t, SetCompositeFieldValue(rev, spec, 90, f90Data))

	// Field 95: Replacement Amounts (42 chars: 12 + 12 + 9 + 9)
	require.NoError(t, rev.Field(95, "000000025000000000000000C00000000000000000"))

	packedRev, err := rev.Pack()
	require.NoError(t, err)
	require.NotEmpty(t, packedRev)

	unpackedRev := iso8583.NewMessage(spec)
	require.NoError(t, unpackedRev.Unpack(packedRev))

	mti, err := unpackedRev.GetMTI()
	require.NoError(t, err)
	assert.Equal(t, "0420", mti)

	tic, err := unpackedRev.GetString(56)
	require.NoError(t, err)
	assert.Equal(t, "TIC98765432101234567890", tic)

	rplAmts, err := unpackedRev.GetString(95)
	require.NoError(t, err)
	assert.Equal(t, "000000025000000000000000C00000000000000000", rplAmts)

	extracted := ExtractMessageFields(unpackedRev, spec)
	require.NotNil(t, extracted)

	f90Extracted, ok := extracted["90"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "0100", f90Extracted["1"])
	assert.Equal(t, "654321", f90Extracted["2"])
	assert.Equal(t, "0816210459", f90Extracted["3"])
	assert.Equal(t, "0001234567800098765432", f90Extracted["4"])
}

func TestTSYSDHI_NetworkManagementMessage(t *testing.T) {
	spec := loadTSYSDHISpec(t)

	netReq := iso8583.NewMessage(spec)
	netReq.MTI("0800")
	require.NoError(t, netReq.Field(7, "0816210600"))
	require.NoError(t, netReq.Field(11, "999001"))
	require.NoError(t, netReq.Field(37, "622800999001"))
	require.NoError(t, netReq.Field(70, "301")) // Echo test

	packedReq, err := netReq.Pack()
	require.NoError(t, err)
	require.NotEmpty(t, packedReq)

	unpackedNet := iso8583.NewMessage(spec)
	require.NoError(t, unpackedNet.Unpack(packedReq))

	mti, err := unpackedNet.GetMTI()
	require.NoError(t, err)
	assert.Equal(t, "0800", mti)

	f70, err := unpackedNet.GetString(70)
	require.NoError(t, err)
	assert.Equal(t, "301", f70)
}

func TestTSYSDHI_AdditionalExtendedFields(t *testing.T) {
	spec := loadTSYSDHISpec(t)

	msg := iso8583.NewMessage(spec)
	msg.MTI("0200")
	require.NoError(t, msg.Field(2, "4000123456789010"))
	require.NoError(t, SetCompositeFieldValue(msg, spec, 3, map[string]interface{}{"1": "00", "2": "00", "3": "00"}))
	require.NoError(t, msg.Field(4, "000000010000"))
	require.NoError(t, msg.Field(7, "0816210600"))
	require.NoError(t, msg.Field(11, "999002"))
	require.NoError(t, msg.Field(37, "622800999002"))
	require.NoError(t, msg.Field(49, "840"))

	// Field 9: Settlement Conversion Rate
	require.NoError(t, msg.Field(9, "76887050"))
	// Field 30: Original Transaction Amount (24 chars)
	require.NoError(t, msg.Field(30, "000000010000000000010000"))
	// Field 75-77: Settlement reconciliation counts
	require.NoError(t, msg.Field(75, "0000000005"))
	require.NoError(t, msg.Field(76, "0000000010"))
	require.NoError(t, msg.Field(77, "0000000002"))
	// Field 86-89: Settlement reconciliation amounts
	require.NoError(t, msg.Field(86, "0000000000050000"))
	require.NoError(t, msg.Field(87, "0000000000005000"))
	require.NoError(t, msg.Field(88, "0000000000100000"))
	require.NoError(t, msg.Field(89, "0000000000002000"))
	// Field 117: Additional Amounts, To Account (LLLVAR)
	require.NoError(t, msg.Field(117, "010080840D000000050000"))
	// Field 118: Intra Country (LLVAR)
	require.NoError(t, msg.Field(118, "61000000000000000000000000001206"))
	// Field 120: Additional Information (LLLVAR)
	require.NoError(t, msg.Field(120, "UD020010101234567890"))
	// Field 127: Record Data (LLLVAR)
	require.NoError(t, msg.Field(127, "MMDS011006654321"))

	// Binary MAC (Field 64 and Field 128)
	require.NoError(t, msg.BinaryField(64, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}))
	require.NoError(t, msg.BinaryField(128, []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}))

	packed, err := msg.Pack()
	require.NoError(t, err)
	require.NotEmpty(t, packed)

	unpacked := iso8583.NewMessage(spec)
	require.NoError(t, unpacked.Unpack(packed))

	f9, err := unpacked.GetString(9)
	require.NoError(t, err)
	assert.Equal(t, "76887050", f9)

	f30, err := unpacked.GetString(30)
	require.NoError(t, err)
	assert.Equal(t, "000000010000000000010000", f30)

	f75, err := unpacked.GetString(75)
	require.NoError(t, err)
	assert.Equal(t, "0000000005", f75)

	f86, err := unpacked.GetString(86)
	require.NoError(t, err)
	assert.Equal(t, "0000000000050000", f86)

	f117, err := unpacked.GetString(117)
	require.NoError(t, err)
	assert.Equal(t, "010080840D000000050000", f117)

	f118, err := unpacked.GetString(118)
	require.NoError(t, err)
	assert.Equal(t, "61000000000000000000000000001206", f118)

	f120, err := unpacked.GetString(120)
	require.NoError(t, err)
	assert.Equal(t, "UD020010101234567890", f120)

	f127, err := unpacked.GetString(127)
	require.NoError(t, err)
	assert.Equal(t, "MMDS011006654321", f127)

	f64Field := unpacked.GetField(64)
	require.NotNil(t, f64Field)
	f64Bytes, err := f64Field.Bytes()
	require.NoError(t, err)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, f64Bytes)

	f128Field := unpacked.GetField(128)
	require.NotNil(t, f128Field)
	f128Bytes, err := f128Field.Bytes()
	require.NoError(t, err)
	assert.Equal(t, []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}, f128Bytes)
}
