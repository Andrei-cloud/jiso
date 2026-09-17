package app

import (
	"time"

	"jiso/internal/server"
)

// ServerStats is the JSON-serializable view of the embedded mock server
// traffic statistics. It carries the same data the legacy
// ServerStats.PrintSummary renders for `server stats`
// (internal/command/cmd_server.go), so CLI --json and the future TUI
// render the same numbers.
type ServerStats struct {
	Running           bool             `json:"running"`
	Port              string           `json:"port"`
	HeaderType        string           `json:"header_type"`
	PID               int              `json:"pid,omitempty"`
	StartTime         time.Time        `json:"start_time"`
	Uptime            time.Duration    `json:"uptime"`
	ActiveConnections int              `json:"active_connections"`
	TotalServed       int64            `json:"total_served"`
	InstantTPS        float64          `json:"instant_tps"`
	PeakTPS           float64          `json:"peak_tps"`
	AverageTPS        float64          `json:"average_tps"`
	RouteCounts       map[string]int64 `json:"route_counts,omitempty"`
	MTICounts         map[string]int64 `json:"mti_counts,omitempty"`
	ResponseCodes     map[string]int64 `json:"response_codes,omitempty"`
	// (TUI §G stats card): matched-vs-fallback split and the two
	// non-serving outcomes, derived from the tracker so the page never
	// re-derives them. Matched excludes server.FallbackRouteName from the
	// served total; omitempty keeps them out of JSON that predates them.
	Matched       int64 `json:"matched,omitempty"`
	Dropped       int64 `json:"dropped,omitempty"`
	RequestErrors int64 `json:"request_errors,omitempty"`
	// SnapshotAt is when this view was captured; only the
	// side-channel snapshot file sets it (in-process callers leave it
	// nil, and omitempty keeps it out of their JSON).
	SnapshotAt *time.Time `json:"snapshot_at,omitempty"`
}

// NewServerStatsFromServerStats snapshots the mock server statistics
// tracker together with the engine metadata PrintSummary receives at the
// call site (port, header type, running flag, active TCP connections).
// AverageTPS is derived as total served over the observed uptime, matching
// the legacy summary line. A nil tracker yields the metadata-only view.
func NewServerStatsFromServerStats(
	stats *server.Stats,
	port string,
	headerType string,
	running bool,
	activeConnections int,
) *ServerStats {
	view := &ServerStats{
		Running:           running,
		Port:              port,
		HeaderType:        headerType,
		ActiveConnections: activeConnections,
	}

	if stats == nil {
		return view
	}

	view.StartTime = stats.StartTime()
	view.Uptime = time.Since(view.StartTime)
	view.TotalServed = stats.TotalServed()
	view.InstantTPS = stats.InstantTPS()
	view.PeakTPS = stats.PeakTPS()
	if view.Uptime.Seconds() > 0 {
		view.AverageTPS = float64(view.TotalServed) / view.Uptime.Seconds()
	}
	view.RouteCounts = stats.RouteStats()
	view.MTICounts = stats.MTIStats()
	view.ResponseCodes = stats.CodeStats()
	view.Dropped = stats.Dropped()
	view.RequestErrors = stats.RequestErrors()
	view.Matched = view.TotalServed - view.RouteCounts[server.FallbackRouteName]

	return view
}
