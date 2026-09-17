// root_workers_state.go derives the §H WorkersState snapshot: table rows
// from the cache, the sparkline ring + label, the net-health line, per-
// active-worker progress rows (ETA → elapsed morph), and the finished
// stress summary. Everything is display strings derived with the
// injectable clock — the page never touches internal/app or reads it.
package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// syncWorkers pushes a fresh §H snapshot into the canonical page
// instance (Update-wrapper placement mirrors syncServer).
func (m *RootModel) syncWorkers() {
	if m.workers == nil {
		return
	}
	m.workers.SetState(m.workersState())
}

// workersState assembles the snapshot: live App views enrich the cache
// first, then rows are ordered longest-runtime-first (the App's own
// Workers() order) and turned into display strings.
func (m *RootModel) workersState() pages.WorkersState {
	m.enrichWorkerRows()
	sep := m.workerSep()

	rows := make([]*workerRowState, 0, len(m.workerRows))
	for _, r := range m.workerRows {
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := m.rowRuntime(rows[i]), m.rowRuntime(rows[j])
		if ri == rj {
			return rows[i].id < rows[j].id
		}

		return ri > rj
	})

	st := pages.WorkersState{StatusLine: m.workersStatus, Summary: m.workersSummary}
	for _, r := range rows {
		st.Workers = append(st.Workers, m.workerRow(r))
		if p, ok := m.progressRow(r); ok {
			st.Progress = append(st.Progress, p)
		}
	}
	st.Sparkline = pages.NormalizeSparkline(m.workerRing)
	st.SparkLabel = m.sparkLabel(rows)
	st.Net = m.netLine(sep)

	return st
}

// enrichWorkerRows folds the App's live WorkerViews into the cache:
// type/names/THR/interval and the running-vs-ramping distinction.
// Terminal cache rows are never re-opened, and rows whose removal is
// still in flight keep their last truth.
func (m *RootModel) enrichWorkerRows() {
	if m.app == nil {
		return
	}
	for _, v := range m.app.Workers() {
		r := m.workerRowFor(v.ID)
		if r.terminal {
			continue
		}
		r.stress = v.Type == "stress_test"
		r.txn = v.Name
		r.thr = v.Workers
		r.interval = v.Interval
		r.done = v.Successful + v.Failed
		r.okCount, r.failCount = v.Successful, v.Failed
		r.consec = v.ConsecutiveFailures
		r.curTps = v.CurrentTPS
		r.start = m.now().Add(-v.Runtime)
		r.status = pages.StatusRunning
		if r.stress {
			if p, ok := m.workerRuns[v.ID]; ok && p.ramp > 0 && v.RampUpProgress < 100 && v.Runtime < p.ramp {
				r.status = pages.StatusRamping
			}
		}
	}
}

// rowRuntime reports the RUNTIME value of a row: frozen at the last
// observed duration once terminal, extrapolated from the fake-clock
// origin while live.
func (m *RootModel) rowRuntime(r *workerRowState) time.Duration {
	if r.terminal && !r.lastSample.IsZero() {
		return r.lastSample.Sub(r.start)
	}

	return m.now().Sub(r.start)
}

// workerRow renders one cache row into display strings.
func (m *RootModel) workerRow(r *workerRowState) pages.WorkerRow {
	runtime := m.rowRuntime(r)
	row := pages.WorkerRow{
		ID: r.id, Type: "background", Status: r.status,
		Txn:     m.dashIfRoot(r.txn),
		Thr:     strconv.Itoa(max(r.thr, 0)),
		Runtime: pages.FormatUptime(&runtime),
		OKFail:  countCell(r.okCount) + " / " + countCell(r.failCount),
	}
	if r.stress {
		row.Type = workerKindStress
		row.IntervalTPS = m.stressTPSCell(r)
	} else if r.interval > 0 {
		row.IntervalTPS = r.interval.String()
	}
	row.Circuit = circuitCell(r, app.CircuitBreakerFailures)

	return row
}

// circuitCell is the §H CIRCUIT column, empty unless there is something
// to report: a zero-failure counter prints nothing, accumulating failures
// earn a cell, and a trip keeps its word and count.
func circuitCell(r *workerRowState, limit int) string {
	consec := min(r.consec, limit)
	if r.status == pages.StatusCircuitBroke {
		return "TRIPPED (" + strconv.Itoa(consec) + "/" + strconv.Itoa(limit) + ")"
	}
	if consec == 0 {
		return ""
	}

	return strconv.Itoa(consec) + "/" + strconv.Itoa(limit)
}

// stressTPSCell is the INTERVAL/TPS cell of a stress row: "60→120 tps"
// (current → target); unknown parts render the dash.
func (m *RootModel) stressTPSCell(r *workerRowState) string {
	p, ok := m.workerRuns[r.id]
	if !ok {
		return ""
	}
	cur := r.curTps
	if cur <= 0 && r.hadSample {
		cur = r.lastSampleRate()
	}

	return formatTps(cur) + "\xe2\x86\x92" + strconv.Itoa(p.targetTps) + " tps"
}

// lastSampleRate falls back to the newest ring sample (the row's own
// curTps is refreshed from the ring on every progress event).
func (r *workerRowState) lastSampleRate() float64 { return r.curTps }

// progressRow derives one per-active-worker progress line (superfile
// Processes pattern); a terminal stress row morphs its ETA to the run's
// elapsed time. bgsend rows have unknown totals: they render the
// degraded count line (Pct < 0 — never a spinner, never a fake 100%).
func (m *RootModel) progressRow(r *workerRowState) (pages.ProgressRow, bool) {
	if !r.stress && r.terminal {
		return pages.ProgressRow{}, false
	}
	p, hasRun := m.workerRuns[r.id]
	if !r.stress && !hasRun && r.total <= 0 {
		return pages.ProgressRow{ID: r.id, Pct: -1, Counts: countCell(r.done) + " sent"}, true
	}
	if r.stress && !hasRun {
		return pages.ProgressRow{}, false
	}
	expected := p.expectedCount()
	line := pages.ProgressRow{ID: r.id, Pct: -1, Counts: countCell(r.done) + " sent"}
	if expected <= 0 {
		expected = r.total // the total the events themselves carried
	}
	if expected > 0 {
		pct := min(max(r.done*100/expected, 0), 100)
		line = pages.ProgressRow{
			ID: r.id, Pct: pct,
			Counts: countCell(r.done) + "/" + countCell(expected) + " sent",
		}
	}
	runtime := m.rowRuntime(r)
	if r.terminal {
		line.ETA = "elapsed " + hhmmss(runtime)
	} else if total := p.ramp + p.duration; total > runtime {
		line.ETA = "ETA " + hhmmss(total-runtime)
	}

	return line, true
}

// expectedCount is the wireframe's expected-message denominator: the
// App's own expected-requests formula targetTps × (ramp + duration).
func (p workerRunParams) expectedCount() int {
	secs := int((p.ramp + p.duration).Seconds())
	if secs < 1 {
		secs = 1
	}

	return p.targetTps * secs
}

// sparkLabel names the sparkline's source worker and its inst/avg TPS
// ("" when the ring is empty): "w-2 inst 118.4 · avg 96.2".
func (m *RootModel) sparkLabel(rows []*workerRowState) string {
	if len(m.workerRing) == 0 {
		return ""
	}
	id := ""
	for _, r := range rows {
		if r.stress && r.done > 0 {
			id = r.id
		}
	}
	sum := 0.0
	for _, s := range m.workerRing {
		sum += s
	}

	return id + " inst " + formatTps(m.workerRing[len(m.workerRing)-1]) +
		m.workerSep() + "avg " + formatTps(sum/float64(len(m.workerRing)))
}

// netLine derives the wireframe's net strip from the App's networking
// metrics: wire volume first, then reconnects, breaker trips and
// retriable/permanent errors; "" when all zero. Reconnects is the
// retry signal the app can observe.
func (m *RootModel) netLine(sep string) string {
	if m.app == nil {
		return ""
	}
	ns := m.app.NetworkingStats()
	if ns == nil {
		return ""
	}
	var parts []string
	if tx, rx := ns.TxBytes(), ns.RxBytes(); tx > 0 || rx > 0 {
		parts = append(parts, "tx "+humanBytes(tx), "rx "+humanBytes(rx))
	}
	if n := ns.ReconnectAttempts(); n > 0 {
		parts = append(parts, "reconnects "+strconv.FormatInt(n, 10))
	}
	if n := ns.CircuitBreakerTrips(); n > 0 {
		parts = append(parts, "breaker "+strconv.FormatInt(n, 10))
	}
	if r, p := ns.RetriableErrors(), ns.PermanentErrors(); r+p > 0 {
		parts = append(parts, "errors "+strconv.FormatInt(r, 10)+"/"+strconv.FormatInt(p, 10))
	}
	if len(parts) == 0 {
		return ""
	}

	return "net: " + strings.Join(parts, sep)
}

// humanBytes renders a byte count with a binary-unit suffix ("512B",
// "2.1MB"): one decimal below 10 units, none above.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	suffix := "KMGTPE"[exp]
	if v := float64(n) / float64(div); v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64) + string(suffix) + "B"
	}

	return strconv.FormatFloat(float64(n)/float64(div), 'f', 0, 64) + string(suffix) + "B"
}

// stressSummaryState builds the §H overlay state from the App's
// StressSummary, carrying every section the legacy CLI table printed:
// run plan, percentiles plus the latency budget, RC counts with shares,
// the fixed-bucket histogram, and the per-transaction breakdown. target
// is the live connection endpoint (root-derived).
func stressSummaryState(th *theme.Theme, s *app.StressSummary, target string) *pages.StressSummaryState {
	runtime := s.Runtime
	sep := th.Separator()
	plan := strconv.Itoa(s.Workers) + " worker"
	if s.Workers != 1 {
		plan += "s"
	}
	plan += sep + strconv.Itoa(s.TargetTPS) + " tps" + sep + "ramp " + s.RampUpDuration.String()

	dur := s.Duration.String()
	if s.Duration <= 0 {
		dur = "until stopped"
	}
	plan += sep + "duration " + dur

	txNames := strings.Join(s.TransactionNames, ", ")
	if txNames == "" {
		txNames = summaryDash(th)
	}

	out := &pages.StressSummaryState{
		WorkerID: s.WorkerID,
		Status:   summaryStatusToken(s.Status),
		Success:  successRatio(s.Successful, s.Sent),
		Run: []pages.SummaryKV{
			{Label: "transactions", Value: txNames},
			{Label: "target", Value: dashOr(target, summaryDash(th))},
			{Label: "plan", Value: plan},
			{Label: "runtime", Value: pages.FormatUptime(&runtime) + sep +
				"actual " + formatTps(s.ActualTPS) + " tps" + sep +
				"peak " + formatTps(s.PeakTPS) + " tps"},
			{Label: "sent", Value: countCell(s.Sent) + sep +
				"ok " + countCell(s.Successful) + sep +
				"fail " + countCell(s.Failed)},
		},
		Percentiles: []pages.SummaryKV{
			{Label: "min", Value: msCell(s.MinLatencyMs) + " ms"},
			{Label: "mean", Value: msCell(s.MeanLatencyMs) + " ms"},
			{Label: "max", Value: msCell(s.MaxLatencyMs) + " ms"},
			{Label: "p50", Value: msCell(s.P50LatencyMs) + " ms"},
			{Label: "p90", Value: msCell(s.P90LatencyMs) + " ms"},
			{Label: "p95", Value: msCell(s.P95LatencyMs) + " ms"},
			{Label: "p99", Value: msCell(s.P99LatencyMs) + " ms"},
		},
		Budget: []pages.SummaryKV{
			{Label: "satisfactory", Value: countCell(s.LatencySatisfactory)},
			{Label: "tolerable", Value: countCell(s.LatencyTolerable)},
			{Label: "exceeded", Value: countCell(s.LatencyExceeded)},
		},
	}
	for _, rc := range sortedRCCodes(s.ResponseCodes) {
		n := s.ResponseCodes[rc]
		out.RCs = append(out.RCs, pages.SummaryKV{
			Label: rc, Value: countCell(n) + sep + sharePct(n, s.Sent),
		})
	}
	for _, b := range s.LatencyBuckets {
		out.Histogram = append(out.Histogram, pages.SummaryBarRow{Label: b.Label, Count: b.Count})
	}
	for _, tx := range s.Transactions {
		rcs := ""
		if codes := sortedRCCodes(tx.ResponseCodes); len(codes) > 0 {
			parts := make([]string, 0, len(codes))
			for _, rc := range codes {
				parts = append(parts, rc+"("+countCell(tx.ResponseCodes[rc])+")")
			}
			rcs = "rc " + strings.Join(parts, " ")
		}

		out.TxRows = append(out.TxRows, pages.SummaryTxRow{
			Name:   tx.Name,
			OKFail: "ok " + countCell(tx.Successful) + sep + "err " + countCell(tx.Failed),
			Mean:   "mean " + msCell(tx.MeanLatencyMs) + " ms",
			P99:    "p99 " + msCell(tx.P99LatencyMs) + " ms",
			RCs:    rcs,
		})
	}

	return out
}

// summaryStatusToken maps the app's worker status to the page's
// canonical token ("completed" -> done; unknown tokens pass through
// and render without a symbol).
func summaryStatusToken(status string) string {
	switch status {
	case "completed":
		return pages.StatusDone
	case "running":
		return pages.StatusRunning
	default:
		return status
	}
}

// successRatio is the ok/sent percentage ("99.3%"); "" when nothing was
// sent so the title renders no chip.
func successRatio(ok, sent int) string {
	if sent <= 0 {
		return ""
	}

	return sharePct(ok, sent)
}

// sharePct renders one decimal share ("99.3%") of n over total.
func sharePct(n, total int) string {
	if total <= 0 {
		return "0.0%"
	}

	return fmt.Sprintf("%.1f%%", float64(n)*100/float64(total))
}

// dashIfRoot substitutes the theme dash for empty display strings at
// root (mirrors pages.dashIf: em dash, "-" under theme.ASCII).
func (m *RootModel) dashIfRoot(s string) string {
	if s != "" {
		return s
	}
	if m.themeOrNil().ASCII {
		return "-"
	}

	return "\u2014"
}

// summaryDash is the em/ascii dash for the summary's missing values.
func summaryDash(th *theme.Theme) string {
	if th != nil && th.ASCII {
		return "-"
	}

	return "\u2014"
}

// dashOr returns v, or the dash when v is empty.
func dashOr(v, dash string) string {
	if v != "" {
		return v
	}

	return dash
}
