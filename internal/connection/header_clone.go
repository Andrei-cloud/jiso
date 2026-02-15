package connection

import (
	"jiso/internal/utils"

	"github.com/moov-io/iso8583/network"
)

// cloneHeader creates a copy of the header to prevent race conditions
// when the same header instance is used for both reading and writing concurrently.
func cloneHeader(h network.Header) network.Header {
	if h == nil {
		return nil
	}

	// Try to handle known types
	switch h.(type) {
	case *utils.Binary2BytesAdapter:
		return utils.NewBinary2BytesAdapter()
	case *network.ASCII4BytesHeader:
		return network.NewASCII4BytesHeader()
	case *network.BCD2BytesHeader:
		return network.NewBCD2BytesHeader()
	}

	// For unknown types, we return the original.
	// This might still race if the implementation is not thread-safe.
	// Users should use known types or provide thread-safe implementations.
	return h
}
