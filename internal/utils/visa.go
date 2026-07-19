package utils

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
)

type VisaHeader struct {
	mu           sync.RWMutex
	length       int
	stationID    [3]byte
	rawStationID string
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
		return bytes, fmt.Errorf("visa station ID must be exactly 6 characters long")
	}
	decoded, err := hex.DecodeString(idStr)
	if err != nil {
		return bytes, fmt.Errorf("invalid visa station ID: must be a 6-digit hex/decimal string: %w", err)
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

// WriteTo writes:
// 1. TCP Header (4 bytes):
//    - 2 bytes payload length (BigEndian uint16) = 22 + message length
//    - 2 bytes marker (0x00, 0x00)
// 2. VisaNet Header (22 bytes):
//    - Header Length (22): 0x16
//    - Header Flag: 0x01
//    - Text Format: 0x02
//    - Total Message Length (2 bytes BigEndian uint16) = 22 + message length
//    - Destination Station (3 bytes): 0x00, 0x00, 0x00
//    - Source Station (3 bytes): h.stationID
//    - Round Trip Control: 0x00
//    - VIP Flags (2 bytes): 0x00, 0x00
//    - Message Status Flags (3 bytes): 0x00, 0x00, 0x00
//    - Batch Number: 0x00
//    - Reserved (3 bytes): 0x00, 0x00, 0x00
//    - User Information: 0x00
func (h *VisaHeader) WriteTo(w io.Writer) (int, error) {
	h.mu.RLock()
	length := h.length
	stationID := h.stationID
	h.mu.RUnlock()

	payloadLen := 22 + length

	// 4 bytes TCP Header + 22 bytes VisaNet Header
	buf := make([]byte, 26)

	// TCP Header
	binary.BigEndian.PutUint16(buf[0:2], uint16(payloadLen))
	buf[2] = 0x00
	buf[3] = 0x00

	// VisaNet Header
	buf[4] = 22                         // Header Length
	buf[5] = 0x01                       // Header Flag
	buf[6] = 0x02                       // Text Format
	binary.BigEndian.PutUint16(buf[7:9], uint16(payloadLen)) // Total Message Length
	// Destination Station (buf[9:12] are 0x00)
	copy(buf[12:15], stationID[:])     // Source Station
	// Round Trip Control (buf[15] is 0x00)
	// VIP Flags (buf[16:18] are 0x00)
	// Message Status Flags (buf[18:21] are 0x00)
	// Batch Number (buf[21] is 0x00)
	// Reserved (buf[22:25] are 0x00)
	// User Information (buf[25] is 0x00)

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

	if tcpHeader[2] != 0x00 || tcpHeader[3] != 0x00 {
		return n, fmt.Errorf("invalid Visa frame marker bytes: %02X %02X", tcpHeader[2], tcpHeader[3])
	}

	payloadLen := int(binary.BigEndian.Uint16(tcpHeader[0:2]))
	if payloadLen < 22 {
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
	h.mu.Unlock()

	return n, nil
}
