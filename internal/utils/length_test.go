package utils

import (
	"bytes"
	"testing"

	"github.com/moov-io/iso8583/network"
)

func TestReadMessageLengthWrapper(t *testing.T) {
	tests := []struct {
		name        string
		header      network.Header
		input       []byte
		expectError bool
		expectedLen int
		errorMsg    string
	}{
		{
			name:        "valid binary2 header",
			header:      NewBinary2BytesAdapter(),
			input:       []byte{0x00, 0x40}, // 64 bytes
			expectError: false,
			expectedLen: 64,
		},
		{
			name:        "too large message",
			header:      NewBinary2BytesAdapter(),
			input:       []byte{0xFF, 0xFF}, // 65535 bytes (larger than MaxMessageSize of 32KB)
			expectError: true,
			errorMsg:    "exceeds maximum allowed size",
		},
		{
			name:        "too small message",
			header:      NewBinary2BytesAdapter(),
			input:       []byte{0x00, 0x0A}, // 10 bytes (smaller than minimum 20)
			expectError: true,
			errorMsg:    "too small for a valid ISO8583 message",
		},
		{
			name:        "valid ascii4 header",
			header:      network.NewASCII4BytesHeader(),
			input:       []byte("0128"), // 128 bytes
			expectError: false,
			expectedLen: 128,
		},
		{
			name:        "valid bcd2 header",
			header:      network.NewBCD2BytesHeader(),
			input:       []byte{0x01, 0x28}, // 128 bytes
			expectError: false,
			expectedLen: 128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := ReadMessageLengthWrapper(tt.header)

			buf := bytes.NewReader(tt.input)
			length, err := reader(buf)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !bytes.Contains([]byte(err.Error()), []byte(tt.errorMsg)) {
					t.Errorf("expected error message to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if length != tt.expectedLen {
					t.Errorf("expected length %d, got %d", tt.expectedLen, length)
				}
			}
		})
	}
}
