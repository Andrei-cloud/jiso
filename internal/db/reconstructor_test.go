package db

import (
	"strings"
	"testing"
)

func TestReconstructFromJSON(t *testing.T) {
	msgJSON := `{"mti":"0200","fields":{"3":"000000","4":"000000010000","11":"000001"}}`

	res, err := Reconstruct(msgJSON, "", "")
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	if res.IsRawFallback {
		t.Errorf("Expected IsRawFallback to be false, got true")
	}

	if res.HEX == "" {
		t.Errorf("Expected non-empty HEX output")
	}

	if !strings.Contains(res.DescribeText, "0200") && !strings.Contains(res.DescribeText, "MTI") {
		t.Errorf("Expected DescribeText to contain MTI details, got: %s", res.DescribeText)
	}
}

func TestReconstructFromHEXFallback(t *testing.T) {
	rawHex := "30323030" // ASCII "0200"

	res, err := Reconstruct("", rawHex, "")
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	if !res.IsRawFallback {
		t.Errorf("Expected IsRawFallback to be true")
	}

	if res.HEX == "" {
		t.Errorf("Expected non-empty HEX output")
	}
}
