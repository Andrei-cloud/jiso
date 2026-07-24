package analyzer

import (
	"encoding/binary"
	"testing"

	"jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamAnalyzerAndVarianceEngine(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec_bcp.json")
	require.NoError(t, err)

	msg1 := iso8583.NewMessage(spec)
	msg1.MTI("0200")
	msg1.Field(3, "000000")
	msg1.Field(7, "0412232900")
	msg1.Field(11, "000001")
	msg1.Field(2, "4111111111111111")

	p1, err := msg1.Pack()
	require.NoError(t, err)

	msg2 := iso8583.NewMessage(spec)
	msg2.MTI("0200")
	msg2.Field(3, "000000")
	msg2.Field(7, "0412233000")
	msg2.Field(11, "000002")
	msg2.Field(2, "4222222222222222")

	p2, err := msg2.Pack()
	require.NoError(t, err)

	// Build stream bytes
	stream := make([]byte, 2+len(p1)+2+len(p2))
	binary.BigEndian.PutUint16(stream[0:2], uint16(len(p1)))
	copy(stream[2:], p1)

	offset := 2 + len(p1)
	binary.BigEndian.PutUint16(stream[offset:offset+2], uint16(len(p2)))
	copy(stream[offset+2:], p2)

	analyzer := NewStreamAnalyzer(spec)
	extracted, err := analyzer.ExtractMessagesFromStream(stream)
	require.NoError(t, err)
	assert.Len(t, extracted, 2)

	flows := analyzer.AggregateFlows(extracted)
	require.Len(t, flows, 1)

	flowKey := "0200_000000"
	flow, exists := flows[flowKey]
	require.True(t, exists)
	assert.Equal(t, 2, flow.Count)

	varianceEng := NewVarianceEngine()
	res, err := varianceEng.AnalyzeFlow(flow)
	require.NoError(t, err)

	assert.Equal(t, config.TypeTransaction, res.Transaction.GetType())
	assert.Equal(t, config.TypeDataset, res.Dataset.GetType())
	assert.Len(t, res.Dataset.Data, 2)
	assert.Equal(t, "4111111111111111", res.Dataset.Data[0]["DE_2"])
	assert.Equal(t, "4222222222222222", res.Dataset.Data[1]["DE_2"])
}
