package utils

import (
	"bytes"
	"encoding/hex"
	"io"
	"testing"
)

func TestParseStationID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expectedHex string
	}{
		{
			name:        "valid decimal station ID",
			input:       "123456",
			expectError: false,
			expectedHex: "123456",
		},
		{
			name:        "valid hex station ID",
			input:       "000001",
			expectError: false,
			expectedHex: "000001",
		},
		{
			name:        "valid letters hex station ID",
			input:       "ABCDEF",
			expectError: false,
			expectedHex: "abcdef",
		},
		{
			name:        "too short station ID",
			input:       "12345",
			expectError: true,
		},
		{
			name:        "too long station ID",
			input:       "1234567",
			expectError: true,
		},
		{
			name:        "non-hex station ID",
			input:       "12345G",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ParseStationID(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				actualHex := hex.EncodeToString(res[:])
				if actualHex != tt.expectedHex {
					t.Errorf("expected hex %s, got %s", tt.expectedHex, actualHex)
				}
			}
		})
	}
}

func TestVisaHeader_WriteTo(t *testing.T) {
	vh, err := NewVisaHeader("123456")
	if err != nil {
		t.Fatalf("failed to create VisaHeader: %v", err)
	}

	vh.SetLength(10) // 10 bytes payload
	if vh.Length() != 10 {
		t.Errorf("expected length 10, got %d", vh.Length())
	}

	var buf bytes.Buffer
	n, err := vh.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	if n != 26 {
		t.Errorf("expected 26 bytes written, got %d", n)
	}

	resBytes := buf.Bytes()
	// TCP header: 2 bytes length (22 + 10 = 32 = 0x0020), 2 bytes marker (0x00, 0x00)
	expectedTCPHeader := []byte{0x00, 0x20, 0x00, 0x00}
	if !bytes.Equal(resBytes[0:4], expectedTCPHeader) {
		t.Errorf("expected TCP header %x, got %x", expectedTCPHeader, resBytes[0:4])
	}

	// VisaNet header:
	// byte 4: Header Length (22 = 0x16)
	if resBytes[4] != 0x16 {
		t.Errorf("expected header length 22 (0x16), got %d (0x%x)", resBytes[4], resBytes[4])
	}
	// byte 5: Header Flag (0x01)
	if resBytes[5] != 0x01 {
		t.Errorf("expected header flag 0x01, got 0x%x", resBytes[5])
	}
	// byte 6: Text Format (0x02)
	if resBytes[6] != 0x02 {
		t.Errorf("expected text format 0x02, got 0x%x", resBytes[6])
	}
	// bytes 7-8: Total Message Length (32 = 0x0020)
	expectedTotalMsgLen := []byte{0x00, 0x20}
	if !bytes.Equal(resBytes[7:9], expectedTotalMsgLen) {
		t.Errorf("expected TotalMessageLength %x, got %x", expectedTotalMsgLen, resBytes[7:9])
	}
	// bytes 9-11: Destination Station (0x00, 0x00, 0x00)
	expectedDestStation := []byte{0x00, 0x00, 0x00}
	if !bytes.Equal(resBytes[9:12], expectedDestStation) {
		t.Errorf("expected DestinationStation %x, got %x", expectedDestStation, resBytes[9:12])
	}
	// bytes 12-14: Source Station (0x12, 0x34, 0x56)
	expectedSrcStation := []byte{0x12, 0x34, 0x56}
	if !bytes.Equal(resBytes[12:15], expectedSrcStation) {
		t.Errorf("expected SourceStation %x, got %x", expectedSrcStation, resBytes[12:15])
	}
}

func TestVisaHeader_ReadFrom(t *testing.T) {
	// TCP header: 2 bytes length (22 + 15 = 37 = 0x0025), 2 bytes marker (0x00, 0x00)
	// VisaNet header: length 22 (0x16), and rest zeroes
	inputHex := "0025000016010200250000001234560000000000000000000000"
	inputBytes, _ := hex.DecodeString(inputHex)

	vh, _ := NewVisaHeader("000000")
	reader := bytes.NewReader(inputBytes)
	n, err := vh.ReadFrom(reader)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}

	if n != 26 {
		t.Errorf("expected 26 bytes read, got %d", n)
	}

	// Payload len is 37, header length is 22. Expected ISO message length = 15.
	if vh.Length() != 15 {
		t.Errorf("expected message length 15, got %d", vh.Length())
	}
}

func TestVisaHeader_ReadFrom_ExtraHeaderBytes(t *testing.T) {
	// Header length is 26 (0x1A).
	// TCP header: length 26 + 10 = 36 = 0x0024, marker 0x0000
	// VisaNet header: length 26, flag 0x01, format 0x02, etc. (total 26 bytes)
	// 4 bytes TCP + 26 bytes VisaHeader
	inputHex := "002400001a010200240000001234560000000000000000000000aa11bb22"
	inputBytes, _ := hex.DecodeString(inputHex)

	vh, _ := NewVisaHeader("000000")
	reader := bytes.NewReader(inputBytes)
	n, err := vh.ReadFrom(reader)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}

	if n != 30 { // 4 TCP + 26 VISA
		t.Errorf("expected 30 bytes read, got %d", n)
	}

	if vh.Length() != 10 {
		t.Errorf("expected message length 10, got %d", vh.Length())
	}
}

func TestVisaHeader_ReadFrom_Errors(t *testing.T) {
	vh, _ := NewVisaHeader("000000")

	// 1. Short TCP header
	_, err := vh.ReadFrom(bytes.NewReader([]byte{0x00, 0x20}))
	if err == nil || err == io.EOF {
		t.Errorf("expected error for short TCP header, got %v", err)
	}

	// 2. Invalid marker
	_, err = vh.ReadFrom(bytes.NewReader([]byte{0x00, 0x20, 0x01, 0x00}))
	if err == nil {
		t.Errorf("expected error for invalid marker")
	}

	// 3. Short payload length (< 22)
	_, err = vh.ReadFrom(bytes.NewReader([]byte{0x00, 0x10, 0x00, 0x00}))
	if err == nil {
		t.Errorf("expected error for short payload length")
	}

	// 4. Short VISA header read
	inputBytes, _ := hex.DecodeString("0020000016010200")
	_, err = vh.ReadFrom(bytes.NewReader(inputBytes))
	if err == nil {
		t.Errorf("expected error for short VISA header")
	}

	// 5. Invalid VISA header length (< 22)
	inputBytes2, _ := hex.DecodeString("00200000100102002000000012345600000000000000000000")
	_, err = vh.ReadFrom(bytes.NewReader(inputBytes2))
	if err == nil {
		t.Errorf("expected error for invalid header length (< 22)")
	}
}
