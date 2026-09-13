package utils

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/moov-io/iso8583/encoding"
)

// sessionControlIndicator is the byte that marks a message as session-control
// rather than a transaction, and MaxMessageLength caps what the header will
// declare, because an ISO8583 message is a few kilobytes and a larger declared
// length means the framing is already wrong.
const (
	sessionControlIndicator = byte('2')
	MaxMessageLength        = 2048
)

// VisaHeader is the VISA length header: the station ID the operator configures
// plus the message length, both under one mutex because a connection's reader
// and writer share the header.
type VisaHeader struct {
	mu               sync.RWMutex
	length           int
	stationID        [3]byte
	rawStationID     string
	isSessionControl bool
}

// NewVisaHeader builds a header for a 6-digit station ID. An ID that is not six
// numeric digits is refused here rather than turned into bytes that would frame
// every message wrongly.
func NewVisaHeader(stationIDStr string) (*VisaHeader, error) {
	parsedID, err := ParseStationID(stationIDStr)
	if err != nil {
		return nil, err
	}
	return &VisaHeader{
		stationID:    parsedID,
		rawStationID: stationIDStr,
	}, nil
}

// ParseStationID turns the operator's 6-digit station ID into the 3 bytes that
// go on the wire. The digits are packed two to a byte, which is why exactly six
// are required: a 5-digit ID would otherwise be silently padded into a value the
// acquirer reads as a different station.
func ParseStationID(idStr string) ([3]byte, error) {
	var bytes [3]byte
	if len(idStr) != 6 {
		return bytes, fmt.Errorf("visa station ID must be exactly 6 numeric digits long")
	}
	for _, ch := range idStr {
		if ch < '0' || ch > '9' {
			return bytes, fmt.Errorf("visa station ID must contain only numeric digits (0-9)")
		}
	}
	decoded, err := hex.DecodeString(idStr)
	if err != nil {
		return bytes, fmt.Errorf("invalid visa station ID: %w", err)
	}
	copy(bytes[:], decoded)
	return bytes, nil
}

// SetLength records the length the next WriteTo declares.
func (h *VisaHeader) SetLength(length int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.length = length
}

// Length returns the declared length, 0 before the first message.
func (h *VisaHeader) Length() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.length
}

// RawStationID returns the station ID as the operator typed it, the six-digit
// string rather than the packed bytes, because that is what a settings page
// re-displays.
func (h *VisaHeader) RawStationID() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rawStationID
}

// IsSessionControl reports whether the header currently marks messages as
// session-control messages.
func (h *VisaHeader) IsSessionControl() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isSessionControl
}

// SetSessionControl toggles the indicator byte the acquirer sees, which is how
// one connection sends session messages and transaction messages with the same
// station ID.
func (h *VisaHeader) SetSessionControl(isSessionControl bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isSessionControl = isSessionControl
}

// WriteTo frames the station ID and the declared length ahead of the message.
// Its (int, error) result comes from moov-io's network.Header, not from
// io.WriterTo, which asks for (int64, error); conforming to that would break the
// header contract the connection layer codes against.
//
//nolint:govet // these implement moov-io network.Header, whose contract is
func (h *VisaHeader) WriteTo(w io.Writer) (int, error) {
	h.mu.RLock()
	length := h.length
	stationID := h.stationID
	isSessionControl := h.isSessionControl
	h.mu.RUnlock()

	payloadLen := 22 + length
	if isSessionControl && length < 22 {
		payloadLen = length
	}

	if payloadLen > MaxMessageLength {
		return 0, fmt.Errorf("length %d exceeds max length %d", payloadLen, MaxMessageLength)
	}

	if isSessionControl && length < 22 {
		buf := make([]byte, 4+length)
		binary.BigEndian.PutUint16(buf[0:2], uint16(payloadLen))
		buf[2] = 0x00
		buf[3] = 0x20 // BCD indicator for session control '2'
		n, err := w.Write(buf)
		return n, err
	}

	// 4 bytes TCP Header + 22 bytes VisaNet Header
	var buf [26]byte

	// TCP Header
	binary.BigEndian.PutUint16(buf[0:2], uint16(payloadLen))
	buf[2] = 0x00
	if isSessionControl {
		buf[3] = 0x20
	} else {
		buf[3] = 0x00
	}

	// VisaNet Header
	buf[4] = 22                                              // Header Length
	buf[5] = 0x01                                            // Header Flag
	buf[6] = 0x02                                            // Text Format
	binary.BigEndian.PutUint16(buf[7:9], uint16(payloadLen)) // Total Message Length

	copy(buf[12:15], stationID[:]) // Source Station

	n, err := w.Write(buf[:])
	return n, err
}

// ReadFrom reads the station ID and length prefix back and records the length it
// declares. Like WriteTo it returns (int, error) for moov-io's network.Header
// contract rather than io.ReaderFrom's.
//
//nolint:govet // these implement moov-io network.Header, whose contract is
func (h *VisaHeader) ReadFrom(r io.Reader) (int, error) {
	// Read 4 bytes TCP Header
	var tcpHeader [4]byte
	n, err := io.ReadFull(r, tcpHeader[:])
	if err != nil {
		return n, fmt.Errorf("reading TCP header: %w", err)
	}

	payloadLen := int(binary.BigEndian.Uint16(tcpHeader[0:2]))
	if payloadLen > MaxMessageLength {
		return n, fmt.Errorf("length %d exceeds max length %d", payloadLen, MaxMessageLength)
	}

	// Decode message format and platform indicators
	indicators, _, err := encoding.BCD.Decode(tcpHeader[3:], 2)
	isSessionCtrl := false
	if err == nil && len(indicators) > 0 {
		isSessionCtrl = (indicators[0] == sessionControlIndicator)
	}

	if payloadLen < 22 {
		if isSessionCtrl || payloadLen == 0 {
			h.mu.Lock()
			h.length = payloadLen
			h.isSessionControl = isSessionCtrl
			h.mu.Unlock()
			return n, nil
		}
		return n, fmt.Errorf("invalid VISA payload length: %d (must be at least 22 bytes)", payloadLen)
	}

	// Read VisaNet Header (22 bytes)
	var visaHeader [22]byte
	n2, err := io.ReadFull(r, visaHeader[:])
	n += n2
	if err != nil {
		return n, fmt.Errorf("reading VISA message header: %w", err)
	}

	headerLength := int(visaHeader[0])
	if headerLength < 22 {
		return n, fmt.Errorf("invalid VISA header length: %d", headerLength)
	}

	// If header is longer than 22 bytes, read the remaining bytes of the header
	if headerLength > 22 {
		extraLen := headerLength - 22
		extraBuf := make([]byte, extraLen)
		n3, err := io.ReadFull(r, extraBuf)
		n += n3
		if err != nil {
			return n, fmt.Errorf("reading extra VISA message header bytes: %w", err)
		}
	}

	h.mu.Lock()
	h.length = payloadLen - headerLength
	h.isSessionControl = isSessionCtrl
	h.mu.Unlock()

	return n, nil
}
