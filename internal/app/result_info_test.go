package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

const infoFieldsJSON = `{"0":"0200","2":"4242424242424242","3":"000000","11":"42"}`

func composedInfoMessage() *iso8583.Message {
	msg := iso8583.NewMessage(utils.GetDefaultSpec())
	msg.MTI("0200")
	// These fields are known-valid for the default spec; a rejection is a
	// fixture bug, so fail fast instead of returning a hollow message.
	for _, f := range []struct {
		id  int
		val string
	}{
		{2, "4242424242424242"},
		{3, "000000"},
		{11, "000042"},
	} {
		if err := msg.Field(f.id, f.val); err != nil {
			panic(fmt.Sprintf("fixture field %d: %v", f.id, err))
		}
	}

	return msg
}

func TestNewInfoView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fieldsJSON  string
		wantMTI     string
		wantProc    string
		wantFields  int
		wantNoField bool
	}{
		{
			name:       "valid fields json extracts mti and processing code",
			fieldsJSON: infoFieldsJSON,
			wantMTI:    "0200",
			wantProc:   "000000",
			wantFields: 4,
		},
		{
			name:        "unparsable fields json keeps fields nil",
			fieldsJSON:  "{not json",
			wantNoField: true,
		},
		{
			name:        "empty fields json keeps fields nil",
			fieldsJSON:  "",
			wantNoField: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewInfoView("Purchase", "a purchase", tt.fieldsJSON)

			if got.Name != "Purchase" || got.Description != "a purchase" {
				t.Errorf("metadata = %+v, want Purchase/a purchase", got)
			}
			if got.MTI != tt.wantMTI || got.ProcessingCode != tt.wantProc {
				t.Errorf("MTI/ProcCode = %q/%q, want %q/%q", got.MTI, got.ProcessingCode, tt.wantMTI, tt.wantProc)
			}
			if tt.wantNoField {
				if got.Fields != nil {
					t.Errorf("Fields = %v, want nil", got.Fields)
				}

				return
			}
			if len(got.Fields) != tt.wantFields {
				t.Errorf("Fields = %v, want %d entries", got.Fields, tt.wantFields)
			}
		})
	}
}

func TestNewInfoViewFromComposedMessage(t *testing.T) {
	t.Parallel()

	got := NewInfoViewFromComposedMessage("Purchase", "a purchase", infoFieldsJSON, composedInfoMessage())

	if got.SpecName != composedInfoMessage().GetSpec().Name {
		t.Errorf("SpecName = %q, want spec name of the composed message", got.SpecName)
	}
	if got.PackedBytes == 0 || got.PackedHEX == "" {
		t.Errorf("PackedBytes/PackedHEX = %d/%q, want packed sample", got.PackedBytes, got.PackedHEX)
	}
	if !strings.Contains(got.ParsedMessage, "0200") {
		t.Errorf("ParsedMessage = %q, want parsed view containing the MTI", got.ParsedMessage)
	}
	if got.PackError != "" || got.ComposeError != "" {
		t.Errorf("PackError/ComposeError = %q/%q, want none", got.PackError, got.ComposeError)
	}

	// Failure shapes: nil message, and a recorded compose error.
	nilMsg := NewInfoViewFromComposedMessage("Purchase", "", infoFieldsJSON, nil)
	if nilMsg.PackedHEX != "" || nilMsg.ParsedMessage != "" {
		t.Errorf("nil message view = %+v, want composition sections empty", nilMsg)
	}

	failed := NewInfoView("Purchase", "", infoFieldsJSON)
	failed.SetComposeError(errors.New("dataset row missing"))
	if failed.ComposeError != "dataset row missing" {
		t.Errorf("ComposeError = %q, want recorded failure text", failed.ComposeError)
	}
}

func TestInfoViewJSONRoundTrip(t *testing.T) {
	t.Parallel()

	failed := NewInfoView("Purchase", "a purchase", "{not json")
	failed.SetComposeError(errors.New("dataset row missing"))

	tests := []struct {
		name     string
		view     *InfoView
		wantKeys []string
	}{
		{
			name:     "composed success shape",
			view:     NewInfoViewFromComposedMessage("Purchase", "a purchase", infoFieldsJSON, composedInfoMessage()),
			wantKeys: []string{"name", "mti", "fields", "spec_name", "packed_hex", "parsed_message"},
		},
		{
			name:     "compose failure shape",
			view:     failed,
			wantKeys: []string{"name", "compose_error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.view, tt.wantKeys)
		})
	}
}
