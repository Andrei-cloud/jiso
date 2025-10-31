package utils

import (
	"bytes"
	"fmt"
	"io"

	connection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/network"
)

var header network.Header

const (
	NAPSPREFIXATM = "ISO016000070"
	NAPSPREFIXPOS = "ISO026000070"

	// MaxMessageSize defines the maximum allowed message size in bytes
	// ISO8583 messages are typically small (a few KB)
	MaxMessageSize = 1024
)

func SelectLength(lenType string) (network.Header, error) {
	switch lenType {
	case "ascii4":
		return network.NewASCII4BytesHeader(), nil
	case "binary2", "NAPS":
		return NewBinary2BytesAdapter(), nil
	case "bcd2":
		return network.NewBCD2BytesHeader(), nil
	default:
		return nil, fmt.Errorf("unknown length type: %s", lenType)
	}
}

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

func NapsWriteLengthWrapper(
	h func(w io.Writer, length int) (int, error),
) func(w io.Writer, length int) (int, error) {
	NAPSPREFIX := []byte(NAPSPREFIXATM)
	return func(w io.Writer, length int) (int, error) {
		// First, call the original function with the modified length.
		n, err := h(w, length+len(NAPSPREFIX))
		if err != nil {
			return n, fmt.Errorf("writing message header wrapper: %w", err)
		}

		// Then, write the NAPSPREFIX to the writer.
		nPrefix, err := w.Write(NAPSPREFIX)
		if err != nil {
			return n + nPrefix, fmt.Errorf("writing NAPSPREFIX: %w", err)
		}

		// Return the total number of bytes written.
		return n + nPrefix, nil
	}
}

func NapsReadLengthWrapper(
	h func(r io.Reader) (int, error),
) func(r io.Reader) (int, error) {
	NAPSPREFIX := []byte(NAPSPREFIXATM)
	return func(r io.Reader) (int, error) {
		// First, call the original function to read the message length.
		length, err := h(r)
		if err != nil {
			return length, fmt.Errorf("reading message header wrapper: %w", err)
		}

		// Then, read the NAPSPREFIX from the reader.
		napsPrefixBuffer := make([]byte, len(NAPSPREFIX))
		n, err := r.Read(napsPrefixBuffer)
		if err != nil {
			return length, fmt.Errorf("reading NAPSPREFIX: %w", err)
		}

		// Check if the read prefix matches the expected NAPSPREFIX.
		if !bytes.Equal(napsPrefixBuffer, []byte(NAPSPREFIXATM)) &&
			!bytes.Equal(napsPrefixBuffer, []byte(NAPSPREFIXPOS)) {
			return length, fmt.Errorf(
				"NAPSPREFIX mismatch: expected %s, got %s",
				NAPSPREFIX,
				napsPrefixBuffer,
			)
		}

		// If everything is fine, return the length.
		return length - n, nil
	}
}
