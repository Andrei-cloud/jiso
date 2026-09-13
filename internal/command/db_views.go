package command

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"jiso/internal/db"
)

// This file holds the ONE human-readable renderer per database review view,
// shared by the cobra `jiso db stats ...` commands and the REPL `dbstats`
// command (M1 review #23). Both surfaces must keep working; new views go in
// here, not into a per-surface copy.

// SessionOverview is the shared data+render shape for a session review:
// JSON-marshalled under --json, human-printed otherwise.
type SessionOverview struct {
	Session      *db.SessionRecord               `json:"session"`
	Stats        map[string]any                  `json:"stats"`
	StressTests  []*db.StressTestSummaryRecord   `json:"stress_tests,omitempty"`
	Transactions []*db.EnrichedTransactionRecord `json:"transactions,omitempty"`
}

// PrintSessionsList renders the sessions table (both `db stats list` and
// `ctf`-adjacent session listings use their own data, but this one printer).
func PrintSessionsList(w io.Writer, sessions []*db.SessionRecord) {
	if len(sessions) == 0 {
		_, _ = fmt.Fprintln(w, "No sessions recorded in database.")

		return
	}

	_, _ = fmt.Fprintf(w, "\n%-36s | %-19s | %-12s | %-6s | %-16s | %-8s | %-14s\n", "Session ID", "Start Time", "Spec Name", "Mode", "Host:Port", "Total Tx", "Status")
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 123))

	for _, s := range sessions {
		spec, mode := s.SpecName, s.ConnectionType
		if spec == "" {
			spec = "-"
		}
		if mode == "" {
			mode = "-"
		}
		hostPort := "-"
		if s.Host != "" || s.Port != "" {
			hostPort = fmt.Sprintf("%s:%s", s.Host, s.Port)
		}
		status := s.Status
		if status == "" {
			status = "active"
		}
		if s.StressTestCount > 0 {
			status += " [STRESS]"
		}

		_, _ = fmt.Fprintf(w, "%-36s | %-19s | %-12s | %-6s | %-16s | %-8d | %-14s\n",
			s.SessionID,
			s.StartTime.Format("2006-01-02 15:04:05"),
			truncateString(spec, 12),
			mode,
			truncateString(hostPort, 16),
			s.TransactionCount,
			status,
		)
	}

	_, _ = fmt.Fprintln(w)
}

// Print writes the session overview, stress-test and transaction sections in
// that order. When hint is non-nil the "inspect a transaction"
// notice goes to it; callers pass nil to suppress the hint (--quiet).
func (v *SessionOverview) Print(w, hint io.Writer) {
	rec, stats := v.Session, v.Stats

	_, _ = fmt.Fprintf(w, "\nDatabase Statistics for Session: %s\n", rec.SessionID)
	_, _ = fmt.Fprintln(w, "=========================================================================")
	if !rec.StartTime.IsZero() {
		_, _ = fmt.Fprintf(w, "Start Time:             %s\n", rec.StartTime.Format("2006-01-02 15:04:05"))
	}
	if !rec.LastActiveTime.IsZero() {
		_, _ = fmt.Fprintf(w, "Last Active Time:       %s\n", rec.LastActiveTime.Format("2006-01-02 15:04:05"))
	}
	if rec.SpecName != "" {
		_, _ = fmt.Fprintf(w, "Specification:          %s (%s)\n", rec.SpecName, rec.SpecPath)
	}
	if rec.TxFileName != "" {
		_, _ = fmt.Fprintf(w, "Transaction File:       %s (%s)\n", rec.TxFileName, rec.TxFilePath)
	}
	if rec.ConnectionType != "" {
		_, _ = fmt.Fprintf(w, "Connection Mode:        %s\n", rec.ConnectionType)
	}
	if rec.Host != "" || rec.Port != "" {
		_, _ = fmt.Fprintf(w, "Target Host / Port:     %s:%s\n", rec.Host, rec.Port)
	}
	if rec.HeaderType != "" {
		_, _ = fmt.Fprintf(w, "Header Format:          %s\n", rec.HeaderType)
	}
	tlsStr := "Disabled"
	if rec.TLSEnabled {
		tlsStr = "Enabled"
	}
	_, _ = fmt.Fprintf(w, "TLS:                    %s\n", tlsStr)
	if rec.Status != "" {
		_, _ = fmt.Fprintf(w, "Status:                 %s\n", rec.Status)
	}

	_, _ = fmt.Fprintf(w, "\nTotal Transactions:     %v\n", stats["total_transactions"])
	_, _ = fmt.Fprintf(w, "Successful Transactions: %v\n", stats["successful_transactions"])
	_, _ = fmt.Fprintf(w, "Failed Transactions:     %v\n", stats["failed_transactions"])
	_, _ = fmt.Fprintf(w, "Average Processing Time: %v ms\n", stats["average_processing_time_ms"])

	printStressTests(w, v.StressTests)

	if responseCodes, ok := stats["response_code_distribution"].(map[string]int); ok && len(responseCodes) > 0 {
		_, _ = fmt.Fprintf(w, "\nResponse Code Distribution:\n")
		for code, count := range responseCodes {
			_, _ = fmt.Fprintf(w, "  %s: %d\n", code, count)
		}
	}

	printTransactionsTable(w, hint, v.Transactions)
}

// printStressTests renders the per-run stress test summary sections.
func printStressTests(w io.Writer, tests []*db.StressTestSummaryRecord) {
	for i, st := range tests {
		if i == 0 {
			_, _ = fmt.Fprintln(w, "\n-------------------------------------------------------------------------")
			_, _ = fmt.Fprintf(w, "STRESS TEST SUMMARY DETAILS (%d Run(s))\n", len(tests))
			_, _ = fmt.Fprintln(w, "-------------------------------------------------------------------------")
		} else {
			_, _ = fmt.Fprintln(w)
		}

		_, _ = fmt.Fprintf(w, "Run #%d (Worker %s):\n", i+1, st.WorkerID)
		if !st.StartTime.IsZero() && !st.EndTime.IsZero() {
			_, _ = fmt.Fprintf(w, "  Time Window:          %s to %s\n", st.StartTime.Format("2006-01-02 15:04:05"), st.EndTime.Format("15:04:05"))
		}
		_, _ = fmt.Fprintf(w, "  Target / Concurrency: %d TPS (Workers: %d)\n", st.TargetTPS, st.Concurrency)
		_, _ = fmt.Fprintf(w, "  Duration:             %.2f s\n", float64(st.TotalDurationMs)/1000.0)
		_, _ = fmt.Fprintf(w, "  Total Executed:       %d (Passed: %d, Failed: %d)\n", st.TotalTransactions, st.SuccessfulTransactions, st.FailedTransactions)
		_, _ = fmt.Fprintf(w, "  TPS Performance:      Avg: %.1f TPS | Peak: %.1f TPS\n", st.AverageTPS, st.PeakTPS)
		_, _ = fmt.Fprintf(w, "  Latency Profile:      Min: %.2f ms | Mean: %.2f ms | Max: %.2f ms\n", st.MinLatencyMs, st.MeanLatencyMs, st.MaxLatencyMs)
		_, _ = fmt.Fprintf(w, "  Percentiles:          P50: %.2f ms | P90: %.2f ms | P95: %.2f ms | P99: %.2f ms\n", st.P50LatencyMs, st.P90LatencyMs, st.P95LatencyMs, st.P99LatencyMs)

		if st.TransactionsJSON != "" {
			var txList []string
			_ = json.Unmarshal([]byte(st.TransactionsJSON), &txList)
			if len(txList) > 0 {
				_, _ = fmt.Fprintf(w, "  Tested Templates:     %s\n", strings.Join(txList, ", "))
			}
		}
		if st.ResponseCodesJSON != "" {
			var respMap map[string]int
			_ = json.Unmarshal([]byte(st.ResponseCodesJSON), &respMap)
			if len(respMap) > 0 {
				rcPairs := make([]string, 0, len(respMap))
				for code, count := range respMap {
					rcPairs = append(rcPairs, fmt.Sprintf("%s: %d", code, count))
				}
				_, _ = fmt.Fprintf(w, "  Response Codes:       %s\n", strings.Join(rcPairs, ", "))
			}
		}
	}
}

// printTransactionsTable renders the session's transaction rows and the inspect
// hint.
func printTransactionsTable(w, hint io.Writer, txs []*db.EnrichedTransactionRecord) {
	if len(txs) == 0 {
		return
	}

	_, _ = fmt.Fprintf(w, "\nTransactions in Session (%d):\n", len(txs))
	_, _ = fmt.Fprintf(w, "%-6s | %-19s | %-24s | %-10s | %-6s | %-4s\n", "ID", "Timestamp", "Name", "Proc Time", "Result", "Code")
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 82))
	for _, t := range txs {
		resStr := "SUCCESS"
		if !t.Success {
			resStr = "FAIL"
		}
		_, _ = fmt.Fprintf(w, "%-6d | %-19s | %-24s | %-7d ms | %-6s | %-4s\n",
			t.ID,
			t.Timestamp.Format("2006-01-02 15:04:05"),
			truncateString(t.TxName, 24),
			t.ProcessingTimeMs,
			resStr,
			t.ResponseCode,
		)
	}

	if hint != nil {
		_, _ = fmt.Fprintln(hint, "\nTo inspect a transaction: jiso db tx <id>")
	}
}

// PrintTransactionDetail renders the retrospective message review for one
// stored transaction.
func PrintTransactionDetail(w io.Writer, tx *db.EnrichedTransactionRecord) {
	_, _ = fmt.Fprintln(w, "\n=========================================================================")
	_, _ = fmt.Fprintf(w, "TRANSACTION RETROSPECTIVE REVIEW [ID: %d]\n", tx.ID)
	_, _ = fmt.Fprintln(w, "=========================================================================")
	_, _ = fmt.Fprintf(w, "Session ID:        %s\n", tx.SessionID)
	_, _ = fmt.Fprintf(w, "Transaction Name:  %s\n", tx.TxName)
	_, _ = fmt.Fprintf(w, "Timestamp:         %s\n", tx.Timestamp.Format("2006-01-02 15:04:05"))
	_, _ = fmt.Fprintf(w, "Processing Time:   %d ms\n", tx.ProcessingTimeMs)
	resultStr := "SUCCESS"
	if !tx.Success {
		resultStr = "FAILED"
	}
	_, _ = fmt.Fprintf(w, "Result:            %s (Response Code: %s)\n", resultStr, tx.ResponseCode)
	if tx.SpecName != "" {
		_, _ = fmt.Fprintf(w, "Specification:     %s (%s)\n", tx.SpecName, tx.SpecPath)
	}
	if tx.TxFileName != "" {
		_, _ = fmt.Fprintf(w, "Template File:     %s (%s)\n", tx.TxFileName, tx.TxFilePath)
	}

	_, _ = fmt.Fprintf(w, "\n--- REQUEST MESSAGE ---\n")
	printReconstructedMessage(w, "Request", tx.RequestJSON, tx.RequestRawHEX, tx.SpecPath)

	_, _ = fmt.Fprintf(w, "\n--- RESPONSE MESSAGE ---\n")
	if tx.ResponseJSON != nil || tx.ResponseRawHEX != nil {
		printReconstructedMessage(w, "Response", derefStringPtr(tx.ResponseJSON), derefStringPtr(tx.ResponseRawHEX), tx.SpecPath)
	} else {
		_, _ = fmt.Fprintln(w, "(No response message recorded for this transaction)")
	}
}

// printReconstructedMessage reconstructs a stored message from its JSON/hex
// columns and writes its HEX and parsed-ISO8583 sections under the given label.
func printReconstructedMessage(w io.Writer, label, jsonStr, hexStr, specPath string) {
	recon, err := db.Reconstruct(jsonStr, hexStr, specPath)
	if err != nil {
		_, _ = fmt.Fprintf(w, "Error reconstructing %s: %v\n", strings.ToLower(label), err)

		return
	}

	if recon.HEX != "" {
		_, _ = fmt.Fprintf(w, "\n%s HEX:\n%s\n", label, recon.HEX)
	}
	if recon.DescribeText != "" {
		_, _ = fmt.Fprintf(w, "%s Parsed ISO8583 Message:\n%s\n", label, recon.DescribeText)
	}
}

// derefStringPtr returns the string a pointer refers to, or "" when nil.
func derefStringPtr(s *string) string {
	if s != nil {
		return *s
	}

	return ""
}

// PrintVisaSessionsList renders the Visa-session eligibility table shared by
// `jiso ctf list` and the REPL `ctf list` (M1 review #23).
func PrintVisaSessionsList(w io.Writer, sessions []*db.SessionRecord) {
	if len(sessions) == 0 {
		_, _ = fmt.Fprintln(w, "No recorded sessions with Visa transactions found.")

		return
	}

	_, _ = fmt.Fprintf(w, "\n%-36s | %-19s | %-12s | %-10s | %-10s\n", "Session ID", "Start Time", "Spec", "Total Tx", "Approved Tx")
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 97))

	for _, s := range sessions {
		spec := s.SpecName
		if spec == "" {
			spec = "-"
		}
		_, _ = fmt.Fprintf(w, "%-36s | %-19s | %-12s | %-10d | %-10d\n",
			s.SessionID,
			s.StartTime.Format("2006-01-02 15:04:05"),
			truncateString(spec, 12),
			s.TransactionCount,
			s.SuccessCount,
		)
	}

	_, _ = fmt.Fprintln(w)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
