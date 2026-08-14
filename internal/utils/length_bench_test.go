package utils

import (
	"bytes"
	"os"
	"testing"
)

func BenchmarkBinary2BytesAdapter_WriteTo(b *testing.B) {
	header := NewBinary2BytesAdapter()
	header.SetLength(128)
	var buf bytes.Buffer
	buf.Grow(2)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = header.WriteTo(&buf)
	}
}

func BenchmarkBinary2BytesAdapter_ReadFrom(b *testing.B) {
	header := NewBinary2BytesAdapter()
	raw := []byte{0x00, 0x80}
	reader := bytes.NewReader(raw)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader.Reset(raw)
		_, _ = header.ReadFrom(reader)
	}
}

func BenchmarkBinary4BytesAdapter_WriteTo(b *testing.B) {
	header := NewBinary4BytesAdapter()
	header.SetLength(128)
	var buf bytes.Buffer
	buf.Grow(4)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = header.WriteTo(&buf)
	}
}

func BenchmarkVisaHeader_WriteTo(b *testing.B) {
	header, err := NewVisaHeader("000000")
	if err != nil {
		b.Fatalf("failed to create visa header: %v", err)
	}
	header.SetLength(100)
	var buf bytes.Buffer
	buf.Grow(26)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = header.WriteTo(&buf)
	}
}

func BenchmarkRandString(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = RandString(12)
	}
}

func BenchmarkGetStan(b *testing.B) {
	counter := GetCounter()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = counter.GetStan()
	}
}

func BenchmarkGetRRN(b *testing.B) {
	rrn := GetRRNInstance()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = rrn.GetRRN()
	}
}

func BenchmarkResolveSpec(b *testing.B) {
	defaultSpec := GetDefaultSpec()
	tempFile, err := os.CreateTemp("", "bench_spec_*.json")
	if err != nil {
		b.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	_, _ = tempFile.WriteString(`{"fields":{}}`)
	_ = tempFile.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ResolveSpec(tempFile.Name(), defaultSpec)
	}
}
