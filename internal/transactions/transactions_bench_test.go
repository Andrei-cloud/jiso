package transactions

import (
	json "github.com/goccy/go-json"
	"os"
	"testing"

	"github.com/moov-io/iso8583"
)

func BenchmarkCompose(b *testing.B) {
	data := []map[string]interface{}{
		{
			"name":        "test1",
			"description": "Test transaction 1",
			"fields": map[string]interface{}{
				"2":  "1234567890123456",
				"3":  123456,
				"4":  "10000",
				"7":  "auto",
				"11": "auto",
				"37": "auto",
				"48": "Test data {{data.card_no}} placeholder",
			},
			"dataset_name": "card_pool",
		},
		{
			"type": "dataset",
			"name": "card_pool",
			"data": []map[string]string{
				{"card_no": "4111111111111111"},
				{"card_no": "4222222222222222"},
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		b.Fatalf("failed to marshal setup data: %v", err)
	}
	file, err := os.CreateTemp("", "bench_transactions.json")
	if err != nil {
		b.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(dataBytes); err != nil {
		b.Fatalf("failed to write temp file: %v", err)
	}
	_ = file.Close()

	tc, err := NewTransactionCollection(file.Name(), iso8583.Spec87)
	if err != nil {
		b.Fatalf("failed to create TransactionCollection: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := tc.Compose("test1")
		if err != nil {
			b.Fatalf("Compose failed: %v", err)
		}
	}
}

func BenchmarkComposeRaw(b *testing.B) {
	data := []map[string]interface{}{
		{
			"name":        "test1",
			"description": "Test transaction 1",
			"fields": map[string]interface{}{
				"2":  "1234567890123456",
				"3":  123456,
				"4":  "10000",
				"7":  "auto",
				"11": "auto",
				"37": "auto",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		b.Fatalf("failed to marshal setup data: %v", err)
	}
	file, err := os.CreateTemp("", "bench_transactions_raw.json")
	if err != nil {
		b.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(dataBytes); err != nil {
		b.Fatalf("failed to write temp file: %v", err)
	}
	_ = file.Close()

	tc, err := NewTransactionCollection(file.Name(), iso8583.Spec87)
	if err != nil {
		b.Fatalf("failed to create TransactionCollection: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := tc.ComposeRaw("test1")
		if err != nil {
			b.Fatalf("ComposeRaw failed: %v", err)
		}
	}
}
