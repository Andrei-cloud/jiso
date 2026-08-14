package analyzer

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

func BenchmarkCorrelator_Correlate(b *testing.B) {
	spec := iso8583.Spec87
	var messages []*AnnotatedMessage

	for i := 1; i <= 200; i++ {
		req := iso8583.NewMessage(spec)
		req.MTI("0200")
		_ = req.Field(11, utils.RandString(6))
		_ = req.Field(3, "000000")

		resp := iso8583.NewMessage(spec)
		resp.MTI("0210")
		stan, _ := req.GetString(11)
		_ = resp.Field(11, stan)
		_ = resp.Field(39, "00")

		messages = append(messages, &AnnotatedMessage{Message: req})
		messages = append(messages, &AnnotatedMessage{Message: resp})
	}

	correlator := NewCorrelator()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := correlator.Correlate(messages)
		if err != nil {
			b.Fatalf("Correlate failed: %v", err)
		}
	}
}

func BenchmarkStreamAnalyzer_ExtractMessagesFromStream(b *testing.B) {
	spec := iso8583.Spec87
	analyzer := NewStreamAnalyzer(spec)

	var stream bytes.Buffer
	for i := 1; i <= 50; i++ {
		msg := iso8583.NewMessage(spec)
		msg.MTI("0800")
		_ = msg.Field(11, "123456")
		_ = msg.Field(70, "001")
		packed, _ := msg.Pack()

		var hdr [2]byte
		binary.BigEndian.PutUint16(hdr[:], uint16(len(packed)))
		stream.Write(hdr[:])
		stream.Write(packed)
	}

	rawStream := stream.Bytes()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := analyzer.ExtractMessagesFromStream(rawStream, "binary2")
		if err != nil {
			b.Fatalf("ExtractMessagesFromStream failed: %v", err)
		}
	}
}
