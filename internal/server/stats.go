package server

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ServerStats tracks mock server traffic statistics
type ServerStats struct {
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
}

func NewServerStats() *ServerStats {
	return &ServerStats{
		startTime:  time.Now(),
		lastTime:   time.Now(),
		mtiStats:   make(map[string]int64),
		routeStats: make(map[string]int64),
		codeStats:  make(map[string]int64),
	}
}

func (s *ServerStats) RecordMessage(mti string, routeName string, responseCode string) {
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

func (s *ServerStats) Reset() {
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
}

func (s *ServerStats) PrintSummary(port string, headerType string, activeConns int) {
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
			label := code
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
