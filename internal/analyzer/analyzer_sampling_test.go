package analyzer

import (
	"encoding/binary"
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/utils"
)

// analyzer_sampling_test.go pins the unparsable-sample
// collector: the sampled extractor records WHERE framing breaks and
// WHAT the analyzer choked on, bounded by the sample cap.

// frameBinary2 frames one payload with a 2-byte big-endian length prefix.
func frameBinary2(payload []byte) []byte {
	out := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(payload)))
	copy(out[2:], payload)

	return out
}

// TestExtractMessagesFromStreamSampled pins the reviewer
// data path: a framed message that will not unpack yields a sample with
// its stream offset, byte length, unpack reason, and head, while the
// good messages still extract.
func TestExtractMessagesFromStreamSampled(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()
	a := NewStreamAnalyzer(spec)

	good := iso8583.NewMessage(spec)
	good.MTI("0200")
	require.NoError(t, good.Field(3, "000000"))
	require.NoError(t, good.Field(7, "0412232900"))
	require.NoError(t, good.Field(11, "000001"))
	require.NoError(t, good.Field(2, "4111111111111111"))
	p1, err := good.Pack()
	require.NoError(t, err)

	// A truncated payload is framed cleanly but will not unpack (the
	// declared field 2 runs past the bytes on hand).
	bad := p1[:len(p1)-8]

	stream := append([]byte{}, frameBinary2(p1)...)
	stream = append(stream, frameBinary2(bad)...)

	collector := &UnparsableCollector{}
	extracted, unparsed, err := a.ExtractMessagesFromStreamSampled(stream, collector, "binary2")
	require.NoError(t, err)
	assert.Len(t, extracted, 1, "the good message still extracts")
	assert.Equal(t, 1, unparsed)

	samples := collector.Samples()
	require.Len(t, samples, 1)
	// The bad body starts after the good frame (2 + len(p1)) and the
	// bad frame's own 2-byte length prefix.
	assert.Equal(t, int64(4+len(p1)), samples[0].Offset)
	assert.Equal(t, len(bad), samples[0].Length)
	assert.NotEmpty(t, samples[0].Reason)
	assert.Equal(t, bad, samples[0].Head, "head holds the whole short body")

	// The fields that unpacked BEFORE the failure are
	// described (MTI always parses first), and the sample names the byte
	// where parsing stopped (past the MTI/bitmap, within the body).
	require.NotEmpty(t, samples[0].Fields, "parsed-before-failure fields are described")
	assert.Equal(t, "0", samples[0].Fields[0].ID, "MTI is the first described field")
	assert.Equal(t, "0200", samples[0].Fields[0].Value)
	assert.Greater(t, samples[0].FailedAt, 0, "the stop offset is past the start")
	assert.LessOrEqual(t, samples[0].FailedAt, len(bad), "the stop offset is within the body")
}

// TestUnparsableCollectorCapsSamples pins the memory bound:
// the total counts every failure, the kept samples stop at the cap.
func TestUnparsableCollectorCapsSamples(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()
	a := NewStreamAnalyzer(spec)

	msg := iso8583.NewMessage(spec)
	msg.MTI("0200")
	require.NoError(t, msg.Field(3, "000000"))
	require.NoError(t, msg.Field(7, "0412232900"))
	require.NoError(t, msg.Field(11, "000001"))
	require.NoError(t, msg.Field(2, "4111111111111111"))
	packed, err := msg.Pack()
	require.NoError(t, err)
	bad := packed[:len(packed)-8]

	var stream []byte
	const failures = MaxUnparsableSamples + 5
	for range failures {
		stream = append(stream, frameBinary2(bad)...)
	}

	collector := &UnparsableCollector{}
	_, unparsed, err := a.ExtractMessagesFromStreamSampled(stream, collector, "binary2")
	require.NoError(t, err)
	assert.Equal(t, failures, unparsed, "the extractor returns the true failure count")
	assert.Len(t, collector.Samples(), MaxUnparsableSamples, "samples stop at the cap")
}
