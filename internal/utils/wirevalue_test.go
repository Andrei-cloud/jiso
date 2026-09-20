package utils

import (
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/specs"
	"github.com/stretchr/testify/require"
)

// loadSpec parses an inline spec JSON through the production builder.
func loadSpec(t *testing.T, js string) *iso8583.MessageSpec {
	t.Helper()
	spec, err := specs.ImportJSON([]byte(js))
	require.NoError(t, err)

	return spec
}

// The bug: moov Numeric keeps an int64, so String() loses the leading zero
// the packer writes back from the spec's padding. WireValue must restore
// exactly the digits that travel on the wire.
func TestWireValuePadsNumericToWireShape(t *testing.T) {
	visa := ResolveSpec("../../specs/visa.json", nil)
	require.NotNil(t, visa)

	msg := iso8583.NewMessage(visa)
	msg.MTI("0100")

	require.NoError(t, msg.Field(7, GetTrxnDateTime()))
	str, err := WireValue(msg.GetField(7))
	require.NoError(t, err)
	require.Len(t, str, 10, "DE7 must describe with its fixed 10 wire digits")
	require.Equal(t, "0", string(str[0]), "Sept-Dec months carry a leading zero the wire did carry")

	require.NoError(t, msg.Field(11, "9913"))
	str, err = WireValue(msg.GetField(11))
	require.NoError(t, err)
	require.Equal(t, "009913", str)
}

// String fields keep their own value (no int64 loss), so WireValue must not
// invent padding where the spec has none — and must mirror the packer where
// the spec does define a padder.
func TestWireValueStringFields(t *testing.T) {
	mini := loadSpec(t, `{"name":"MINI","fields":{
		"0":  {"type":"String","length":4,"enc":"ASCII","prefix":"ASCII.Fixed"},
		"1":  {"type":"Bitmap","length":8,"enc":"Binary","prefix":"Binary.Fixed"},
		"63": {"type":"String","length":6,"enc":"ASCII","prefix":"ASCII.Fixed"},
		"48": {"type":"String","length":10,"enc":"ASCII","prefix":"ASCII.Fixed","padding":{"type":"Left","pad":"0"}},
		"60": {"type":"Numeric","length":6,"enc":"BCD","prefix":"BCD.Fixed"}
	}}`)

	msg := iso8583.NewMessage(mini)
	msg.MTI("0100")

	require.NoError(t, msg.Field(63, "09913"))
	str, err := WireValue(msg.GetField(63))
	require.NoError(t, err)
	require.Equal(t, "09913", str, "unpadded String keeps its stored value verbatim")

	require.NoError(t, msg.Field(48, "AB"))
	str, err = WireValue(msg.GetField(48))
	require.NoError(t, err)
	require.Equal(t, "00000000AB", str, "padded String mirrors the packer")

	require.NoError(t, msg.Field(60, "123"))
	str, err = WireValue(msg.GetField(60))
	require.NoError(t, err)
	require.Equal(t, "000123", str, "digits-only Numeric without a padder still travelled zero-filled")
}

// Binary (hex string) values must not be touched.
func TestWireValueLeavesBinaryAlone(t *testing.T) {
	visa := ResolveSpec("../../specs/visa.json", nil)
	msg := iso8583.NewMessage(visa)
	msg.MTI("0100")

	require.NoError(t, msg.BinaryField(52, []byte{0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x00, 0x11}))
	str, err := WireValue(msg.GetField(52))
	require.NoError(t, err)
	require.Equal(t, "0A0B0C0D0E0F0011", str)
}
