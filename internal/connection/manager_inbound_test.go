package connection

import (
	"fmt"
	"testing"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
)

func TestCloseCleansUpPendingRequests(t *testing.T) {
	spec := mockMessageSpec()
	// Create a server that doesn't respond (for timeout testing)
	server, err := startTestServer(spec, false) // Don't respond
	assert.NoError(t, err)
	defer server.Close()

	manager := NewManager(
		"localhost",
		fmt.Sprintf("%d", server.port()),
		spec,
		false,
		3,
		5*time.Second,
		10*time.Second,
		nil,
	)
	manager.SetResponseTimeout(500 * time.Millisecond) // Short timeout

	// Connect
	err = manager.Connect(false, utils.NewBinary2BytesAdapter())
	assert.NoError(t, err)

	// Send multiple async requests
	const numRequests = 3
	responseChans := make([]<-chan *iso8583.Message, numRequests)

	for i := 0; i < numRequests; i++ {
		message := iso8583.NewMessage(spec)
		err = message.Field(0, "0800")
		assert.NoError(t, err)
		stan := fmt.Sprintf("%06d", 123456+i)
		err = message.Field(11, stan)
		assert.NoError(t, err)

		responseChan, err := manager.SendAsync(message, fmt.Sprintf("test_transaction_%d", i))
		assert.NoError(t, err)
		responseChans[i] = responseChan
	}

	// Close manager - should clean up all pending requests
	err = manager.Close()
	assert.NoError(t, err)

	// All channels should be closed
	for i, responseChan := range responseChans {
		select {
		case resp, ok := <-responseChan:
			if ok {
				t.Errorf("Channel %d should be closed, but got response: %v", i, resp)
			}
			// ok == false means channel is closed, which is expected
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Channel %d should be closed immediately after Close()", i)
		}
	}
}

