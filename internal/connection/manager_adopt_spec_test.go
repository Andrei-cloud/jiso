// manager_adopt_spec_test.go pins the fatal UAT journey: a transaction
// composed with its own STAMPED spec (an analyzed extract carries the
// capture's dialect) sent over a connection built on a different spec must
// still receive its response - the connection adopts the message's dialect
// (reconnecting is the honest cost), otherwise the reply arrives packed in
// a dialect the connection cannot unpack and the send dies in a timeout
// beside a server log that had already answered.
package connection

import (
	"strconv"
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/require"

	"jiso/internal/utils"
)

// stampedDialectSpec is the message's own dialect: the same field set as
// the connection's test spec, a different name, and a HEX-packed MTI -
// messages packed in it are undecodable under the ASCII test spec, which
// reproduces the UAT's visa-BCD-on-flex decode failure.
func stampedDialectSpec() *iso8583.MessageSpec {
	return &iso8583.MessageSpec{
		Name: "Stamped Dialect",
		Fields: map[int]field.Field{
			0: field.NewHex(&field.Spec{
				Length:      4,
				Description: "Message Type Indicator (packed)",
				Enc:         encoding.Binary,
				Pref:        prefix.ASCII.Fixed,
			}),
			1: field.NewBitmap(&field.Spec{
				Length:      16,
				Description: "Bitmap",
				Enc:         encoding.Binary,
				Pref:        prefix.Binary.Fixed,
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
}

func TestManagerAdoptsStampedDialect(t *testing.T) {
	t.Parallel()

	specA := mockMessageSpec()
	specB := stampedDialectSpec()

	// The mock server answers in the message's stamped dialect (like the
	// UAT's visa-spec server did).
	server, err := startTestServer(specB, true)
	require.NoError(t, err)
	defer server.Close()

	mgr := NewManager("localhost", strconv.Itoa(server.port()), specA, false, 0, 2*time.Second, 4*time.Second, nil)
	require.NoError(t, mgr.Connect(false, utils.NewBinary2BytesAdapter()))
	defer func() { _ = mgr.Close() }()

	msg := iso8583.NewMessage(specB)
	require.NoError(t, msg.Field(0, "0100"))
	require.NoError(t, msg.Field(11, "000123"))

	// Without the adoption the reply sits undecodable on the wire and this
	// Send dies in a timeout; with it the dialect follows the message and
	// the answer lands.
	resp, err := mgr.Send(msg)
	require.NoError(t, err)
	require.NotNil(t, resp, "the response was packed in the message's own dialect and must still arrive")

	require.Equal(t, specB.Name, mgr.GetSpec().Name, "the connection must adopt the message's dialect")
}
