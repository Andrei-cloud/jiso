package utils

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/moov-io/iso8583/encoding"
)

const (
	sessionControlIndicator = byte('2')
	MaxMessageLength        = 2048
)

type VisaHeader struct {
	mu               sync.RWMutex
	length           int
	stationID        [3]byte
	rawStationID     string
	isSessionControl bool
}

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

func (h *VisaHeader) SetLength(length int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.length = length
}

func (h *VisaHeader) Length() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.length
}

func (h *VisaHeader) RawStationID() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rawStationID
}

func (h *VisaHeader) IsSessionControl() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isSessionControl
}

func (h *VisaHeader) SetSessionControl(isSessionControl bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isSessionControl = isSessionControl
}

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
	buf := make([]byte, 26)

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

	n, err := w.Write(buf)
	return n, err
}

func (h *VisaHeader) ReadFrom(r io.Reader) (int, error) {
	// Read 4 bytes TCP Header
	tcpHeader := make([]byte, 4)
	n, err := io.ReadFull(r, tcpHeader)
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
	visaHeader := make([]byte, 22)
	n2, err := io.ReadFull(r, visaHeader)
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
