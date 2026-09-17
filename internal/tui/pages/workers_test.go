// workers_test.go covers the §H page contract: empty state
// names the next action, the columns render, every status is
// symbol+word (circuit-broke included, never color alone), `k`/`K`
// dispatch the typed messages with the selected id, the summary overlay
// owns Esc first and hands it back, and narrow widths truncate without
// wrapping (120/90/70 + below-floor).
package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
)

// workersFixtureState is the §H sample table (fixed display
// strings — the page renders them verbatim).
func workersFixtureState() WorkersState {
	return WorkersState{
		Workers: []WorkerRow{
			{
				ID: "w-1", Type: "background", Txn: "Sign On", Status: StatusRunning,
				Thr: "1", IntervalTPS: "5s", Runtime: "00:04:12", OKFail: "50 / 0", Circuit: "",
			},
			{
				ID: "w-2", Type: "stress", Txn: "Purchase+2", Status: StatusRamping,
				Thr: "4", IntervalTPS: "60->120 tps", Runtime: "00:00:41", OKFail: "1,802 / 12", Circuit: "",
			},
			{
				ID: "w-3", Type: "stress", Txn: "Purchase", Status: StatusCircuitBroke,
				Thr: "2", IntervalTPS: "10->50 tps", Runtime: "00:01:03", OKFail: "402 / 31", Circuit: "TRIPPED (10/10)",
			},
			{
				ID: "w-4", Type: "background", Txn: "Echo", Status: StatusStopped,
				Thr: "1", IntervalTPS: "1s", Runtime: "00:02:00", OKFail: "120 / 1", Circuit: "1/10",
			},
			{
				ID: "w-5", Type: "background", Txn: "Echo", Status: StatusDone,
				Thr: "1", IntervalTPS: "1s", Runtime: "00:00:10", OKFail: "10 / 0", Circuit: "",
			},
		},
		Sparkline:  NormalizeSparkline([]float64{0, 40, 80, 118}),
		SparkLabel: "w-2 inst 118.4 avg 96.2",
		Net:        "net: reconnects 2 breaker 1",
		Progress: []ProgressRow{
			{ID: "w-2", Pct: 41, Counts: "7,940/19,200 sent", ETA: "ETA 01:12"},
			{ID: "w-1", Pct: -1, Counts: "50 sent"},
		},
	}
}

func workersPageAt(t *testing.T, st WorkersState, w, h int) *Workers {
	t.Helper()
	p := NewWorkers(asciiTheme(t))
	p.SetState(st)
	_, _ = p.Update(windowSize(w, h))

	return p
}

func TestWorkersEmptyState(t *testing.T) {
	t.Parallel()

	lines := workersBody(t, workersPageAt(t, WorkersState{}, 120, 32))
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"WORKERS & STRESS", "no workers", "b background-send", "t stress test"} {
		if !strings.Contains(joined, want) {
			t.Errorf("empty body lacks %q:\n%s", want, joined)
		}
	}
}

// workersBody renders the page body split into exactly the content-area
// lines (clipBlockStyled pads; no TrimRight so the count is exact).
func workersBody(t *testing.T, p *Workers) []string {
	t.Helper()

	return strings.Split(p.View().Content, "\n")
}

// workersBodyWidth checks every rendered line fits the terminal width.
func workersBodyFits(t *testing.T, p *Workers, width int) []string {
	t.Helper()
	lines := workersBody(t, p)
	for i, line := range lines {
		if n := lipgloss.Width(line); n > width {
			t.Fatalf("width %d line %d is %d cells (wrap/overflow): %q", width, i, n, line)
		}
	}

	return lines
}

func TestWorkersTableColumnsAndValues(t *testing.T) {
	t.Parallel()

	joined := strings.Join(workersBody(t, workersPageAt(t, workersFixtureState(), 132, 40)), "\n")
	for _, col := range []string{"ID", "TYPE", "TRANSACTION", "STATUS", "THR", "INTERVAL/TPS", "RUNTIME", "OK/FAIL", "CIRCUIT"} {
		if !strings.Contains(joined, col) {
			t.Errorf("header lacks column %q", col)
		}
	}
	for _, cell := range []string{"w-1", "background", "Sign On", "5s", "00:04:12", "50 / 0", "TRIPPED (10/10)", "1,802 / 12", "60->120 tps"} {
		if !strings.Contains(joined, cell) {
			t.Errorf("body lacks cell %q", cell)
		}
	}
}

func TestWorkersStatusSymbolPlusWord(t *testing.T) {
	t.Parallel()

	joined := strings.Join(workersBody(t, workersPageAt(t, workersFixtureState(), 120, 40)), "\n")
	// ascii theme: the five statuses must pair a glyph with the word.
	for _, want := range []string{"[ok] running", "[!] ramping", "[ok] done", ". stopped", "[x] circuit-broke"} {
		if !strings.Contains(joined, want) {
			t.Errorf("status cell %q missing (symbol+word contract)", want)
		}
	}
}

func TestWorkersProgressRows(t *testing.T) {
	t.Parallel()

	joined := strings.Join(workersBody(t, workersPageAt(t, workersFixtureState(), 120, 40)), "\n")
	if !strings.Contains(joined, "PROGRESS") {
		t.Fatal("progress section title missing")
	}
	if !strings.Contains(joined, "w-2 [") || !strings.Contains(joined, "41%") {
		t.Errorf("bounded bar row missing:\n%s", joined)
	}
	if !strings.Contains(joined, "7,940/19,200 sent") || !strings.Contains(joined, "ETA 01:12") {
		t.Errorf("counts/ETA note missing:\n%s", joined)
	}
	// Unknown totals render the degraded count line (never a spinner).
	if !strings.Contains(joined, "w-1 50 sent") {
		t.Errorf("unknown-total count row missing:\n%s", joined)
	}
}

func TestWorkersTpsStripWideAndNarrow(t *testing.T) {
	t.Parallel()

	wide := strings.Join(workersBody(t, workersPageAt(t, workersFixtureState(), 120, 40)), "\n")
	if !strings.Contains(wide, "TPS w-2 inst 118.4 avg 96.2") {
		t.Errorf("wide TPS strip missing:\n%s", wide)
	}
	if !strings.Contains(wide, "net:") {
		t.Errorf("net strip missing on wide layout:\n%s", wide)
	}
	narrow := strings.Join(workersBody(t, workersPageAt(t, workersFixtureState(), 90, 40)), "\n")
	if !strings.Contains(narrow, "TPS w-2") {
		t.Errorf("narrow layout must fold TPS into the status line:\n%s", narrow)
	}
}

func TestWorkersStopKeysDispatch(t *testing.T) {
	t.Parallel()

	p := workersPageAt(t, workersFixtureState(), 120, 40)
	if got := p.SelectedID(); got != "w-1" {
		t.Fatalf("cursor id = %q, want w-1", got)
	}
	_, cmd := p.Update(press('k'))
	msg := cmd()
	stop, ok := msg.(WorkersStopMsg)
	if !ok || stop.ID != "w-1" {
		t.Fatalf("k msg = %#v, want WorkersStopMsg{w-1}", msg)
	}
	_, cmd = p.Update(tea.KeyPressMsg{Code: 'K', Text: "K"})
	if _, ok := cmd().(WorkersStopAllMsg); !ok {
		t.Fatalf("K msg = %#v, want WorkersStopAllMsg", cmd())
	}
	// Down then k selects the second row's canonical id.
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = p.Update(press('k'))
	m, ok := cmd().(WorkersStopMsg)
	if !ok {
		t.Fatalf("k after down = %#v (%T), want WorkersStopMsg", cmd(), cmd())
	}
	if m.ID != "w-2" {
		t.Fatalf("k after down = %q, want w-2", m.ID)
	}
}

func TestWorkersStartFormsDispatch(t *testing.T) {
	t.Parallel()

	p := workersPageAt(t, workersFixtureState(), 120, 40)
	_, cmd := p.Update(press('b'))
	if m, ok := cmd().(WorkersOpenFormMsg); !ok || m.Kind != "bgsend" {
		t.Fatalf("b msg = %#v, want WorkersOpenFormMsg{bgsend}", cmd())
	}
	_, cmd = p.Update(press('t'))
	if m, ok := cmd().(WorkersOpenFormMsg); !ok || m.Kind != "stress" {
		t.Fatalf("t msg = %#v, want WorkersOpenFormMsg{stress}", cmd())
	}
}

func workersSummaryFixture() *StressSummaryState {
	return &StressSummaryState{
		WorkerID: "w-2",
		Status:   StatusDone,
		Success:  "99.3%",
		Run: []SummaryKV{
			{Label: "transactions", Value: "Echo, Purchase"},
			{Label: "target", Value: "127.0.0.1:9999"},
			{Label: "plan", Value: "1 worker | 100 tps | ramp 30s | duration 5m0s"},
			{Label: "runtime", Value: "00:01:51 | actual 82.3 tps | peak 101.2 tps"},
			{Label: "sent", Value: "1,814 | ok 1,802 | fail 12"},
		},
		Percentiles: []SummaryKV{
			{Label: "min", Value: "0.4 ms"},
			{Label: "mean", Value: "1.1 ms"},
			{Label: "p50", Value: "3.2 ms"},
			{Label: "p99", Value: "41.0 ms"},
		},
		Budget: []SummaryKV{
			{Label: "satisfactory", Value: "1,700"},
			{Label: "tolerable", Value: "100"},
			{Label: "exceeded", Value: "14"},
		},
		RCs:       []SummaryKV{{Label: "00", Value: "1,802 | 99.3%"}, {Label: "96", Value: "12 | 0.7%"}},
		Histogram: []SummaryBarRow{{Label: "<=50ms", Count: 1700}, {Label: ">50ms", Count: 114}},
		TxRows: []SummaryTxRow{
			{Name: "Echo", OKFail: "ok 1,202 | err 4", Mean: "mean 0.4 ms", P99: "p99 21.0 ms", RCs: "rc 00(1,202) 96(4)"},
			{Name: "Purchase", OKFail: "ok 600 | err 8", Mean: "mean 2.4 ms", P99: "p99 41.0 ms", RCs: "rc 00(600) 96(8)"},
		},
	}
}

func TestWorkersSummaryOverlayOpenAndEsc(t *testing.T) {
	t.Parallel()

	st := workersFixtureState()
	st.Summary = workersSummaryFixture()
	p := workersPageAt(t, st, 120, 40)

	if !p.SummaryOpen() {
		t.Fatal("pushing a new Summary must open the overlay")
	}
	joined := strings.Join(workersBody(t, p), "\n")
	for _, want := range []string{
		"STRESS SUMMARY", "w-2", "RUN", "Echo, Purchase", "127.0.0.1:9999",
		"peak 101.2 tps", "LATENCY MS", "min", "0.4 ms", "p99", "41.0 ms",
		"budget", "satisfactory", "RESPONSE CODES", "1,802", "HISTOGRAM",
		"PER TRANSACTION", "Purchase", "rc 00(1,202)", "esc close",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overlay lacks %q:\n%s", want, joined)
		}
	}
	// The overlay owns the keyboard: k does not stop anything.
	if _, cmd := p.Update(press('k')); cmd != nil {
		t.Fatal("k while overlay open must be swallowed")
	}
	// Esc closes the overlay and does NOT pop the page.
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatalf("first Esc must only close the overlay, got %v", cmd())
	}
	if p.SummaryOpen() {
		t.Fatal("overlay still open after Esc")
	}
	// Esc now hands back the pop message.
	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(WorkersPopMsg); !ok {
		t.Fatalf("second Esc = %#v, want WorkersPopMsg", cmd())
	}
}

func TestWorkersNoSummaryNoOverlay(t *testing.T) {
	t.Parallel()

	p := workersPageAt(t, workersFixtureState(), 120, 40)
	if p.SummaryOpen() {
		t.Fatal("overlay opened without a Summary")
	}
	joined := strings.Join(workersBody(t, p), "\n")
	if strings.Contains(joined, "PERCENTILES") {
		t.Fatal("summary sections rendered without a summary")
	}
}

func TestWorkersSummaryReopensOnNewIdentity(t *testing.T) {
	t.Parallel()

	st := workersFixtureState()
	st.Summary = workersSummaryFixture()
	p := workersPageAt(t, st, 120, 40)
	_, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.SummaryOpen() {
		t.Fatal("overlay closed by Esc must stay closed across re-pushes")
	}
	p.SetState(st) // same summary id -> no re-arm
	if p.SummaryOpen() {
		t.Fatal("same summary id must not re-open the overlay")
	}
	st2 := st
	st2.Summary = workersSummaryFixture()
	st2.Summary.WorkerID = "w-9"
	p.SetState(st2) // new id -> overlay re-arms
	if !p.SummaryOpen() {
		t.Fatal("a new summary must re-open the overlay")
	}
}

func TestWorkersWidthsTruncateNeverWrap(t *testing.T) {
	t.Parallel()

	st := workersFixtureState()
	st.Summary = nil
	for _, width := range []int{120, 90, 70, 48} {
		p := workersPageAt(t, st, width, 32)
		lines := workersBodyFits(t, p, width)
		_, h := frame.ContentSize(width, 32)
		if len(lines) != h {
			t.Fatalf("width %d: body lines = %d, want the %d-line content area", width, len(lines), h)
		}
		if !strings.Contains(strings.Join(lines, "\n"), "WORKERS & STRESS") {
			t.Fatalf("width %d lost the page title", width)
		}
	}
}

func TestWorkersHints(t *testing.T) {
	t.Parallel()

	p := workersPageAt(t, WorkersState{}, 120, 32)
	prim := map[string]bool{}
	for _, h := range p.Hints() {
		if h.Primary {
			prim[h.Key] = true
		}
	}
	for _, k := range []string{"b", "t", "k", "K"} {
		if !prim[k] {
			t.Fatalf("hint %s must be primary", k)
		}
	}
}
