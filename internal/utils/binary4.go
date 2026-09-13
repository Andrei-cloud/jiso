package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// Binary4BytesAdapter presents a 4-byte big-endian binary length prefix as the
// moov-io network.Header that frames messages on the wire. moov ships ASCII and
// BCD prefixes but no bare binary one, so this is the missing adapter; the mutex
// is what lets one connection's header be shared by the reader and writer.
type Binary4BytesAdapter struct {
	mu     sync.RWMutex
	length int
}

// NewBinary4BytesAdapter returns a header with a length of zero. It is usable
// immediately: every write sets the length first, so there is no half-built state.
func NewBinary4BytesAdapter() *Binary4BytesAdapter {
	return &Binary4BytesAdapter{}
}

// SetLength records the length the next WriteTo emits. The connection layer calls
// it after encoding a message, so the value always describes what is about to go
// out.
func (a *Binary4BytesAdapter) SetLength(length int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.length = length
}

// Length returns the length currently recorded, 0 before the first message.
func (a *Binary4BytesAdapter) Length() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.length
}

// WriteTo emits the 4-byte prefix. Its (int, error) shape comes from moov-io's
// network.Header, not from io.WriterTo, which asks for (int64, error); conforming
// to that would break the header contract the connection layer codes against.
//
//nolint:govet // these implement moov-io network.Header, whose contract is
func (a *Binary4BytesAdapter) WriteTo(w io.Writer) (int, error) {
	a.mu.RLock()
	length := a.length
	a.mu.RUnlock()

	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(length))

	n, err := w.Write(buf[:])
	if err != nil {
		return n, fmt.Errorf("writing binary4 header: %w", err)
	}

	return n, nil
}

// ReadFrom reads 4 bytes of prefix and records the length they declare, which is
// how the connection layer learns a message's size before reading its body. Like
// WriteTo it returns (int, error) for the network.Header contract.
//
//nolint:govet // these implement moov-io network.Header, whose contract is
func (a *Binary4BytesAdapter) ReadFrom(r io.Reader) (int, error) {
	var buf [4]byte
	n, err := io.ReadFull(r, buf[:])
	if err != nil {
		return n, fmt.Errorf("reading binary4 header: %w", err)
	}

	length := int(binary.BigEndian.Uint32(buf[:]))

	a.mu.Lock()
	a.length = length
	a.mu.Unlock()

	return n, nil
}
