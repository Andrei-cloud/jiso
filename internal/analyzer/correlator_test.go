package analyzer

import (
	"testing"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrelator_BasicPairing(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	reqMsg.Field(3, "000000")
	reqMsg.Field(11, "000001")
	reqMsg.Field(2, "4111111111111111")

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	respMsg.Field(3, "000000")
	respMsg.Field(11, "000001")
	respMsg.Field(39, "00")

	annotated := []*AnnotatedMessage{
		{Message: reqMsg, Direction: DirectionRequest, Order: 0},
		{Message: respMsg, Direction: DirectionResponse, Order: 1},
	}

	correlator := NewCorrelator()
	pairs, err := correlator.Correlate(annotated)
	require.NoError(t, err)
	require.Len(t, pairs, 1)

	assert.NotNil(t, pairs[0].Request)
	assert.NotNil(t, pairs[0].Response)
	assert.Nil(t, pairs[0].Reversal)

	reqMTI, _ := pairs[0].Request.Message.GetMTI()
	respMTI, _ := pairs[0].Response.Message.GetMTI()
	assert.Equal(t, "0200", reqMTI)
	assert.Equal(t, "0210", respMTI)
}

func TestCorrelator_RRNFallback(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	reqMsg.Field(3, "000000")
	reqMsg.Field(37, "987654321012")

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	respMsg.Field(3, "000000")
	respMsg.Field(37, "987654321012")
	respMsg.Field(39, "00")

	annotated := []*AnnotatedMessage{
		{Message: reqMsg, Direction: DirectionRequest, Order: 0},
		{Message: respMsg, Direction: DirectionResponse, Order: 1},
	}

	correlator := NewCorrelator()
	pairs, err := correlator.Correlate(annotated)
	require.NoError(t, err)
	require.Len(t, pairs, 1)
	assert.NotNil(t, pairs[0].Response)
}

func TestCorrelator_ReversalDetection(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	reqMsg.Field(3, "000000")
	reqMsg.Field(11, "000001")
	reqMsg.Field(7, "0412232900")

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	respMsg.Field(3, "000000")
	respMsg.Field(11, "000001")
	respMsg.Field(39, "00")

	revMsg := iso8583.NewMessage(spec)
	revMsg.MTI("0400")
	revMsg.Field(3, "000000")
	revMsg.Field(11, "000002")
	// DE 90 contains original MTI 0200 + STAN 000001 + DateTime 0412232900
	revMsg.Field(90, "02000000010412232900000000000000000000000")

	annotated := []*AnnotatedMessage{
		{Message: reqMsg, Direction: DirectionRequest, Order: 0},
		{Message: respMsg, Direction: DirectionResponse, Order: 1},
		{Message: revMsg, Direction: DirectionRequest, Order: 2},
	}

	correlator := NewCorrelator()
	pairs, err := correlator.Correlate(annotated)
	require.NoError(t, err)
	require.Len(t, pairs, 1)

	assert.NotNil(t, pairs[0].Request)
	assert.NotNil(t, pairs[0].Response)
	assert.NotNil(t, pairs[0].Reversal)
	assert.Contains(t, pairs[0].Label, "Reversal")
}
