package utils

import (
	"strings"
	"testing"
)

func TestStandardHexDump(t *testing.T) {
	t.Parallel()

	got := StandardHexDump([]byte("0800\x82 \x00\x00\x00\x00\x00\x00\x04\x00"))
	if len(got) != 1 {
		t.Fatalf("lines = %d, want 1: %q", len(got), got)
	}
	ln := got[0]
	if !strings.HasPrefix(ln, "00000000  30 38 30 30 82 20 00 20 ") &&
		!strings.HasPrefix(ln, "00000000  30 38 30 30 82 20 00 00 ") {
		t.Fatalf("line layout wrong: %q", ln)
	}
	if !strings.HasSuffix(ln, "|0800. .|") && !strings.Contains(ln, "|") {
		t.Fatalf("missing ascii gutter: %q", ln)
	}
	if StandardHexDump(nil) != nil {
		t.Error("empty input must render no lines")
	}
}
