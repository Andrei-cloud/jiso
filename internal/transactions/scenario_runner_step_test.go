package transactions

import (
	"strings"
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/padding"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/require"

	"jiso/internal/service"
)

// specWithField builds a minimal spec holding one string field for
// validator tests.
func specWithField(id int, fs *field.Spec) *iso8583.MessageSpec {
	return &iso8583.MessageSpec{
		Name:   "unit-test-spec",
		Fields: map[int]field.Field{id: field.NewString(fs)},
	}
}

// TestValidateStringFieldLengthUndersize: a value shorter
// than a fixed-prefix field's length is a guaranteed Pack failure at
// send time (it surfaced there as a misleading "network send failed");
// the validator must reject it at load with the real cause instead.
func TestValidateStringFieldLengthUndersize(t *testing.T) {
	t.Parallel()

	fixed := &field.Spec{
		Length: 4,
		Enc:    encoding.ASCII,
		Pref:   prefix.ASCII.Fixed,
	}
	tests := []struct {
		name    string
		spec    *field.Spec
		value   string
		wantErr string
	}{
		{
			name:    "undersize fixed unpadded rejected",
			spec:    fixed,
			value:   "021",
			wantErr: "requires exactly 4",
		},
		{
			name: "undersize fixed padded accepted",
			spec: &field.Spec{
				Length: 4,
				Enc:    encoding.ASCII,
				Pref:   prefix.ASCII.Fixed,
				Pad:    padding.NewLeftPadder('0'),
			},
			value: "0",
		},
		{
			name: "undersize variable accepted",
			spec: &field.Spec{
				Length: 25,
				Enc:    encoding.ASCII,
				Pref:   prefix.ASCII.LL,
			},
			value: "short",
		},
		{
			name:    "oversize still rejected",
			spec:    fixed,
			value:   "0123456789",
			wantErr: "exceeds maximum length 4",
		},
		{
			name:  "auto keyword accepted",
			spec:  fixed,
			value: "auto",
		},
		{
			name:  "placeholder accepted",
			spec:  fixed,
			value: "{{field22}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStringFieldLength(22, tt.value, specWithField(22, tt.spec))
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestPackStepRequestNamesSpec: the pack failure message must name the
// spec (a local spec-fit failure), never masquerade as a network fault.
func TestPackStepRequestNamesSpec(t *testing.T) {
	t.Parallel()

	spec := *iso8583.Spec87 // copy: never mutate the shared global
	spec.Name = "FLEX-TEST"
	sr := &ScenarioRunner{svc: &service.Service{MessageSpec: &spec}}

	msg := iso8583.NewMessage(&spec)
	require.NoError(t, msg.Field(0, "0800"))
	// Spec87 field 22 is ASCII.Fixed length 3; "02" is a guaranteed
	// local pack failure (class).
	require.NoError(t, msg.Field(22, "02"))

	_, err := sr.packStepRequest(msg)
	require.Error(t, err)
	if !strings.Contains(err.Error(), `spec "FLEX-TEST"`) {
		t.Errorf("pack error must name the spec, got: %v", err)
	}
	if strings.Contains(err.Error(), "network") {
		t.Errorf("pack error must not read like a network fault: %v", err)
	}
}
