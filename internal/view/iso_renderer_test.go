package view

import (
	"bytes"
	"testing"
)

func TestNewISOMessageRenderer(t *testing.T) {
	// Test with nil output
	renderer := NewISOMessageRenderer(nil)
	if renderer == nil {
		t.Fatal("NewISOMessageRenderer returned nil")
	}
	if renderer.output == nil {
		t.Error("output should not be nil when nil passed")
	}

	// Test with custom output
	var buf bytes.Buffer
	renderer2 := NewISOMessageRenderer(&buf)
	if renderer2.output != &buf {
		t.Error("output should be the passed writer")
	}
}
