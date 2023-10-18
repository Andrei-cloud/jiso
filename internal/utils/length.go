package utils

import (
	"fmt"
	"io"

	"github.com/moov-io/iso8583/network"
)

var header network.Header

func SelectLength(lenType string) {
	switch lenType {
	case "ascii4":
		header = network.NewASCII4BytesHeader()
	case "binary2":
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
