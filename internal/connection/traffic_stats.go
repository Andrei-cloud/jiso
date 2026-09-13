// traffic_stats.go wires wire-volume counters into the moov connection
// wrappers so the §H net strip ("tx 2.1MB rx 1.8MB") has real bus
// counters. Bytes consumed/produced by the length reader/writer are
// counted exactly; the message body (which the library reads/writes on
// the same stream after the length header) is accounted from the length
// the wrapper returns — exact on success, marginally optimistic when a
// read/write dies mid-body. Retransmission is a TCP concern the
// application layer cannot observe; the strip keeps "reconnects" as its
// retry signal.
package connection

import (
	"io"

	moovconnection "github.com/moov-io/iso8583-connection"
)

// countingReader forwards reads and tallies the bytes consumed.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)

	return n, err
}

// countingWriter forwards writes and tallies the bytes produced.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)

	return n, err
}

// countReads wraps the outermost length reader so every inbound message
// (header consumed here + body read by the library) reaches the rx
// counter.
func (m *Manager) countReads(inner moovconnection.MessageLengthReader) moovconnection.MessageLengthReader {
	return func(r io.Reader) (int, error) {
		cr := &countingReader{r: r}
		length, err := inner(cr)
		if m.networkStats != nil {
			m.networkStats.RecordRxBytes(cr.n)
			if err == nil {
				m.networkStats.RecordRxBytes(int64(length))
			}
		}

		return length, err
	}
}

// countWrites wraps the outermost length writer so every outbound
// message sent through the library's Send path (header produced here +
// body written by the library) reaches the tx counter. Raw
// Connection.Write calls are counted at their call sites instead.
func (m *Manager) countWrites(inner moovconnection.MessageLengthWriter) moovconnection.MessageLengthWriter {
	return func(w io.Writer, length int) (int, error) {
		cw := &countingWriter{w: w}
		n, err := inner(cw, length)
		if m.networkStats != nil {
			m.networkStats.RecordTxBytes(cw.n)
			if err == nil {
				m.networkStats.RecordTxBytes(int64(length))
			}
		}

		return n, err
	}
}

// recordSendBytes accounts one raw Connection.Write payload.
func (m *Manager) recordSendBytes(n int) {
	if m.networkStats != nil {
		m.networkStats.RecordTxBytes(int64(n))
	}
}
