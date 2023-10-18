package utils

import (
	"fmt"
	"io"

	"github.com/moov-io/iso8583/network"
)

type Binary2BytesAdapter struct {
	binary2Bytes *network.Binary2Bytes
}

func NewBinary2BytesAdapter() *Binary2BytesAdapter {
	return &Binary2BytesAdapter{network.NewBinary2BytesHeader()}
}

func (a *Binary2BytesAdapter) SetLength(length int) {
	a.binary2Bytes.SetLength(length)
}

func (a *Binary2BytesAdapter) Length() int {
	return a.binary2Bytes.Length()
}

func (a *Binary2BytesAdapter) WriteTo(w io.Writer) (int, error) {
	return a.binary2Bytes.WriteTo(w)
}

func (a *Binary2BytesAdapter) ReadFrom(r io.Reader) (int, error) {
	n, err := a.binary2Bytes.ReadFrom(r)
	if err != nil {
		return 0, fmt.Errorf("reading from reader: %w", err)
	}

	return n, nil
}
