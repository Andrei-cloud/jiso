package app

import (
	"testing"

	"jiso/internal/server"
)

func TestNewServerStatsFromServerStats(t *testing.T) {
	t.Parallel()

	tracked := server.NewStats()
	tracked.RecordMessage("0200", "purchase-route", "00")
	tracked.RecordMessage("0200", "purchase-route", "05")
	tracked.RecordMessage("0800", "echo-route", "00")

	tests := []struct {
		name        string
		stats       *server.Stats
		wantServed  int64
		wantMTIs    map[string]int64
		wantCodes   map[string]int64
		wantAverage bool
	}{
		{
			name:       "nil tracker keeps metadata only",
			stats:      nil,
			wantServed: 0,
		},
		{
			name:       "fresh tracker has no traffic",
			stats:      server.NewStats(),
			wantServed: 0,
		},
		{
			name:        "recorded traffic is snapshotted",
			stats:       tracked,
			wantServed:  3,
			wantMTIs:    map[string]int64{"0200": 2, "0800": 1},
			wantCodes:   map[string]int64{"00": 2, "05": 1},
			wantAverage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewServerStatsFromServerStats(tt.stats, "8583", "binary2", true, 2)

			if got.Running != true || got.Port != "8583" || got.HeaderType != "binary2" || got.ActiveConnections != 2 {
				t.Errorf("metadata = %+v, want running/8583/binary2/2", got)
			}
			if got.TotalServed != tt.wantServed {
				t.Errorf("TotalServed = %d, want %d", got.TotalServed, tt.wantServed)
			}
			for mti, want := range tt.wantMTIs {
				if got.MTICounts[mti] != want {
					t.Errorf("MTICounts[%s] = %d, want %d", mti, got.MTICounts[mti], want)
				}
			}
			for code, want := range tt.wantCodes {
				if got.ResponseCodes[code] != want {
					t.Errorf("ResponseCodes[%s] = %d, want %d", code, got.ResponseCodes[code], want)
				}
			}
			if tt.wantAverage && got.AverageTPS <= 0 {
				t.Errorf("AverageTPS = %v, want > 0", got.AverageTPS)
			}
		})
	}
}

func TestServerStatsJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tracked := server.NewStats()
	tracked.RecordMessage("0200", "purchase-route", "00")

	tests := []struct {
		name     string
		stats    *ServerStats
		wantKeys []string
	}{
		{
			name:     "empty server shape",
			stats:    NewServerStatsFromServerStats(server.NewStats(), "8583", "ascii4", false, 0),
			wantKeys: []string{"running", "port", "header_type", "uptime"},
		},
		{
			name:     "served traffic shape",
			stats:    NewServerStatsFromServerStats(tracked, "8583", "binary2", true, 1),
			wantKeys: []string{"total_served", "mti_counts", "route_counts", "response_codes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.stats, tt.wantKeys)
		})
	}
}
