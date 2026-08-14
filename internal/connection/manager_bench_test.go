package connection

import (
	"testing"

	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

func BenchmarkBuildFullPayload_Binary2(b *testing.B) {
	msg := iso8583.NewMessage(iso8583.Spec87)
	msg.MTI("0800")
	_ = msg.Field(11, "123456")
	_ = msg.Field(70, "001")

	mgr := &Manager{
		header: utils.NewBinary2BytesAdapter(),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := mgr.buildFullPayload(msg)
		if err != nil {
			b.Fatalf("buildFullPayload failed: %v", err)
		}
	}
}

func BenchmarkBuildFullPayload_Binary4(b *testing.B) {
	msg := iso8583.NewMessage(iso8583.Spec87)
	msg.MTI("0800")
	_ = msg.Field(11, "123456")
	_ = msg.Field(70, "001")

	mgr := &Manager{
		header: utils.NewBinary4BytesAdapter(),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := mgr.buildFullPayload(msg)
		if err != nil {
			b.Fatalf("buildFullPayload failed: %v", err)
		}
	}
}

func BenchmarkCloneHeader_Binary2(b *testing.B) {
	h := utils.NewBinary2BytesAdapter()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cloneHeader(h)
	}
}
