package analyzer

import (
	"encoding/binary"
	"fmt"

	"github.com/moov-io/iso8583"
)

// CapturedFlow represents an aggregated transaction flow captured from network traffic
type CapturedFlow struct {
	MTI        string
	DE3        string
	Messages   []*iso8583.Message
	Count      int
}

// StreamAnalyzer extracts and aggregates ISO8583 messages from raw byte streams
type StreamAnalyzer struct {
	spec *iso8583.MessageSpec
}

// NewStreamAnalyzer creates a new StreamAnalyzer with an ISO8583 message spec
func NewStreamAnalyzer(spec *iso8583.MessageSpec) *StreamAnalyzer {
	return &StreamAnalyzer{
		spec: spec,
	}
}

// ExtractMessagesFromStream extracts 2-byte length framed ISO8583 messages from a byte stream
func (a *StreamAnalyzer) ExtractMessagesFromStream(streamData []byte) ([]*iso8583.Message, error) {
	var messages []*iso8583.Message
	offset := 0

	for offset+2 <= len(streamData) {
		msgLen := int(binary.BigEndian.Uint16(streamData[offset : offset+2]))
		offset += 2

		if msgLen <= 0 || offset+msgLen > len(streamData) {
			break
		}

		payload := streamData[offset : offset+msgLen]
		offset += msgLen

		msg := iso8583.NewMessage(a.spec)
		if err := msg.Unpack(payload); err == nil {
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

// AggregateFlows groups extracted messages by MTI + DE3 (Processing Code)
func (a *StreamAnalyzer) AggregateFlows(messages []*iso8583.Message) map[string]*CapturedFlow {
	flows := make(map[string]*CapturedFlow)

	for _, msg := range messages {
		mti, _ := msg.GetMTI()
		de3 := ""
		if f := msg.GetField(3); f != nil {
			de3, _ = f.String()
		}

		key := fmt.Sprintf("%s_%s", mti, de3)
		flow, exists := flows[key]
		if !exists {
			flow = &CapturedFlow{
				MTI:      mti,
				DE3:      de3,
				Messages: make([]*iso8583.Message, 0),
			}
			flows[key] = flow
		}

		flow.Messages = append(flow.Messages, msg)
		flow.Count++
	}

	return flows
}
