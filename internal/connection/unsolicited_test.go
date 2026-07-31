package connection

import (
	"testing"
	"time"

	"jiso/internal/config"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
)

type mockMatcherImpl struct{}

func (m *mockMatcherImpl) MatchAndCompose(req *iso8583.Message, spec *iso8583.MessageSpec) (*config.MockRouteConfig, *iso8583.Message, error) {
	resp := iso8583.NewMessage(spec)
	resp.MTI("0810")
	resp.Field(39, "00")
	route := &config.MockRouteConfig{Name: "Mock Matcher Route"}
	return route, resp, nil
}

func TestSetMockMatcher(t *testing.T) {
	spec := mockMessageSpec()
	mgr := NewManager("localhost", "9999", spec, false, 1, 1*time.Second, 2*time.Second, nil)

	assert.Nil(t, mgr.mockMatcher)

	matcher := &mockMatcherImpl{}

	mgr.SetMockMatcher(matcher)
	assert.NotNil(t, mgr.mockMatcher)

	mgr.SetMockMatcher(nil)
	assert.Nil(t, mgr.mockMatcher)
}

func TestUnsolicitedIncomingMessageHandling(t *testing.T) {
	spec := mockMessageSpec()
	mgr := NewManager("localhost", "9999", spec, false, 1, 1*time.Second, 2*time.Second, nil)

	matcher := &mockMatcherImpl{}
	mgr.SetMockMatcher(matcher)

	// Send unmatched message to handleInboundMessage (no pending request)
	unsolicitedMsg := iso8583.NewMessage(spec)
	unsolicitedMsg.MTI("0800")
	_ = unsolicitedMsg.Field(11, "123456")

	// handleInboundMessage should process via mockMatcher without panicking
	assert.NotPanics(t, func() {
		mgr.handleInboundMessage(unsolicitedMsg)
	})

	// Allow goroutine time to complete matching logic
	time.Sleep(50 * time.Millisecond)
}
