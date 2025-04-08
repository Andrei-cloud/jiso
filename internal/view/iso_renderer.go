package view

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/moov-io/iso8583"
)

// ISOMessageRenderer handles displaying ISO8583 messages
type ISOMessageRenderer struct {
	output io.Writer
}

// NewISOMessageRenderer creates a new renderer with optional output destination
func NewISOMessageRenderer(output io.Writer) *ISOMessageRenderer {
	if output == nil {
		output = os.Stdout
	}
	return &ISOMessageRenderer{output: output}
}

// RenderMessage renders an ISO8583 message
func (r *ISOMessageRenderer) RenderMessage(msg *iso8583.Message) {
	iso8583.Describe(msg, r.output, iso8583.DoNotFilterFields()...)
}

// RenderRequestResponse renders a request-response pair with timing
func (r *ISOMessageRenderer) RenderRequestResponse(
	request, response *iso8583.Message,
	elapsed time.Duration,
) {
	fmt.Fprintln(r.output, "--- REQUEST ---")
	r.RenderMessage(request)

	fmt.Fprintln(r.output, "\n--- RESPONSE ---")
	r.RenderMessage(response)

	fmt.Fprintf(r.output, "\nElapsed time: %s\n", elapsed.Round(time.Millisecond))
}
