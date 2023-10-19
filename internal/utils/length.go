package utils

import (
	"bytes"
	"fmt"
	"io"

	"github.com/moov-io/iso8583/network"
)

var header network.Header

const NAPSPREFIXString = "ISO016000070"

func SelectLength(lenType string) {
	switch lenType {
	case "ascii4":
		header = network.NewASCII4BytesHeader()
	case "binary2", "NAPS":
		header = NewBinary2BytesAdapter()
	case "bcd2":
		header = network.NewBCD2BytesHeader()
	default:
		header = network.NewASCII4BytesHeader()
	}
}

func ReadMessageLength(r io.Reader) (int, error) {
	n, err := header.ReadFrom(r)
	if err != nil {
		return n, err
	}

	return header.Length(), nil
}

func WriteMessageLength(w io.Writer, length int) (int, error) {
	header.SetLength(length)
	n, err := header.WriteTo(w)
	if err != nil {
		return n, fmt.Errorf("writing message header: %w", err)
	}

	return n, nil
}

func NapsWriteLengthWrapper(h func(w io.Writer, length int) (int, error)) func(w io.Writer, length int) (int, error) {
	NAPSPREFIX := []byte(NAPSPREFIXString)
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

func NapsReadLengthWrapper(h func(r io.Reader) (int, error)) func(r io.Reader) (int, error) {
	NAPSPREFIX := []byte(NAPSPREFIXString)
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
		if !bytes.Equal(napsPrefixBuffer, NAPSPREFIX) {
			return length, fmt.Errorf("NAPSPREFIX mismatch: expected %s, got %s", NAPSPREFIX, napsPrefixBuffer)
		}

		// If everything is fine, return the length.
		return length - n, nil
	}
}
