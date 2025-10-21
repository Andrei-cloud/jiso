package connection

import (
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
)

// mockMessageSpec creates a basic ISO8583 message spec for testing
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
		},
	}
	return spec
}

func TestNewManager(t *testing.T) {
	spec := mockMessageSpec()
	manager := NewManager("localhost", "8080", spec, true, 3, 5*time.Second, 10*time.Second)

	assert.NotNil(t, manager)
	assert.Equal(t, "localhost:8080", manager.GetAddress())
	assert.Equal(t, "Not initialized", manager.GetStatus())
	assert.False(t, manager.IsConnected())
}

func TestManagerConnectionStatus(t *testing.T) {
	spec := mockMessageSpec()
	manager := NewManager("localhost", "8080", spec, false, 3, 5*time.Second, 10*time.Second)

	// Initial state should be not connected
	assert.False(t, manager.IsConnected())

	// A connection that hasn't been established should be able to close without error
	err := manager.Close()
	assert.NoError(t, err)
}

func TestManagerSendWithNoConnection(t *testing.T) {
	spec := mockMessageSpec()
	manager := NewManager("localhost", "8080", spec, false, 3, 5*time.Second, 10*time.Second)

	// Create a test message
	message := iso8583.NewMessage(spec)
	err := message.Field(0, "0100")
	assert.NoError(t, err)

	// Attempting to send without a connection should fail
	_, err = manager.Send(message)
	assert.Error(t, err)
	assert.Equal(t, moovconnection.ErrConnectionClosed, err)

	// Same for background send
	_, err = manager.BackgroundSend(message)
	assert.Error(t, err)
	assert.Equal(t, moovconnection.ErrConnectionClosed, err)
}
