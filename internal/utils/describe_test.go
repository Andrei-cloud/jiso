package utils

import (
	"bytes"
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/require"
)

func TestDescribeUsesUpstreamFormatter(t *testing.T) {
	msg := iso8583.NewMessage(iso8583.Spec87)
	msg.MTI("0200")
	require.NoError(t, msg.Field(7, "123456"))
	require.NoError(t, msg.Field(11, "12345"))

	var buf bytes.Buffer
	err := Describe(msg, &buf)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, "ISO 8583")
	require.Contains(t, out, "Message Type Indicator")
	require.Contains(t, out, "F7")
	require.Contains(t, out, "F11")
}
