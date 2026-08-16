package utils

import (
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFieldData_And_ExtractMessageFields(t *testing.T) {
	spec := &iso8583.MessageSpec{
		Fields: map[int]field.Field{
			0: field.NewString(&field.Spec{
				Length:      4,
				Description: "MTI",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			1: field.NewBitmap(&field.Spec{
				Length:      8,
				Description: "Bitmap",
				Enc:         encoding.Binary,
				Pref:        prefix.Binary.Fixed,
			}),
			2: field.NewString(&field.Spec{
				Length:      16,
				Description: "PAN",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			62: field.NewComposite(&field.Spec{
				Length:      255,
				Description: "CPS",
				Pref:        prefix.Binary.Fixed,
				Bitmap:      field.NewBitmap(&field.Spec{Length: 1, Description: "Field 62.0 Bitmap", Enc: encoding.Binary, Pref: prefix.Binary.Fixed, DisableAutoExpand: true}),
				Subfields: map[string]field.Field{
					"1": field.NewString(&field.Spec{
						Length:      1,
						Description: "ACI",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
					"2": field.NewString(&field.Spec{
						Length:      15,
						Description: "TID",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
					"3": field.NewString(&field.Spec{
						Length:      4,
						Description: "Validation Code",
						Enc:         encoding.ASCII,
						Pref:        prefix.ASCII.Fixed,
					}),
				},
			}),
		},
	}

	msg := iso8583.NewMessage(spec)
	msg.MTI("0100")
	require.NoError(t, msg.Field(2, "4085652009074000"))

	compMap := map[string]interface{}{
		"1": "A",
		"2": "466215320236000",
		"3": "6225",
	}
	require.NoError(t, SetCompositeFieldValue(msg, spec, 62, compMap))

	fields := ExtractMessageFields(msg, spec)
	require.NotNil(t, fields)

	assert.Equal(t, "4085652009074000", fields["2"])

	f62, ok := fields["62"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "A", f62["1"])
	assert.Equal(t, "466215320236000", f62["2"])
	assert.Equal(t, "6225", f62["3"])
}

func TestSortedSubfieldKeys(t *testing.T) {
	input := map[string]int{
		"10": 1,
		"2":  2,
		"1":  3,
		"C1": 4,
		"20": 5,
		"A1": 6,
	}

	sorted := SortedSubfieldKeys(input)
	// Numeric keys sorted numerically first, then alphanumeric
	expected := []string{"1", "2", "10", "20", "A1", "C1"}
	assert.Equal(t, expected, sorted)
}
