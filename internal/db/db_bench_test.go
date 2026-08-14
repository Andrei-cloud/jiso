package db

import (
	"testing"

	"github.com/moov-io/iso8583"
)

func BenchmarkDeriveResponseCode(b *testing.B) {
	jsonStr := `{"mti":"0810","fields":{"11":"123456","39":"00","70":"001"}}`
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = deriveResponseCode(&jsonStr)
	}
}

func BenchmarkMessageToJSON(b *testing.B) {
	msg := iso8583.NewMessage(iso8583.Spec87)
	msg.MTI("0200")
	_ = msg.Field(2, "4111111111111111")
	_ = msg.Field(3, "000000")
	_ = msg.Field(4, "000000010000")
	_ = msg.Field(11, "123456")
	_ = msg.Field(37, "000000123456")
	_ = msg.Field(41, "TERM0001")
	_ = msg.Field(42, "MERCHANT0000001")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := MessageToJSON(msg)
		if err != nil {
			b.Fatalf("MessageToJSON failed: %v", err)
		}
	}
}
