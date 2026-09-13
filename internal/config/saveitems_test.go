package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMarshalItemPreviewSortsFieldKeys UAT round 6: the generated-item
// picker preview must show ISO8583 field keys in numeric ascending order
// (0,2,11,14), not Go's default map order (0,11,14,2). Composite
// subfields sort numerically too (7 before 56).
func TestMarshalItemPreviewSortsFieldKeys(t *testing.T) {
	it := Item{
		Type:   TypeTransaction,
		Name:   "Captured Flow 0400",
		Fields: []byte(`{"0":"0400","11":"auto","14":"2512","2":"411111","104":{"56":"y","7":"z"}}`),
	}

	b, err := MarshalItemPreview(it, "", "  ")
	if err != nil {
		t.Fatalf("MarshalItemPreview: %v", err)
	}
	p := string(b)

	for _, ord := range [][2]string{{`"0"`, `"2"`}, {`"2"`, `"11"`}, {`"11"`, `"14"`}, {`"7"`, `"56"`}} {
		a := strings.Index(p, ord[0]+":")
		c := strings.Index(p, ord[1]+":")
		if a < 0 || c < 0 || a >= c {
			t.Errorf("preview field order wrong: %s (pos %d) must precede %s (pos %d):\n%s", ord[0], a, ord[1], c, p)
		}
	}
}

// TestSaveItemsWritesNumericFieldOrder pins the write contract: SaveItems
// persists fields in numeric ascending order, so the picker preview (built
// from MarshalItemPreview) and the file agree.
func TestSaveItemsWritesNumericFieldOrder(t *testing.T) {
	it := Item{Type: TypeTransaction, Name: "T", Fields: []byte(`{"0":"a","11":"b","2":"c"}`)}
	path := filepath.Join(t.TempDir(), "out.json")

	if err := SaveItems(path, []Item{it}); err != nil {
		t.Fatalf("SaveItems: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	p := string(data)

	i0 := strings.Index(p, `"0":`)
	i2 := strings.Index(p, `"2":`)
	i11 := strings.Index(p, `"11":`)
	if i0 >= i2 || i2 >= i11 {
		t.Errorf("SaveItems did not sort field keys numerically:\n%s", p)
	}
}
