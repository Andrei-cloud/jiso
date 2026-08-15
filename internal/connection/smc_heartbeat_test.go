package connection

import (
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/utils"
)

func TestIsVisaHeader(t *testing.T) {
	vHdr, err := utils.NewVisaHeader("123456")
	assert.NoError(t, err)
	assert.True(t, IsVisaHeader(vHdr))

	binHdr, _ := utils.SelectLength("binary2")
	assert.False(t, IsVisaHeader(binHdr))

	assert.False(t, IsVisaHeader(nil))
}

func TestSMCHeartbeatDaemon_Lifecycle(t *testing.T) {
	mgr := NewManager("localhost", "9999", nil, false, 1, time.Second, time.Second, nil)
	daemon := NewSMCHeartbeatDaemon(mgr, 100*time.Millisecond)

	assert.False(t, daemon.IsRunning())

	daemon.Start()
	assert.True(t, daemon.IsRunning())

	// Idempotent start
	daemon.Start()
	assert.True(t, daemon.IsRunning())

	daemon.Stop()
	assert.False(t, daemon.IsRunning())

	// Idempotent stop
	daemon.Stop()
	assert.False(t, daemon.IsRunning())
}

func TestVisaSMCHeartbeatField63UsesCompositeNetworkID(t *testing.T) {
	spec := &iso8583.MessageSpec{
		Name: "visa-0800-test",
		Fields: map[int]field.Field{
			0:  field.NewString(&field.Spec{Length: 4, Description: "MTI", Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed}),
			1:  field.NewBitmap(&field.Spec{Length: 8, Description: "Primary Bitmap", Enc: encoding.Binary, Pref: prefix.Binary.Fixed}),
			7:  field.NewString(&field.Spec{Length: 10, Description: "Transmission Date and Time", Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed}),
			11: field.NewString(&field.Spec{Length: 6, Description: "STAN", Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed}),
			63: field.NewComposite(&field.Spec{
				Length:      255,
				Description: "V.I.P. Private-Use Field",
				Pref:        prefix.Binary.L,
				Bitmap:      field.NewBitmap(&field.Spec{Length: 3, Description: "Field 63.0 Bitmap", Enc: encoding.Binary, Pref: prefix.Binary.Fixed, DisableAutoExpand: true}),
				Subfields: map[string]field.Field{
					"1": field.NewString(&field.Spec{Length: 4, Description: "63.1 Network ID", Enc: encoding.BCD, Pref: prefix.BCD.Fixed}),
					"3": field.NewString(&field.Spec{Length: 4, Description: "63.3 Message Reason Code", Enc: encoding.BCD, Pref: prefix.BCD.Fixed}),
				},
			}),
		},
	}

	msg := iso8583.NewMessage(spec)
	msg.MTI("0800")

	err := utils.SetCompositeFieldValue(msg, spec, 63, map[string]interface{}{"1": "0002"})
	require.NoError(t, err)

	packed, err := msg.Pack()
	require.NoError(t, err)
	assert.NotEmpty(t, packed)

	f63, ok := msg.GetField(63).(*field.Composite)
	require.True(t, ok)
	_, exists := f63.GetSubfields()["1"]
	assert.True(t, exists)
}
