package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// Binary2BytesAdapter presents a 2-byte big-endian binary length prefix as the moov-io network.Header that frames
// messages on the wire. moov ships ASCII and BCD prefixes but no bare binary
// one, so this is the missing adapter; the mutex is what lets one connection's
// header be shared by the reader and writer goroutines.
type Binary2BytesAdapter struct {
	mu     sync.RWMutex
	length int
}

// NewBinary2BytesAdapter returns a header with a length of zero. It is usable immediately:
// every write sets the length first, so there is no half-built state.
func NewBinary2BytesAdapter() *Binary2BytesAdapter {
	return &Binary2BytesAdapter{}
}

// SetLength records the length the next WriteTo emits. The connection layer
// calls it after encoding a message, so the value always describes what is
// about to go out.
func (a *Binary2BytesAdapter) SetLength(length int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.length = length
}

// Length returns the length currently recorded, 0 before the first message.
func (a *Binary2BytesAdapter) Length() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.length
}

// WriteTo emits the 2-byte prefix. Its (int, error) shape comes from
// moov-io's network.Header, not from io.WriterTo, which asks for
// (int64, error); conforming to that would break the header contract the
// connection layer codes against.
//
//nolint:govet // these implement moov-io network.Header, whose contract is
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

// ReadFrom reads 2 bytes of prefix and records the length they declare,
// which is how the connection layer learns a message's size before reading its
// body. Like WriteTo it returns (int, error) for the network.Header contract.
//
//nolint:govet // these implement moov-io network.Header, whose contract is
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
