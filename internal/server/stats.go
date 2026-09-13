package server

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// FallbackRouteName is the routeStats key the engine records for requests
// that matched no configured route (the catch-all RC-12 fallback path in
// handleConn). Consumers (app.Stats, the TUI §G stats card) derive
// matched-vs-fallback from this single name.
const FallbackRouteName = "Catch-all Fallback"

// Stats tracks mock server traffic statistics
type Stats struct {
	mu          sync.RWMutex
	startTime   time.Time
	totalServed int64
	mtiStats    map[string]int64
	routeStats  map[string]int64
	codeStats   map[string]int64
	lastTotal   int64
	lastTime    time.Time
	instantTps  float64
	peakInstTps float64
	dropped     int64 // atomic: drop_connection route hits (SCR-507)
	requestErrs int64 // atomic: request unpack failures (SCR-507)
}

// NewStats starts the clock at construction: the rates are measured from here, so
// a server built early and started much later reports its rate over the time it
// actually ran rather than over the idle stretch before it.
func NewStats() *Stats {
	return &Stats{
		startTime:  time.Now(),
		lastTime:   time.Now(),
		mtiStats:   make(map[string]int64),
		routeStats: make(map[string]int64),
		codeStats:  make(map[string]int64),
	}
}

// RecordMessage counts one served message against the total and each dimension it
// carries. A dimension the message has no value for is skipped rather than
// counted under an empty key, which would otherwise show up in the server view as
// a blank row that reads like a data problem.
func (s *Stats) RecordMessage(mti, routeName, responseCode string) {
	atomic.AddInt64(&s.totalServed, 1)

	s.mu.Lock()
	defer s.mu.Unlock()

	if mti != "" {
		s.mtiStats[mti]++
	}
	if routeName != "" {
		s.routeStats[routeName]++
	}
	if responseCode != "" {
		s.codeStats[responseCode]++
	}

	// Update sliding window TPS
	now := time.Now()
	deltaTime := now.Sub(s.lastTime).Seconds()
	if deltaTime >= 0.5 {
		deltaTotal := s.totalServed - s.lastTotal
		inst := float64(deltaTotal) / deltaTime
		if s.instantTps == 0 {
			s.instantTps = inst
		} else {
			s.instantTps = 0.7*inst + 0.3*s.instantTps
		}
		if s.instantTps > s.peakInstTps {
			s.peakInstTps = s.instantTps
		}
		s.lastTotal = s.totalServed
		s.lastTime = now
	}
}

// Reset starts the counters and the clock over, so the figures describe the
// interval from now rather than the whole life of a server that has been running
// long enough to average itself flat.
func (s *Stats) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTime = time.Now()
	s.lastTime = time.Now()
	s.totalServed = 0
	s.lastTotal = 0
	s.instantTps = 0
	s.peakInstTps = 0
	s.mtiStats = make(map[string]int64)
	s.routeStats = make(map[string]int64)
	s.codeStats = make(map[string]int64)
	atomic.StoreInt64(&s.dropped, 0)
	atomic.StoreInt64(&s.requestErrs, 0)
}

// RecordDrop counts a matched route that dropped the connection
// (drop_connection routes never serve a response).
func (s *Stats) RecordDrop() {
	atomic.AddInt64(&s.dropped, 1)
}

// RecordRequestError counts a request payload that failed to unpack.
func (s *Stats) RecordRequestError() {
	atomic.AddInt64(&s.requestErrs, 1)
}

// Dropped returns the number of drop_connection hits since the last Reset.
func (s *Stats) Dropped() int64 {
	return atomic.LoadInt64(&s.dropped)
}

// RequestErrors returns the number of failed request unpacks since Reset.
func (s *Stats) RequestErrors() int64 {
	return atomic.LoadInt64(&s.requestErrs)
}

// TotalServed returns the number of messages served since the last Reset.
func (s *Stats) TotalServed() int64 {
	return atomic.LoadInt64(&s.totalServed)
}

// StartTime returns when the current stats window started.
func (s *Stats) StartTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.startTime
}

// InstantTPS returns the smoothed instantaneous throughput.
func (s *Stats) InstantTPS() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.instantTps
}

// PeakTPS returns the highest smoothed instantaneous throughput observed.
func (s *Stats) PeakTPS() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.peakInstTps
}

// MTIStats returns a copy of the per-MTI served-message counts.
func (s *Stats) MTIStats() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return copyCounts(s.mtiStats)
}

// RouteStats returns a copy of the per-route served-message counts.
func (s *Stats) RouteStats() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return copyCounts(s.routeStats)
}

// CodeStats returns a copy of the per-response-code served-message counts.
func (s *Stats) CodeStats() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return copyCounts(s.codeStats)
}

func copyCounts(counts map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(counts))
	for key, count := range counts {
		out[key] = count
	}

	return out
}

// PrintSummary writes the human-readable run summary: port, header, active
// connections, totals, and the average and instantaneous rates. It reads the maps
// under a read lock, so taking a summary while the server is serving cannot tear
// a count.
func (s *Stats) PrintSummary(port, headerType string, activeConns int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := atomic.LoadInt64(&s.totalServed)
	elapsed := time.Since(s.startTime)
	avgTps := 0.0
	if elapsed.Seconds() > 0 {
		avgTps = float64(total) / elapsed.Seconds()
	}

	fmt.Println("\n================================================================================")
	fmt.Println("                          EMBEDDED MOCK SERVER STATISTICS")
	fmt.Println("================================================================================")
	fmt.Printf("Status:                 Running (Port: %s, Header: %s)\n", port, headerType)
	fmt.Printf("Runtime Duration:       %s\n", elapsed.Round(time.Second))
	fmt.Printf("Active TCP Connections: %d\n", activeConns)
	fmt.Printf("Total Served Messages:  %d\n", total)
	fmt.Printf("Throughput Performance: Instant TPS: %.1f | Peak TPS: %.1f | Avg TPS: %.1f\n", s.instantTps, s.peakInstTps, avgTps)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("SERVED BY MOCK ROUTE")
	fmt.Println("--------------------------------------------------------------------------------")
	if len(s.routeStats) == 0 {
		fmt.Println("  (No matched routes yet)")
	} else {
		for routeName, count := range s.routeStats {
			pct := 0.0
			if total > 0 {
				pct = float64(count) / float64(total) * 100.0
			}
			fmt.Printf("  Route %-30s %-10d (%6.2f%%)\n", `"`+routeName+`":`, count, pct)
		}
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("SERVED BY MTI")
	fmt.Println("--------------------------------------------------------------------------------")
	if len(s.mtiStats) == 0 {
		fmt.Println("  (No messages processed yet)")
	} else {
		for mti, count := range s.mtiStats {
			pct := 0.0
			if total > 0 {
				pct = float64(count) / float64(total) * 100.0
			}
			fmt.Printf("  MTI %-32s %-10d (%6.2f%%)\n", `"`+mti+`":`, count, pct)
		}
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("RESPONSE CODE BREAKDOWN (DE 39)")
	fmt.Println("--------------------------------------------------------------------------------")
	if len(s.codeStats) == 0 {
		fmt.Println("  (No response codes recorded yet)")
	} else {
		for code, count := range s.codeStats {
			pct := 0.0
			if total > 0 {
				pct = float64(count) / float64(total) * 100.0
			}
			var label string
			if code == "00" {
				label = `"00" (Approved)`
			} else {
				label = `"` + code + `"`
			}
			fmt.Printf("  Code %-31s %-10d (%6.2f%%)\n", label+":", count, pct)
		}
	}
	fmt.Println("================================================================================")
}
