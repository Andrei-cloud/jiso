package command

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"jiso/internal/db"
)

// TestSessionOverviewPrintRestoresDroppedSections asserts the shared
// renderer keeps the stress-test "Tested Templates" and "Response Codes"
// sections the duplicated cobra copy had dropped (M1 review #23), and emits
// the single shared v2 hint stream (M1 review #16).
func TestSessionOverviewPrintRestoresDroppedSections(t *testing.T) {
	t.Parallel()

	view := &SessionOverview{
		Session: &db.SessionRecord{SessionID: "s1", Status: "active"},
		Stats:   map[string]any{"total_transactions": 1},
		StressTests: []*db.StressTestSummaryRecord{
			{
				WorkerID:          "w1",
				TransactionsJSON:  `["Purchase"]`,
				ResponseCodesJSON: `{"00":1}`,
			},
		},
		Transactions: []*db.EnrichedTransactionRecord{
			{ID: 1, SessionID: "s1", TxName: "Purchase", Timestamp: time.Now(), Success: true, ResponseCode: "00"},
		},
	}

	var w, hint bytes.Buffer
	view.Print(&w, &hint)

	out := w.String()
	assert.Contains(t, out, "Database Statistics for Session: s1")
	assert.Contains(t, out, "Tested Templates:     Purchase")
	assert.Contains(t, out, "Response Codes:       00: 1")
	assert.Contains(t, hint.String(), "To inspect a transaction: jiso db tx <id>")

	// A nil hint writer suppresses the notice (--quiet semantics).
	var w2 bytes.Buffer
	view.Print(&w2, nil)
	assert.NotContains(t, w2.String(), "To inspect a transaction")
}

func TestPrintSessionsList(t *testing.T) {
	t.Parallel()

	var w bytes.Buffer

	PrintSessionsList(&w, nil)
	assert.Contains(t, w.String(), "No sessions recorded in database.")

	w.Reset()
	PrintSessionsList(&w, []*db.SessionRecord{
		{SessionID: "s1", StartTime: time.Now(), SpecName: "spec.json", ConnectionType: "CLIENT", TransactionCount: 2, StressTestCount: 1},
	})
	assert.Contains(t, w.String(), "s1")
	assert.Contains(t, w.String(), "[STRESS]")
}

func TestPrintVisaSessionsList(t *testing.T) {
	t.Parallel()

	var w bytes.Buffer

	PrintVisaSessionsList(&w, nil)
	assert.Contains(t, w.String(), "No recorded sessions with Visa transactions found.")

	w.Reset()
	PrintVisaSessionsList(&w, []*db.SessionRecord{
		{SessionID: "v1", StartTime: time.Now(), SpecName: "visa.json", TransactionCount: 3, SuccessCount: 2},
	})
	assert.Contains(t, w.String(), "v1")
	assert.Contains(t, w.String(), "Approved Tx")
}
