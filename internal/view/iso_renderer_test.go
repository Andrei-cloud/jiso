package view

import (
	"bytes"
	"testing"

	"github.com/moov-io/iso8583"
)

func createTestMessage() *iso8583.Message {
	msg := iso8583.NewMessage(iso8583.Spec87)
	msg.Field(0, "0100")

	return msg
}

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
