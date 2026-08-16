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
	DirectionRequest MessageDirection = iota
	DirectionResponse
	DirectionUnknown
)

func (d MessageDirection) String() string {
	switch d {
	case DirectionRequest:
		return "Request"
	case DirectionResponse:
		return "Response"
	default:
		return "Unknown"
	}
}

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
func (a *StreamAnalyzer) ExtractAnnotatedMessagesFromFile(
	filePath string,
	headerType string,
	serverPort ...uint16,
) ([]*AnnotatedMessage, error) {
	targetServerPort := uint16(0)
	if len(serverPort) > 0 {
		targetServerPort = serverPort[0]
	}

	if targetServerPort == 0 {
		dirs, err := InspectPCAPDirections(filePath)
		if err == nil && len(dirs) > 0 {
			// Select first server port found from inspected traffic
			for _, d := range dirs {
				if d.TargetPort > 0 {
					targetServerPort = d.TargetPort
					break
				}
			}
		}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open capture file '%s': %w", filePath, err)
	}
	defer f.Close()

	headerBuf := make([]byte, 24)
	n, _ := io.ReadFull(f, headerBuf)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek error: %w", err)
	}

	if n < 4 || !IsPCAPFile(headerBuf[:n]) {
		// Fallback for raw byte stream file
		msgs, err := a.ExtractMessagesFromReader(f, headerType)
		if err != nil {
			return nil, err
		}
		var annotated []*AnnotatedMessage
		for i, msg := range msgs {
			annotated = append(annotated, &AnnotatedMessage{
				Message:   msg,
				Direction: DirectionUnknown,
				Order:     i,
			})
		}
		return annotated, nil
	}

	type streamEndpoint struct {
		srcPort uint16
		dstPort uint16
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

	magicBE := binary.BigEndian.Uint32(headerBuf[0:4])
	if magicBE == pcapngSectionHeader {
		_ = parsePCAPNGPackets(f, collector)
	} else {
		_ = parsePCAPPackets(f, collector)
	}

	var result []*AnnotatedMessage
	order := 0

	if targetServerPort > 0 {
		var reqBuf, respBuf bytes.Buffer
		var lastReqSrc, lastReqDst uint16
		var lastRespSrc, lastRespDst uint16

		for _, ep := range streamOrder {
			buf := streamBuffers[ep]
			if ep.dstPort == targetServerPort {
				reqBuf.Write(buf.Bytes())
				lastReqSrc = ep.srcPort
				lastReqDst = ep.dstPort
			} else if ep.srcPort == targetServerPort {
				respBuf.Write(buf.Bytes())
				lastRespSrc = ep.srcPort
				lastRespDst = ep.dstPort
			} else {
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

		for _, m := range reqMsgs {
			result = append(result, &AnnotatedMessage{
				Message:   m,
				Direction: DirectionRequest,
				Order:     order,
				SrcPort:   lastReqSrc,
				DstPort:   lastReqDst,
			})
			order++
		}

		for _, m := range respMsgs {
			result = append(result, &AnnotatedMessage{
				Message:   m,
				Direction: DirectionResponse,
				Order:     order,
				SrcPort:   lastRespSrc,
				DstPort:   lastRespDst,
			})
			order++
		}
	} else {
		for _, ep := range streamOrder {
			buf := streamBuffers[ep]
			msgs, _ := a.ExtractMessagesFromStream(buf.Bytes(), headerType)
			for _, m := range msgs {
				mti, _ := m.GetMTI()
				dir := DirectionRequest
				if utils.IsResponseMTI(mti) {
					dir = DirectionResponse
				}
				result = append(result, &AnnotatedMessage{
					Message:   m,
					Direction: dir,
					Order:     order,
					SrcPort:   ep.srcPort,
					DstPort:   ep.dstPort,
				})
				order++
			}
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid ISO8583 messages extracted from '%s' with header '%s'", filePath, headerType)
	}

	return result, nil
}
