package analyzer

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

// CapturedFlow represents an aggregated transaction flow captured from network traffic
type CapturedFlow struct {
	MTI      string
	DE3      string
	DE22     string
	Messages []*iso8583.Message
	Count    int
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

// ExtractMessagesFromReader extracts framed ISO8583 messages from an io.Reader using the specified header type
func (a *StreamAnalyzer) ExtractMessagesFromReader(r io.Reader, headerType string) ([]*iso8583.Message, error) {
	if headerType == "" {
		headerType = "binary2"
	}
	hdr, err := utils.SelectLength(headerType)
	if err != nil {
		return nil, fmt.Errorf("invalid header type '%s': %w", headerType, err)
	}

	readLenFunc := utils.ReadMessageLengthWrapper(hdr)
	if headerType == "NAPS" {
		readLenFunc = utils.NapsReadLengthWrapper(readLenFunc)
	}

	var messages []*iso8583.Message

	for {
		msgLen, err := readLenFunc(r)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			// End of readable stream or unparseable framing
			break
		}

		if msgLen <= 0 || msgLen > utils.MaxMessageSize {
			break
		}

		payload := make([]byte, msgLen)
		_, err = io.ReadFull(r, payload)
		if err != nil {
			break
		}

		msg := iso8583.NewMessage(a.spec)
		if err := msg.Unpack(payload); err == nil {
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

// ExtractMessagesFromStream extracts ISO8583 messages from a byte slice using the given header type
func (a *StreamAnalyzer) ExtractMessagesFromStream(streamData []byte, headerType ...string) ([]*iso8583.Message, error) {
	hType := "binary2"
	if len(headerType) > 0 && headerType[0] != "" {
		hType = headerType[0]
	}
	return a.ExtractMessagesFromReader(bytes.NewReader(streamData), hType)
}

// AggregateFlows groups extracted messages by MTI + DE3 (Processing Code) + DE22 (POS Entry Mode)
func (a *StreamAnalyzer) AggregateFlows(messages []*iso8583.Message) map[string]*CapturedFlow {
	flows := make(map[string]*CapturedFlow)

	for _, msg := range messages {
		mti, _ := msg.GetMTI()
		de3 := ""
		if f := msg.GetField(3); f != nil {
			de3, _ = f.String()
		}
		de22 := ""
		if f := msg.GetField(22); f != nil {
			de22, _ = f.String()
		}

		var key string
		if de22 != "" {
			key = fmt.Sprintf("%s_%s_%s", mti, de3, de22)
		} else {
			key = fmt.Sprintf("%s_%s", mti, de3)
		}

		flow, exists := flows[key]
		if !exists {
			flow = &CapturedFlow{
				MTI:      mti,
				DE3:      de3,
				DE22:     de22,
				Messages: make([]*iso8583.Message, 0),
			}
			flows[key] = flow
		}

		flow.Messages = append(flow.Messages, msg)
		flow.Count++
	}

	return flows
}
