package connection

import (
	"bytes"
	"io"
	"testing"

	moovconnection "github.com/moov-io/iso8583-connection"

	"jiso/internal/metrics"
)

func TestCountReadsAccountsHeaderAndBody(t *testing.T) {
	t.Parallel()

	ns := metrics.NewNetworkingStats()
	m := &Manager{networkStats: ns}

	var inner moovconnection.MessageLengthReader = func(r io.Reader) (int, error) {
		buf := make([]byte, 2)
		if _, err := r.Read(buf); err != nil {
			return 0, err
		}

		return 10, nil
	}

	length, err := m.countReads(inner)(bytes.NewReader([]byte("0123456789abcdef")))
	if err != nil || length != 10 {
		t.Fatalf("read = (%d, %v)", length, err)
	}
	if got := ns.RxBytes(); got != 12 {
		t.Errorf("RxBytes = %d, want 12 (2 header + 10 body)", got)
	}
}

func TestCountWritesAccountsHeaderAndBody(t *testing.T) {
	t.Parallel()

	ns := metrics.NewNetworkingStats()
	m := &Manager{networkStats: ns}

	var inner moovconnection.MessageLengthWriter = func(w io.Writer, _ int) (int, error) {
		return w.Write([]byte("00"))
	}

	n, err := m.countWrites(inner)(&bytes.Buffer{}, 10)
	if err != nil || n != 2 {
		t.Fatalf("write = (%d, %v)", n, err)
	}
	if got := ns.TxBytes(); got != 12 {
		t.Errorf("TxBytes = %d, want 12 (2 header + 10 body)", got)
	}
}

func TestCountReadsErrorCountsOnlyConsumed(t *testing.T) {
	t.Parallel()

	ns := metrics.NewNetworkingStats()
	m := &Manager{networkStats: ns}

	var inner moovconnection.MessageLengthReader = func(r io.Reader) (int, error) {
		buf := make([]byte, 4)
		if _, err := r.Read(buf); err != nil {
			return 0, err
		}

		return 0, io.ErrUnexpectedEOF
	}

	if _, err := m.countReads(inner)(bytes.NewReader([]byte("ab"))); err == nil {
		t.Fatal("want error")
	}
	if got := ns.RxBytes(); got != 2 {
		t.Errorf("RxBytes = %d, want 2 (consumed bytes only)", got)
	}
}
