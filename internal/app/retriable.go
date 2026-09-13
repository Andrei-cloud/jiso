package app

import (
	"errors"
	"strings"

	moovconnection "github.com/moov-io/iso8583-connection"
)

// IsRetriableError classifies a send error for networking-stats recording.
// Moved from the legacy REPL send path so App.Send and the command-layer
// background senders share one implementation.
func IsRetriableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, moovconnection.ErrConnectionClosed) {
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
	if errors.Is(err, moovconnection.ErrConnectionClosed) {
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
