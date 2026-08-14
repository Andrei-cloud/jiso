package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

type Binary2BytesAdapter struct {
	mu     sync.RWMutex
	length int
}

func NewBinary2BytesAdapter() *Binary2BytesAdapter {
	return &Binary2BytesAdapter{}
}

func (a *Binary2BytesAdapter) SetLength(length int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.length = length
}

func (a *Binary2BytesAdapter) Length() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.length
}

func (a *Binary2BytesAdapter) WriteTo(w io.Writer) (int, error) {
	a.mu.RLock()
	length := a.length
	a.mu.RUnlock()

	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], uint16(length))

	n, err := w.Write(buf[:])
	if err != nil {
		return n, fmt.Errorf("writing binary2 header: %w", err)
	}

	return n, nil
}

func (a *Binary2BytesAdapter) ReadFrom(r io.Reader) (int, error) {
	var buf [2]byte
	n, err := io.ReadFull(r, buf[:])
	if err != nil {
		return n, fmt.Errorf("reading binary2 header: %w", err)
	}

	length := int(binary.BigEndian.Uint16(buf[:]))

	a.mu.Lock()
	a.length = length
	a.mu.Unlock()

	return n, nil
}
