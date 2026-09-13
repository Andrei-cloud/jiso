package app

import (
	"testing"
	"time"

	"jiso/internal/db"
)

func dbTransactionRecordFixture(withResponse bool) *db.EnrichedTransactionRecord {
	rec := &db.EnrichedTransactionRecord{
		ID:               42,
		SessionID:        "sess-1",
		Timestamp:        scenarioFixtureTime().Add(2 * time.Second),
		TxName:           "Purchase",
		TxFilePath:       "/tx/purchases.json",
		TxFileName:       "purchases.json",
		SpecPath:         "/specs/cb.json",
		SpecName:         "cb.json",
		RequestJSON:      `{"mti":"0200"}`,
		RequestRawHEX:    "0800000000000000",
		ProcessingTimeMs: 15,
		Success:          withResponse,
		ResponseCode:     map[bool]string{true: "00", false: ""}[withResponse],
	}
	if withResponse {
		respJSON := `{"mti":"0210"}`
		respHex := "0800200000000000"
		rec.ResponseJSON = &respJSON
		rec.ResponseRawHEX = &respHex
	}

	return rec
}

func TestNewDbStatsViewFromTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		record         *db.EnrichedTransactionRecord
		request        *db.ReconstructedMessage
		response       *db.ReconstructedMessage
		wantNil        bool
		wantHasResp    bool
		wantRequestHEX string
	}{
		{
			name:           "successful review with both messages",
			record:         dbTransactionRecordFixture(true),
			request:        &db.ReconstructedMessage{HEX: "0800000000000000", DescribeText: "MTI 0200"},
			response:       &db.ReconstructedMessage{HEX: "0800200000000000", DescribeText: "MTI 0210"},
			wantHasResp:    true,
			wantRequestHEX: "0800000000000000",
		},
		{
			name:    "failed review without response",
			record:  dbTransactionRecordFixture(false),
			request: &db.ReconstructedMessage{HEX: "0800", DescribeText: "MTI 0200", IsRawFallback: true},
		},
		{
			name:    "nil record yields empty view",
			record:  nil,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewDbStatsViewFromTransaction(tt.record, tt.request, tt.response)

			if got.Mode != "transaction" {
				t.Errorf("Mode = %q, want transaction", got.Mode)
			}
			if tt.wantNil {
				if got.Transaction != nil {
					t.Fatalf("Transaction = %+v, want nil", got.Transaction)
				}

				return
			}

			if got.Transaction.HasResponse != tt.wantHasResp {
				t.Errorf("HasResponse = %v, want %v", got.Transaction.HasResponse, tt.wantHasResp)
			}
			if tt.wantRequestHEX != "" {
				if got.Transaction.Request == nil || got.Transaction.Request.HEX != tt.wantRequestHEX {
					t.Errorf("Request = %+v, want hex %s", got.Transaction.Request, tt.wantRequestHEX)
				}
			}
			if tt.name == "failed review without response" {
				if got.Transaction.Response != nil {
					t.Errorf("Response = %+v, want nil", got.Transaction.Response)
				}
				if got.Transaction.Request == nil || !got.Transaction.Request.RawFallback {
					t.Errorf("Request = %+v, want raw fallback", got.Transaction.Request)
				}
			}
		})
	}
}

func TestDbStatsViewJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		view     *DbStatsView
		wantKeys []string
	}{
		{
			name:     "transaction retrospective shape",
			view:     NewDbStatsViewFromTransaction(dbTransactionRecordFixture(true), &db.ReconstructedMessage{HEX: "0800"}, &db.ReconstructedMessage{DescribeText: "MTI 0210"}),
			wantKeys: []string{"transaction", "has_response", "request", "describe_text"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.view, tt.wantKeys)
		})
	}
}
