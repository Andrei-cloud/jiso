package app

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

// InfoView is the JSON-serializable composed message info for inspect:
// the transaction metadata from the repository's Info, the composed
// sample message (packed hex + parsed field view), and spec metadata.
//
// parity contract: it is the SINGLE composition data model both
// `jiso inspect <tx>` (headless, --json renders this struct verbatim) and
// the REPL `info` command (thin shim over NewInfoView /
// NewInfoViewFromComposedMessage) are built from, so the two paths cannot
// drift. Dataset/session interpolation happens inside
// transactions.Repository.Compose, upstream of this view.
type InfoView struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	MTI            string         `json:"mti,omitempty"`
	ProcessingCode string         `json:"processing_code,omitempty"`
	Fields         map[string]any `json:"fields"`
	SpecName       string         `json:"spec_name,omitempty"`
	PackedBytes    int            `json:"packed_bytes,omitempty"`
	PackedHEX      string         `json:"packed_hex,omitempty"`
	ParsedMessage  string         `json:"parsed_message,omitempty"`
	ComposeError   string         `json:"compose_error,omitempty"`
	PackError      string         `json:"pack_error,omitempty"`
}

// NewInfoView builds the pre-composition view from the exact outputs of
// transactions.Repository.Info (name, description, fields JSON document).
// Unparsable fields JSON leaves Fields nil; composition errors are passed
// through via SetComposeError so failure shapes stay renderable.
func NewInfoView(name, description, fieldsJSON string) *InfoView {
	view := &InfoView{
		Name:        name,
		Description: description,
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err == nil {
		view.Fields = fields
		view.MTI = fieldString(fields, "0")
		view.ProcessingCode = fieldString(fields, "3")
	}

	return view
}

// NewInfoViewFromComposedMessage builds the full inspect view from the
// repository Info outputs plus a composed sample message: it packs the
// message (recording PackError on failure, like the legacy inspect) and
// renders the parsed field view and spec metadata from the message spec.
func NewInfoViewFromComposedMessage(name, description, fieldsJSON string, message *iso8583.Message) *InfoView {
	view := NewInfoView(name, description, fieldsJSON)

	if message == nil {
		return view
	}

	if spec := message.GetSpec(); spec != nil {
		view.SpecName = spec.Name
	}

	packed, err := message.Pack()
	if err != nil {
		view.PackError = err.Error()

		return view
	}

	view.PackedBytes = len(packed)
	view.PackedHEX = utils.HexDump(packed)

	var buf bytes.Buffer
	_ = utils.Describe(message, &buf, iso8583.DoNotFilterFields()...)
	view.ParsedMessage = buf.String()

	return view
}

// SetComposeError records a transaction composition failure on the view.
func (v *InfoView) SetComposeError(err error) {
	if err != nil {
		v.ComposeError = err.Error()
	}
}

func fieldString(fields map[string]any, key string) string {
	value, ok := fields[key]
	if !ok {
		return ""
	}

	if s, ok := value.(string); ok {
		return s
	}

	return fmt.Sprintf("%v", value)
}
