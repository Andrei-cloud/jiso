package app

import "time"

// SendResult is the minimal JSON-serializable outcome of one App.Send call.
// APP-203 expands the result vocabulary; until then keep these structs free of
// io/terminal dependencies so headless frontends can marshal them.
//
// Result struct conventions (APP-203, see result_*.go): plain fields only
// (strings, numbers, bools, time.Time, time.Duration, slices/maps thereof),
// lowercase snake_case json tags, constructors named New<Type>FromSource
// taking the existing internal types. Instants are time.Time, durations
// are time.Duration (same as SendResult.Elapsed); latency percentiles keep
// the session DB's float64-millisecond precision in *_latency_ms fields.
type SendResult struct {
	// Description is the rendered request/response view (the exact text the
	// legacy REPL renderer wrote to stdout), empty when nothing was
	// rendered.
	Description string `json:"description"`
	// Hex is the packed request hex dump.
	Hex string `json:"hex"`
	// Fields holds the response fields as structured views.
	Fields []FieldView `json:"fields,omitempty"`
	// Elapsed is the round-trip send time.
	Elapsed time.Duration `json:"elapsed"`
	// Error carries the failure text when the send pipeline failed after
	// the message was packed.
	Error string `json:"error,omitempty"`
	// Warnings carries non-fatal diagnostics (e.g. STAN mismatch detail)
	// for frontends to print on stderr.
	Warnings []string `json:"warnings,omitempty"`
	// ResponseHex is the packed response hex dump for --hex rendering.
	ResponseHex string `json:"responseHex,omitempty"`
}

// FieldView is one ISO8583 field rendered for display or JSON output.
type FieldView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
}
