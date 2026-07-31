package command

import (
	"fmt"
	"strings"

	moovconnection "github.com/moov-io/iso8583-connection"
)

func isRetriableError(err error) bool {
	if err == nil {
		return false
	}
	if err == moovconnection.ErrConnectionClosed {
		return true
	}

	errStr := err.Error()

	// Permanent errors - don't retry
	permanentErrors := []string{
		"message validation failed",
		"MTI field",
		"required field",
		"field error",
		"invalid",
		"authentication failed",
		"authorization failed",
		"unauthorized",
		"forbidden",
	}

	for _, permErr := range permanentErrors {
		if strings.Contains(strings.ToLower(errStr), permErr) {
			return false
		}
	}

	// Connection closed is permanent
	if err == moovconnection.ErrConnectionClosed {
		return false
	}

	// Network-related errors are retriable
	retriableErrors := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"network is unreachable",
		"no such host",
		"temporary failure",
		"server unavailable",
		"service unavailable",
		"internal server error", // Sometimes temporary
		"bad gateway",           // Network issue
		"gateway timeout",
	}

	for _, retErr := range retriableErrors {
		if strings.Contains(strings.ToLower(errStr), retErr) {
			return true
		}
	}

	// Default: assume retriable for unknown errors (safer to retry)
	return true
}

func hexDump(data []byte) string {
	var buf strings.Builder
	for i := 0; i < len(data); i += 16 {
		// offset
		fmt.Fprintf(&buf, "%08x  ", i)
		// hex bytes
		for j := 0; j < 16; j++ {
			if i+j < len(data) {
				fmt.Fprintf(&buf, "%02x ", data[i+j])
			} else {
				buf.WriteString("   ")
			}
			if j == 7 {
				buf.WriteString(" ")
			}
		}
		buf.WriteString(" |")
		// ASCII
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 32 && b <= 126 {
				buf.WriteByte(b)
			} else {
				buf.WriteByte('.')
			}
		}
		buf.WriteString("|\n")
	}
	return buf.String()
}
