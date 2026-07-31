package utils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBinary4BytesAdapter(t *testing.T) {
	adapter := NewBinary4BytesAdapter()
	adapter.SetLength(349)
	require.Equal(t, 349, adapter.Length())

	var buf bytes.Buffer
	n, err := adapter.WriteTo(&buf)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Equal(t, []byte{0x00, 0x00, 0x01, 0x5d}, buf.Bytes())

	readAdapter := NewBinary4BytesAdapter()
	nRead, err := readAdapter.ReadFrom(&buf)
	require.NoError(t, err)
	require.Equal(t, 4, nRead)
	require.Equal(t, 349, readAdapter.Length())
}
