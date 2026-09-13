package utils

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	connection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/network"

	"jiso/internal/config"
)

// The NAPS prefix strings some acquirers put in front of the framed message:
// the ATM and POS terminal variants. They are wire content rather than
// configuration, so they live beside the wrappers that write and verify them.
const (
	NAPSPREFIXATM = "ISO016000070"
	NAPSPREFIXPOS = "ISO026000070"

	// MaxMessageSize defines the maximum allowed message size in bytes
	// ISO8583 messages are typically small (a few KB)
	MaxMessageSize = 1024
)

// SelectLength returns the client-side length header an operator named. The
// names are wire-facing vocabulary shared with LengthTypeOptions and
// SelectServerHeader, which is why they are constants: a name offered by the
// connect form but rejected here reads to the operator as a broken tool.
const (
	LengthTypeASCII4  = "ascii4"
	LengthTypeBinary2 = "binary2"
	LengthTypeNAPS    = "naps"
	LengthTypeBinary4 = "binary4"
	LengthTypeBCD2    = "bcd2"
	LengthTypeVisa    = "visa"
)

// SelectLength returns the client-side length header an operator named. The names
// are wire-facing vocabulary shared with LengthTypeOptions (what the connect form
// and the analyze prompt offer) and SelectServerHeader, which is why they are
// constants: a name offered in one place and rejected in another reads to the
// operator as a broken tool, and no test would say which of the three misspelled
// it.
func SelectLength(lenType string) (network.Header, error) {
	switch strings.ToLower(lenType) {
	case LengthTypeASCII4:
		return network.NewASCII4BytesHeader(), nil
	case LengthTypeBinary2, LengthTypeNAPS:
		return NewBinary2BytesAdapter(), nil
	case LengthTypeBinary4:
		return NewBinary4BytesAdapter(), nil
	case LengthTypeBCD2:
		return network.NewBCD2BytesHeader(), nil
	case LengthTypeVisa:
		stationID := config.GetConfig().GetVisaStationID()
		if stationID == "" {
			stationID = "000000" // Default for server role / fallback: station ID all zeros
		}

		return NewVisaHeader(stationID)
	default:
		return nil, fmt.Errorf("unknown length type: %s", lenType)
	}
}

// LengthTypeOptions lists the length header types SelectLength accepts,
// in its own acceptance order (LengthTypeNAPS shares the binary2 codec and is
// offered separately, as the connect prompt does). UI option lists —
// the TUI §J header step and the CLI analyze interactive prompt — must
// source from here so they never offer a type the engine rejects.
func LengthTypeOptions() []string {
	return []string{LengthTypeASCII4, LengthTypeBinary2, LengthTypeNAPS, LengthTypeBinary4, LengthTypeBCD2, LengthTypeVisa}
}

// SelectServerHeader returns the appropriate header for embedded server role
// For VISA header on server role, station ID is set to all zeros ("000000")
func SelectServerHeader(lenType string) (network.Header, error) {
	switch strings.ToLower(lenType) {
	case LengthTypeASCII4:
		return network.NewASCII4BytesHeader(), nil
	case LengthTypeBinary2, LengthTypeNAPS, "":
		return NewBinary2BytesAdapter(), nil
	case LengthTypeBinary4:
		return NewBinary4BytesAdapter(), nil
	case LengthTypeBCD2:
		return network.NewBCD2BytesHeader(), nil
	case LengthTypeVisa:
		return NewVisaHeader("000000")
	default:
		return nil, fmt.Errorf("unknown server length type: %s", lenType)
	}
}

// ReadMessageLengthWrapper adapts a network.Header into the reader shape the
// connection layer calls: read the prefix, then hand back the message length it
// declares.
func ReadMessageLengthWrapper(header network.Header) connection.MessageLengthReader {
	return func(r io.Reader) (int, error) {
		n, err := header.ReadFrom(r)
		if err != nil {
			return n, err
		}

		messageLength := header.Length()

		// Validate message size to prevent buffer overflow attacks
		if messageLength < 0 {
			return n, fmt.Errorf("invalid message length: negative value %d", messageLength)
		}

		if messageLength > MaxMessageSize {
			return n, fmt.Errorf(
				"message length %d exceeds maximum allowed size %d",
				messageLength,
				MaxMessageSize,
			)
		}

		// ISO8583 messages should have a minimum reasonable size
		if messageLength < 20 { // MTI (4) + bitmap (8-16) + at least some data
			return n, fmt.Errorf(
				"message length %d is too small for a valid ISO8583 message",
				messageLength,
			)
		}

		return messageLength, nil
	}
}

// WriteMessageLengthWrapper adapts a header into the writer shape: record the
// length, then emit the prefix. It is the counterpart of
// ReadMessageLengthWrapper, and the pair is why a header can be swapped without
// touching the connection code.
func WriteMessageLengthWrapper(header network.Header) connection.MessageLengthWriter {
	return func(w io.Writer, length int) (int, error) {
		header.SetLength(length)
		n, err := header.WriteTo(w)
		if err != nil {
			return n, fmt.Errorf("writing message header: %w", err)
		}

		return n, nil
	}
}

// NapsWriteLengthWrapper puts a fixed NAPS prefix in front of the framed
// message: the declared length grows by the prefix size, the prefix is written,
// then the header and body follow. Acquirers that expect ISO016000070 count
// those bytes as part of the message, so the length has to include them.
func NapsWriteLengthWrapper(
	h func(w io.Writer, length int) (int, error),
) func(w io.Writer, length int) (int, error) {
	napsPrefix := []byte(NAPSPREFIXATM)
	return func(w io.Writer, length int) (int, error) {
		// First, call the original function with the modified length.
		n, err := h(w, length+len(napsPrefix))
		if err != nil {
			return n, fmt.Errorf("writing message header wrapper: %w", err)
		}

		// Then, write the NAPS prefix to the writer.
		nPrefix, err := w.Write(napsPrefix)
		if err != nil {
			return n + nPrefix, fmt.Errorf("writing napsPrefix: %w", err)
		}

		// Return the total number of bytes written.
		return n + nPrefix, nil
	}
}

// NapsReadLengthWrapper reads and verifies that prefix before the length header,
// failing the connection on a mismatch instead of mis-framing the message that
// follows it.
func NapsReadLengthWrapper(
	h func(r io.Reader) (int, error),
) func(r io.Reader) (int, error) {
	napsPrefix := []byte(NAPSPREFIXATM)
	return func(r io.Reader) (int, error) {
		// First, call the original function to read the message length.
		length, err := h(r)
		if err != nil {
			return length, fmt.Errorf("reading message header wrapper: %w", err)
		}

		// Then, read the NAPS prefix from the reader. ReadFull because a
		// bare Read may return a short TCP segment and cause a spurious
		// prefix mismatch.
		var napsPrefixBuffer [len(NAPSPREFIXATM)]byte
		n, err := io.ReadFull(r, napsPrefixBuffer[:])
		if err != nil {
			return length, fmt.Errorf("reading napsPrefix: %w", err)
		}

		// Check if the read prefix matches the expected napsPrefix.
		if !bytes.Equal(napsPrefixBuffer[:], []byte(NAPSPREFIXATM)) &&
			!bytes.Equal(napsPrefixBuffer[:], []byte(NAPSPREFIXPOS)) {
			return length, fmt.Errorf(
				"napsPrefix mismatch: expected %s, got %s",
				napsPrefix,
				napsPrefixBuffer[:],
			)
		}

		// If everything is fine, return the length.
		return length - n, nil
	}
}
