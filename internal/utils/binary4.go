package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

type Binary4BytesAdapter struct {
	mu     sync.RWMutex
	length int
}

func NewBinary4BytesAdapter() *Binary4BytesAdapter {
	return &Binary4BytesAdapter{}
}

func (a *Binary4BytesAdapter) SetLength(length int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.length = length
}

func (a *Binary4BytesAdapter) Length() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.length
}

func (a *Binary4BytesAdapter) WriteTo(w io.Writer) (int, error) {
	a.mu.RLock()
	length := a.length
	a.mu.RUnlock()

	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(length))

	n, err := w.Write(buf)
	if err != nil {
		return n, fmt.Errorf("writing binary4 header: %w", err)
	}

	return n, nil
}

func (a *Binary4BytesAdapter) ReadFrom(r io.Reader) (int, error) {
	buf := make([]byte, 4)
	n, err := io.ReadFull(r, buf)
	if err != nil {
		return n, fmt.Errorf("reading binary4 header: %w", err)
	}

	length := int(binary.BigEndian.Uint32(buf))

	a.mu.Lock()
	a.length = length
	a.mu.Unlock()

	return n, nil
}
