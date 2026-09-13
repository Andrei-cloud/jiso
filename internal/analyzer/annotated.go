package analyzer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

// MessageDirection indicates whether a message is a request or response
type MessageDirection int

const (
	// DirectionRequest is a message that went out from us.
	DirectionRequest MessageDirection = iota
	// DirectionResponse is the reply that came back for a request.
	DirectionResponse
	// DirectionUnknown is a message the analyzer could not classify from its MTI,
	// kept as its own value so unclassified traffic stays visible instead of being
	// counted as one of the two directions it knows.
	DirectionUnknown
)

// AnnotatedMessage wraps an iso8583.Message with directional and ordering metadata
type AnnotatedMessage struct {
	Message   *iso8583.Message
	Direction MessageDirection
	Order     int
	SrcPort   uint16
	DstPort   uint16
}

// ExtractAnnotatedMessagesFromFile extracts ISO8583 messages from a PCAP/PCAPNG file,
// tagging each message with DirectionRequest or DirectionResponse based on serverPort.
// If serverPort is 0, it auto-detects the server port using InspectPCAPDirections.
// streamEndpoint keys the per-direction byte buffers built while walking a
// capture's packets.
type streamEndpoint struct {
	srcPort uint16
	dstPort uint16
}

// ExtractAnnotatedMessagesFromFile extracts ISO8583 messages from a PCAP/PCAPNG file,
// tagging each message with DirectionRequest or DirectionResponse based on serverPort.
// If serverPort is 0, it auto-detects the server port using InspectPCAPDirections.
func (a *StreamAnalyzer) ExtractAnnotatedMessagesFromFile(
	filePath string,
	headerType string,
	serverPort ...uint16,
) ([]*AnnotatedMessage, error) {
	targetServerPort := resolveServerPort(filePath, serverPort)

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open capture file '%s': %w", filePath, err)
	}
	defer func() { _ = f.Close() }() // read-only handle; nothing to report on close

	headerBuf := make([]byte, 24)
	n, _ := io.ReadFull(f, headerBuf)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek error: %w", err)
	}
	if n < 4 || !IsPCAPFile(headerBuf[:n]) {
		return a.annotateRawStream(f, headerType)
	}

	var streamOrder []streamEndpoint
	streamBuffers := make(map[streamEndpoint]*bytes.Buffer)
	collector := func(srcPort, dstPort uint16, payload []byte) {
		if len(payload) == 0 {
			return
		}
		ep := streamEndpoint{srcPort: srcPort, dstPort: dstPort}
		if streamBuffers[ep] == nil {
			streamBuffers[ep] = &bytes.Buffer{}
			streamOrder = append(streamOrder, ep)
		}
		streamBuffers[ep].Write(payload)
	}

	if binary.BigEndian.Uint32(headerBuf[0:4]) == pcapngSectionHeader {
		_ = parsePCAPNGPackets(f, collector)
	} else {
		_ = parsePCAPPackets(f, collector)
	}

	var result []*AnnotatedMessage
	if targetServerPort > 0 {
		result, err = a.annotateSplitStreams(headerType, streamOrder, streamBuffers, targetServerPort)
		if err != nil {
			return nil, err
		}
	} else {
		result = a.annotateByMTI(headerType, streamOrder, streamBuffers)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid ISO8583 messages extracted from '%s' with header '%s'", filePath, headerType)
	}

	return result, nil
}

// resolveServerPort returns the explicit server port, or the first target port
// seen in the capture's inspected directions when none was supplied.
func resolveServerPort(filePath string, serverPort []uint16) uint16 {
	if len(serverPort) > 0 {
		return serverPort[0]
	}
	dirs, err := InspectPCAPDirections(filePath)
	if err != nil {
		return 0
	}
	for _, d := range dirs {
		if d.TargetPort > 0 {
			return d.TargetPort
		}
	}

	return 0
}

// annotateRawStream unpacks a non-PCAP byte stream into direction-unknown
// annotated messages.
func (a *StreamAnalyzer) annotateRawStream(f io.Reader, headerType string) ([]*AnnotatedMessage, error) {
	msgs, err := a.ExtractMessagesFromReader(f, headerType)
	if err != nil {
		return nil, err
	}
	annotated := make([]*AnnotatedMessage, 0, len(msgs))
	for i, msg := range msgs {
		annotated = append(annotated, &AnnotatedMessage{Message: msg, Direction: DirectionUnknown, Order: i})
	}

	return annotated, nil
}

// annotateSplitStreams routes each stream's bytes to the request or response
// buffer by the server port, unpacks both, and tags them request/response.
func (a *StreamAnalyzer) annotateSplitStreams(headerType string, streamOrder []streamEndpoint, streamBuffers map[streamEndpoint]*bytes.Buffer, targetServerPort uint16) ([]*AnnotatedMessage, error) {
	var reqBuf, respBuf bytes.Buffer
	var lastReqSrc, lastReqDst uint16
	var lastRespSrc, lastRespDst uint16

	for _, ep := range streamOrder {
		buf := streamBuffers[ep]
		switch {
		case ep.dstPort == targetServerPort:
			reqBuf.Write(buf.Bytes())
			lastReqSrc = ep.srcPort
			lastReqDst = ep.dstPort
		case ep.srcPort == targetServerPort:
			respBuf.Write(buf.Bytes())
			lastRespSrc = ep.srcPort
			lastRespDst = ep.dstPort
		default:
			reqBuf.Write(buf.Bytes())
		}
	}

	reqMsgs, reqErr := a.ExtractMessagesFromStream(reqBuf.Bytes(), headerType)
	respMsgs, respErr := a.ExtractMessagesFromStream(respBuf.Bytes(), headerType)
	if reqErr != nil && len(reqMsgs) == 0 && respErr != nil && len(respMsgs) == 0 {
		if reqErr != nil {
			return nil, fmt.Errorf("extracting request messages: %w", reqErr)
		}

		return nil, fmt.Errorf("extracting response messages: %w", respErr)
	}

	result := make([]*AnnotatedMessage, 0, len(reqMsgs)+len(respMsgs))
	order := 0
	for _, m := range reqMsgs {
		result = append(result, &AnnotatedMessage{Message: m, Direction: DirectionRequest, Order: order, SrcPort: lastReqSrc, DstPort: lastReqDst})
		order++
	}
	for _, m := range respMsgs {
		result = append(result, &AnnotatedMessage{Message: m, Direction: DirectionResponse, Order: order, SrcPort: lastRespSrc, DstPort: lastRespDst})
		order++
	}

	return result, nil
}

// annotateByMTI unpacks each stream and tags messages request/response by their
// own MTI, used when no server port is known to split the streams.
func (a *StreamAnalyzer) annotateByMTI(headerType string, streamOrder []streamEndpoint, streamBuffers map[streamEndpoint]*bytes.Buffer) []*AnnotatedMessage {
	var result []*AnnotatedMessage
	order := 0
	for _, ep := range streamOrder {
		buf := streamBuffers[ep]
		msgs, _ := a.ExtractMessagesFromStream(buf.Bytes(), headerType)
		for _, m := range msgs {
			dir := DirectionRequest
			if utils.IsResponseMTI(getFieldMTI(m)) {
				dir = DirectionResponse
			}
			result = append(result, &AnnotatedMessage{Message: m, Direction: dir, Order: order, SrcPort: ep.srcPort, DstPort: ep.dstPort})
			order++
		}
	}

	return result
}
