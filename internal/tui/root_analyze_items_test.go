package tui

import (
	"strings"
	"testing"

	app "jiso/internal/app"
	"jiso/internal/config"
)

// TestAnalyzeItemRowsPreviewNumericOrder UAT round 6: the generated-item
// picker's per-item preview must render ISO8583 fields in numeric ascending
// order (0,2,11) — exactly what the file will contain — not Go's default
// map key order (0,11,2).
func TestAnalyzeItemRowsPreviewNumericOrder(t *testing.T) {
	t.Parallel()

	out := &app.AnalyzeOutput{Mode: app.AnalyzeModeTx}
	out.AttachGeneratedItems([]config.Item{{
		Type:   config.TypeTransaction,
		Name:   "Captured Flow 0400",
		Fields: []byte(`{"0":"0400","11":"auto","2":"411111"}`),
	}})

	rows := analyzeItemRows(out)
	if len(rows) != 1 {
		t.Fatalf("roster rows = %d, want 1", len(rows))
	}
	p := rows[0].Preview
	i0 := strings.Index(p, `"0":`)
	i2 := strings.Index(p, `"2":`)
	i11 := strings.Index(p, `"11":`)
	if i0 >= i2 || i2 >= i11 {
		t.Errorf("picker preview field order is not numeric ascending:\n%s", p)
	}
}
