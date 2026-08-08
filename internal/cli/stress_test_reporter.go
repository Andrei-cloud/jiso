package cli

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"jiso/internal/config"
)

func (w *stressTestWorker) printSummary(finalTps float64) {
	w.mu.Lock()
	total := w.successful + w.failed
	latenciesCopy := make([]time.Duration, len(w.latencies))
	copy(latenciesCopy, w.latencies)
	respCodesCopy := make(map[string]int, len(w.respCodes))
	for k, v := range w.respCodes {
		respCodesCopy[k] = v
	}
	// Copy txStats
	txStatsCopy := make(map[string]*txStats)
	for k, v := range w.txStats {
		ts := &txStats{
			successful: v.successful,
			failed:     v.failed,
			respCodes:  make(map[string]int),
			latencies:  make([]time.Duration, len(v.latencies)),
		}
		for rk, rv := range v.respCodes {
			ts.respCodes[rk] = rv
		}
		copy(ts.latencies, v.latencies)
		txStatsCopy[k] = ts
	}
	startTimeCopy := w.startTime
	endTimeCopy := w.endTime
	namesCopy := make([]string, len(w.names))
	copy(namesCopy, w.names)
	peakInstantTpsCopy := w.peakInstantTps
	w.mu.Unlock()

	if total == 0 {
		fmt.Printf("\nWorker %s: Stress test completed but no transactions were executed.\n", w.id)
		return
	}

	// Sort latencies to calculate percentiles
	sort.Slice(latenciesCopy, func(i, j int) bool {
		return latenciesCopy[i] < latenciesCopy[j]
	})

	var minLatency, maxLatency, meanLatency, p50, p90, p95, p99 time.Duration
	var totalDuration time.Duration

	if len(latenciesCopy) > 0 {
		minLatency = latenciesCopy[0]
		maxLatency = latenciesCopy[len(latenciesCopy)-1]
		for _, d := range latenciesCopy {
			totalDuration += d
		}
		meanLatency = totalDuration / time.Duration(len(latenciesCopy))

		p50 = percentile(latenciesCopy, 0.50)
		p90 = percentile(latenciesCopy, 0.90)
		p95 = percentile(latenciesCopy, 0.95)
		p99 = percentile(latenciesCopy, 0.99)
	}

	// Get response timeout budget
	timeout := config.GetConfig().GetResponseTimeout()

	// Latency budgets
	var satisfactory, tolerable, exceeded int
	for _, d := range latenciesCopy {
		switch {
		case d <= timeout/2:
			satisfactory++
		case d <= timeout:
			tolerable++
		default:
			exceeded++
		}
	}

	// Build histogram
	type bucket struct {
		label string
		min   time.Duration
		max   time.Duration
		count int
	}
	buckets := []bucket{
		{label: "  0ms -  10ms", min: 0, max: 10 * time.Millisecond},
		{label: " 10ms -  50ms", min: 10 * time.Millisecond, max: 50 * time.Millisecond},
		{label: " 50ms - 100ms", min: 50 * time.Millisecond, max: 100 * time.Millisecond},
		{label: "100ms - 250ms", min: 100 * time.Millisecond, max: 250 * time.Millisecond},
		{label: "250ms - 500ms", min: 250 * time.Millisecond, max: 500 * time.Millisecond},
		{label: "500ms - 1.0s ", min: 500 * time.Millisecond, max: 1000 * time.Millisecond},
		{label: " 1.0s - 2.5s ", min: 1000 * time.Millisecond, max: 2500 * time.Millisecond},
		{label: " 2.5s - 5.0s ", min: 2500 * time.Millisecond, max: 5000 * time.Millisecond},
		{label: "    > 5.0s   ", min: 5000 * time.Millisecond, max: 999999 * time.Hour},
	}

	for _, d := range latenciesCopy {
		for i := range buckets {
			if d > buckets[i].min && d <= buckets[i].max {
				buckets[i].count++
				break
			}
		}
	}

	// Print the output
	fmt.Println("\n================================================================================")
	fmt.Printf("                          STRESS TEST SUMMARY - Worker %s\n", w.id)
	fmt.Println("================================================================================")
	fmt.Printf("Session ID:             %s\n", w.sessionID)
	fmt.Printf("Start Time:             %s\n", startTimeCopy.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("End Time:               %s\n", endTimeCopy.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Selected Transactions:  %s\n", strings.Join(namesCopy, ", "))
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("ALL TESTING SUMMARY")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Target TPS:             %-10d Concurrency (Workers): %-10d\n", w.targetTps, w.numWorkers)
	fmt.Printf("Instant TPS (Peak):     %-10.1f Average TPS:           %-10.1f\n", peakInstantTpsCopy, finalTps)
	fmt.Printf("Total Test Duration:    %-10s\n", w.duration)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Transaction Counts:\n")
	fmt.Printf("  Total Executions:     %-10d\n", total)
	fmt.Printf("  Successful:           %-10d (%6.2f%%)\n", w.successful, float64(w.successful)/float64(total)*100.0)
	fmt.Printf("  Failed:               %-10d (%6.2f%%)\n", w.failed, float64(w.failed)/float64(total)*100.0)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Response Code Breakdown:\n")
	// For predictable ordering, print success code "00" first if present, then others
	if count, ok := respCodesCopy["00"]; ok {
		fmt.Printf("  Code %-16s %-10d (%6.2f%%)\n", `"00":`, count, float64(count)/float64(total)*100.0)
	}
	for code, count := range respCodesCopy {
		if code == "00" {
			continue
		}
		fmt.Printf("  Code %-16s %-10d (%6.2f%%)\n", `"`+code+`":`, count, float64(count)/float64(total)*100.0)
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Latency Profile:\n")
	fmt.Printf(
		"  Min Latency:          %-15s Median (p50):          %-15s\n",
		minLatency.Round(time.Microsecond),
		p50.Round(time.Microsecond),
	)
	fmt.Printf(
		"  Max Latency:          %-15s p90 Percentile:        %-15s\n",
		maxLatency.Round(time.Microsecond),
		p90.Round(time.Microsecond),
	)
	fmt.Printf(
		"  Mean Latency:         %-15s p95 Percentile:        %-15s\n",
		meanLatency.Round(time.Microsecond),
		p95.Round(time.Microsecond),
	)
	fmt.Printf("                                       p99 Percentile:        %-15s\n", p99.Round(time.Microsecond))
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Latency Budget (Timeout: %s):\n", timeout)
	fmt.Printf("  Satisfactory (<= 50%% of timeout):  %-10d (%6.2f%%)\n", satisfactory, float64(satisfactory)/float64(total)*100.0)
	fmt.Printf("  Tolerable    (51%%-100%% of timeout): %-10d (%6.2f%%)\n", tolerable, float64(tolerable)/float64(total)*100.0)
	fmt.Printf("  Exceeded     (> 100%% of timeout):   %-10d (%6.2f%%)\n", exceeded, float64(exceeded)/float64(total)*100.0)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Latency Histogram:\n")

	// Find max count to scale the bar
	maxCount := 0
	for _, b := range buckets {
		if b.count > maxCount {
			maxCount = b.count
		}
	}

	for _, b := range buckets {
		if b.count == 0 {
			continue // skip empty buckets to reduce noise
		}
		barLength := 0
		if maxCount > 0 {
			barLength = (b.count * 30) / maxCount
		}
		bar := strings.Repeat("█", barLength)
		fmt.Printf("  [%s]: %-30s %-10d (%6.2f%%)\n", b.label, bar, b.count, float64(b.count)/float64(total)*100.0)
	}

	fmt.Println("================================================================================")
	fmt.Println("                    PER TRANSACTION TYPE DETAILS")
	fmt.Println("================================================================================")
	for _, name := range namesCopy {
		ts := txStatsCopy[name]
		var tsTotal, tsSuccessful, tsFailed int
		var tsRespCodes map[string]int
		var tsLatencies []time.Duration
		if ts != nil {
			tsSuccessful = ts.successful
			tsFailed = ts.failed
			tsTotal = tsSuccessful + tsFailed
			tsRespCodes = ts.respCodes
			tsLatencies = ts.latencies
		} else {
			tsRespCodes = make(map[string]int)
		}

		fmt.Printf("Transaction: %s\n", name)
		fmt.Printf("  Total Executions:     %-10d\n", tsTotal)
		if tsTotal > 0 {
			fmt.Printf("  Successful:           %-10d (%6.2f%%)\n", tsSuccessful, float64(tsSuccessful)/float64(tsTotal)*100.0)
			fmt.Printf("  Failed:               %-10d (%6.2f%%)\n", tsFailed, float64(tsFailed)/float64(tsTotal)*100.0)

			// Response code breakdown
			fmt.Printf("  Response Code Breakdown:\n")
			if count, ok := tsRespCodes["00"]; ok {
				fmt.Printf("    Code %-14s %-10d (%6.2f%%)\n", `"00":`, count, float64(count)/float64(tsTotal)*100.0)
			}
			var sortedCodes []string
			for code := range tsRespCodes {
				if code != "00" {
					sortedCodes = append(sortedCodes, code)
				}
			}
			sort.Strings(sortedCodes)
			for _, code := range sortedCodes {
				count := tsRespCodes[code]
				fmt.Printf("    Code %-14s %-10d (%6.2f%%)\n", `"`+code+`":`, count, float64(count)/float64(tsTotal)*100.0)
			}

			// Latency Profile
			sort.Slice(tsLatencies, func(i, j int) bool {
				return tsLatencies[i] < tsLatencies[j]
			})
			var tsMin, tsMax, tsMean, tsP50, tsP90, tsP95, tsP99 time.Duration
			var tsTotalDuration time.Duration
			if len(tsLatencies) > 0 {
				tsMin = tsLatencies[0]
				tsMax = tsLatencies[len(tsLatencies)-1]
				for _, d := range tsLatencies {
					tsTotalDuration += d
				}
				tsMean = tsTotalDuration / time.Duration(len(tsLatencies))
				tsP50 = percentile(tsLatencies, 0.50)
				tsP90 = percentile(tsLatencies, 0.90)
				tsP95 = percentile(tsLatencies, 0.95)
				tsP99 = percentile(tsLatencies, 0.99)
			}
			fmt.Printf("  Latency Profile:\n")
			fmt.Printf(
				"    Min Latency:        %-15s Median (p50):          %-15s\n",
				tsMin.Round(time.Microsecond),
				tsP50.Round(time.Microsecond),
			)
			fmt.Printf(
				"    Max Latency:        %-15s p90 Percentile:        %-15s\n",
				tsMax.Round(time.Microsecond),
				tsP90.Round(time.Microsecond),
			)
			fmt.Printf(
				"    Mean Latency:       %-15s p95 Percentile:        %-15s\n",
				tsMean.Round(time.Microsecond),
				tsP95.Round(time.Microsecond),
			)
			fmt.Printf("                                         p99 Percentile:        %-15s\n", tsP99.Round(time.Microsecond))
		} else {
			fmt.Printf("  Successful:           0          (  0.00%%\n")
			fmt.Printf("  Failed:               0          (  0.00%%\n")
		}
		fmt.Println("--------------------------------------------------------------------------------")
	}
	fmt.Println("================================================================================")
}

func percentile(sorted []time.Duration, pct float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if pct <= 0.0 {
		return sorted[0]
	}
	if pct >= 1.0 {
		return sorted[len(sorted)-1]
	}
	idx := float64(len(sorted)-1) * pct
	low := int(math.Floor(idx))
	high := int(math.Ceil(idx))
	if low == high {
		return sorted[low]
	}
	diff := idx - float64(low)
	return time.Duration(float64(sorted[low]) + diff*float64(sorted[high]-sorted[low]))
}
