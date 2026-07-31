package connection

import (
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
)

func mockMessageSpec() *iso8583.MessageSpec {
	spec := &iso8583.MessageSpec{
		Name: "Test Spec",
		Fields: map[int]field.Field{
			0: field.NewString(&field.Spec{
				Length:      4,
				Description: "Message Type Indicator",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			1: field.NewBitmap(&field.Spec{
				Length:      16,
				Description: "Bitmap",
				Enc:         encoding.Binary,
				Pref:        prefix.Binary.Fixed,
			}),
			2: field.NewString(&field.Spec{
				Length:      19,
				Description: "Primary Account Number",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.LL,
			}),
			11: field.NewString(&field.Spec{
				Length:      6,
				Description: "Systems Trace Audit Number",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			39: field.NewString(&field.Spec{
				Length:      2,
				Description: "Response Code",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
		},
	}
	return spec
}

func TestNewManager(t *testing.T) {
	spec := mockMessageSpec()
	manager := NewManager("localhost", "8080", spec, true, 3, 5*time.Second, 10*time.Second, nil)

	assert.NotNil(t, manager)
	assert.Equal(t, "localhost:8080", manager.GetAddress())
	assert.Equal(t, "Not initialized", manager.GetStatus())
	assert.False(t, manager.IsConnected())
}

