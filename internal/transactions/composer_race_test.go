package transactions

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/require"
)

// Regression: concurrent Compose calls raced on the unguarded tc.cache map
// (fatal "concurrent map writes") and lazily initialized parsedCache with
// no lock. Under -race this test fails on the old implementation.
func TestConcurrentComposeCaches(t *testing.T) {
	t.Parallel()

	data := []map[string]any{
		{
			"name":        "test1",
			"description": "Test transaction 1",
			"fields": map[string]any{
				"0":  "0200",
				"2":  "1234567890123456",
				"3":  123456,
				"4":  "10000",
				"7":  "auto",
				"11": "auto",
				"37": "auto",
			},
		},
		{
			"name":        "test2",
			"description": "Test transaction 2",
			"fields": map[string]any{
				"0":  "0200",
				"2":  "9876543210987654",
				"3":  654321,
				"4":  "20000",
				"7":  "auto",
				"11": "auto",
				"37": "auto",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	require.NoError(t, err)

	tmpFile := filepath.Join(t.TempDir(), "test_transactions.json")
	require.NoError(t, os.WriteFile(tmpFile, dataBytes, 0o644))

	tc, err := NewTransactionCollection(tmpFile, iso8583.Spec87)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "test1"
			if i%2 == 0 {
				name = "test2"
			}
			for j := 0; j < 40; j++ {
				if _, err := tc.Compose(name); err != nil {
					t.Errorf("Compose(%s): %v", name, err)
					return
				}
				if _, err := tc.ComposeRaw(name); err != nil {
					t.Errorf("ComposeRaw(%s): %v", name, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
