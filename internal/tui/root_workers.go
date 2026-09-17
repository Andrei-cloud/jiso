// root_workers.go owns the §H worker truth. The page is a
// presentation-only consumer of WorkersState snapshots while root folds
// the App worker manager's bus events into a row cache — the designed
// consumer of WorkerStarted/WorkerProgress/WorkerStopped (the same
// bridge.Msg path the forwarded pages.EventMsg uses, proven
// tick-free by TestWorkerProgressUpdatesRowWithoutTick). The cache is the table's
// truth: App Workers snapshots enrich live rows, events flip terminal
// ones, and a `k`/`K` never writes status optimistically — the row flips
// only when WorkerStopped arrives. The sparkline ring (last ~24 TPS
// samples), the per-worker progress rows (ETA → elapsed morph), and the
// finished-run summary (StressSummaryByID, never invented) are all
// derived here with the injectable clock; the runtime-refresh tick reuses
// the §G seq-token lifecycle. Start/stop go through the SAME App entry
// points the CLI shims drive (WorkerStart/StressStart/WorkerStop/
// WorkerStopAll/StressSummaryByID).
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// workerTickInterval is the §H runtime-refresh cadence (display only;
// row data itself is event-driven).
const workerTickInterval = time.Second

// workerRunParams remembers a stress run's form inputs so progress rows
// can derive expected counts and ETAs (the 7,940/19,200 +
// ETA 01:12); bgsend runs need none (their totals are unbounded).
type workerRunParams struct {
	names     []string
	targetTps int
	ramp      time.Duration
	duration  time.Duration
	workers   int
}

// workerRowState is one row of the root-side worker cache. start is the
// fake-clock origin (now minus the runtime seen at first sight), so the
// RUNTIME column and ETAs derive from the injectable clock.
type workerRowState struct {
	id        string
	stress    bool
	txn       string
	status    string
	start     time.Time
	terminal  bool
	done      int
	okCount   int
	failCount int
	consec    int
	total     int
	thr       int
	interval  time.Duration
	curTps    float64

	lastDone   int
	lastSample time.Time
	hadSample  bool
}

// workerRuntimeTickMsg is one runtime-refresh tick carrying its arming
// generation seq (the serverStatsTickMsg pattern: a stale seq marks the
// message ignored).
type workerRuntimeTickMsg struct{ seq uint64 }

// defaultWorkersTick is the production tick scheduler; tests inject
// m.workerTickf to observe arming without a clock.
func defaultWorkersTick(d time.Duration, mk func() tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return mk() })
}

// themeOrNil resolves the theme for root-derived glyphs ("" modes fall
// back to the default).
// workerKindStress is the kind tag a stress-run worker carries. The root sets it
// from the worker event and the row builder reads it back to decide which columns
// to fill, so the two ends have to agree; the app layer's own tag for a stress
// worker is spelled differently on purpose ("stress_test"), because that one is
// persisted and this one is not.
const workerKindStress = "stress"

func (m *RootModel) themeOrNil() *theme.Theme {
	if m.theme != nil {
		return m.theme
	}

	return theme.Default()
}

// workerSep is the derived-line separator, ASCII-fied for ascii themes.
func (m *RootModel) workerSep() string { return m.themeOrNil().Separator() }

// onWorkerStarted seeds the cache row (the row appears from the bus
// alone — no snapshot poll needed). Kind "stress" marks the stress
// shape; anything else is a background sender.
func (m *RootModel) onWorkerStarted(ev events.WorkerStarted, now time.Time) {
	if m.workerRows == nil {
		m.workerRows = map[string]*workerRowState{}
	}
	if r, ok := m.workerRows[ev.ID]; ok && !r.terminal {
		r.stress = r.stress || ev.Kind == workerKindStress
		r.status = pages.StatusRunning

		return
	}
	m.workerRows[ev.ID] = &workerRowState{
		id: ev.ID, stress: ev.Kind == workerKindStress,
		status: pages.StatusRunning, start: now,
	}
	m.workersStatus = ""
}

// onWorkerProgress folds one throttled progress event into its row: the
// completion counters the Note carries ("12.3 tps ok=100 err=2" stress,
// "ok=50 err=0" bgsend) verbatim — the table renders exactly what the
// event carried — and appends a TPS sample to the sparkline ring (the
// Note's leading float when present, else the done/time delta rate).
func (m *RootModel) onWorkerProgress(ev events.WorkerProgress, now time.Time) {
	r := m.workerRowFor(ev.ID)
	r.done = ev.Done
	if ev.Total > 0 {
		r.total = ev.Total
	}
	r.okCount, r.failCount, r.curTps = parseWorkerNote(ev.Note, r.okCount, r.failCount, r.curTps)

	sample := r.curTps
	if !r.stress || sample <= 0 {
		sample = workerRate(r, ev.Done, now)
	}
	if sample > 0 {
		m.workerRing = append(m.workerRing, sample)
		if over := len(m.workerRing) - pages.SparkWidth; over > 0 {
			m.workerRing = append([]float64(nil), m.workerRing[over:]...)
		}
		if r.stress {
			r.curTps = sample
		}
	}
	r.lastDone, r.lastSample, r.hadSample = ev.Done, now, true
}

// onWorkerStopped flips the row terminal — the only path that writes a
// terminal status (no optimistic stop). A finished stress run also
// fetches its summary through the App accessor; no summary, no overlay.
func (m *RootModel) onWorkerStopped(ev events.WorkerStopped, now time.Time) {
	r := m.workerRowFor(ev.ID)
	switch {
	case ev.Reason == "done":
		r.status = pages.StatusDone
	case strings.HasPrefix(ev.Reason, "failed"):
		r.status = pages.StatusCircuitBroke
	default:
		r.status = pages.StatusStopped
	}
	r.terminal = true
	r.lastSample = now

	if r.stress {
		m.applyStressTerminal(ev, now)
	}
}

// applyStressTerminal fetches the stress summary for a terminal worker event and
// fills the workers summary and the "last stress" card from that single fetch.
func (m *RootModel) applyStressTerminal(ev events.WorkerStopped, now time.Time) {
	s, err := m.stressSummary(ev.ID)
	if err != nil || s == nil {
		return
	}

	if s.WorkerID == "" {
		s.WorkerID = ev.ID
	}
	target := ""
	if cfg := m.configOrNil(); cfg != nil {
		target = cfg.GetHost() + ":" + cfg.GetPort()
	}
	m.workersSummary = stressSummaryState(m.themeOrNil(), s, target)
	// The LAST STRESS card stamps from this SAME
	// single summary fetch (never per tick); it gates the §A
	// "Stress summary" reopen row.
	m.lastStress = &pages.LastStressCard{
		Time:    now.Format("15:04:05"),
		ID:      ev.ID,
		Done:    ev.Reason == "done",
		OkPct:   successRatio(s.Successful, s.Sent),
		Workers: workerCountCell(s.Workers),
		TPS:     formatTps(s.ActualTPS) + " tps",
		P99:     msCell(s.P99LatencyMs) + "ms",
	}
}

// viewLastStressSummary reopens §H with the last completed stress run's
// summary overlay (the §A LAST STRESS card's "Stress summary" row). It
// reuses the state the WorkerStopped fold already stamped; only when the
// page carries no (or a different run's) summary does it re-read the
// injectable accessor — the same leg the bus fold uses. The overlay
// itself is the existing §H path (SetState identity / OpenSummary), so
// Esc closes it exactly like the design.
func (m *RootModel) viewLastStressSummary() (tea.Model, tea.Cmd) {
	if m.lastStress == nil || m.lastStress.ID == "" {
		m.pushToast("no stress run yet", widgets.ToastInfo)

		return m, nil
	}
	id := m.lastStress.ID
	if m.workersSummary == nil || m.workersSummary.WorkerID != id {
		s, err := m.stressSummary(id)
		if err != nil || s == nil {
			m.pushToast("no stress summary for "+id, widgets.ToastError)

			return m, nil
		}
		if s.WorkerID == "" {
			s.WorkerID = id
		}
		target := ""
		if cfg := m.configOrNil(); cfg != nil {
			target = cfg.GetHost() + ":" + cfg.GetPort()
		}
		m.workersSummary = stressSummaryState(m.themeOrNil(), s, target)
	}
	if m.Current().ID() != pages.WorkersPageID {
		m.Push(m.workers)
	}
	m.workers.OpenSummary()
	m.debug.logf("stress summary view worker=%s", id)

	return m, nil
}

// workerRowFor returns the cache row, seeding a provisional one for
// events that raced ahead of WorkerStarted (or arrived after a
// restart): the table still shows the truth the bus carried.
func (m *RootModel) workerRowFor(id string) *workerRowState {
	if m.workerRows == nil {
		m.workerRows = map[string]*workerRowState{}
	}
	r, ok := m.workerRows[id]
	if !ok {
		r = &workerRowState{id: id, status: pages.StatusRunning, start: m.now()}
		m.workerRows[id] = r
	}

	return r
}

// workerRate derives a TPS sample from completion deltas since the last
// progress event (fake-clock deterministic).
func workerRate(r *workerRowState, done int, now time.Time) float64 {
	if !r.hadSample {
		r.lastDone, r.lastSample = done, now

		return 0
	}
	dt := now.Sub(r.lastSample).Seconds()
	if dt <= 0 || done <= r.lastDone {
		return 0
	}
	rate := float64(done-r.lastDone) / dt
	r.lastDone, r.lastSample = done, now

	return rate
}

// parseWorkerNote reads "ok=N err=M" (and the optional "%.1f tps"
// prefix) out of a WorkerProgress Note, keeping the previous values
// when a part is absent.
func parseWorkerNote(note string, prevOK, prevFail int, prevTPS float64) (ok, fail int, tps float64) {
	ok, fail, tps = prevOK, prevFail, prevTPS
	if i := strings.Index(note, " tps"); i > 0 {
		if v, err := strconv.ParseFloat(note[:i], 64); err == nil {
			tps = v
		}
	}
	if i := strings.Index(note, "ok="); i >= 0 {
		ok, fail = parseWorkerOKFail(note[i+3:], ok, fail)
	}

	return ok, fail, tps
}

// parseWorkerOKFail reads the "ok=N err=M" counts from a worker note tail,
// returning the passed-in ok/fail unchanged for any part that is absent.
func parseWorkerOKFail(rest string, ok, fail int) (outOK, outFail int) {
	outOK, outFail = ok, fail
	if j := strings.Index(rest, " "); j >= 0 {
		if v, err := strconv.Atoi(rest[:j]); err == nil {
			outOK = v
		}
		rest = rest[j+1:]
	}

	if strings.HasPrefix(rest, "err=") {
		if v, err := strconv.Atoi(strings.TrimSpace(rest[4:])); err == nil {
			outFail = v
		}
	}

	return outOK, outFail
}

// stressSummary reads a finished run's summary through the injectable
// leg (nil = app.StressSummaryByID — the accessor the CLI stress watcher
// prints from).
func (m *RootModel) stressSummary(id string) (*app.StressSummary, error) {
	fn := m.workerSummaryFn
	if fn == nil {
		if m.app == nil {
			return nil, fmt.Errorf("%s", errNoAppWired)
		}
		fn = m.app.StressSummaryByID
	}

	return fn(id)
}

// activeWorkerCount counts rows still doing work (running or ramping);
// it gates the runtime tick and the §N3 stop-all / quit confirms.
func (m *RootModel) activeWorkerCount() int {
	n := 0
	for _, r := range m.workerRows {
		if !r.terminal && (r.status == pages.StatusRunning || r.status == pages.StatusRamping) {
			n++
		}
	}

	return n
}

// armWorkersTick enforces the §H tick lifecycle (the §G contract): arm
// only while the page is current AND a worker is active; leaving or
// quiescing bumps the seq so in-flight ticks turn stale.
func (m *RootModel) armWorkersTick() tea.Cmd {
	active := m.Current() != nil && m.Current().ID() == pages.WorkersPageID
	if !active || m.activeWorkerCount() == 0 {
		if m.workerTickWait {
			m.workerTickSeq++
			m.workerTickWait = false
			m.debug.logf("workers tick disarm seq=%d", m.workerTickSeq)
		}

		return nil
	}
	if m.workerTickWait {
		return nil
	}

	seq := m.workerTickSeq
	m.workerTickWait = true

	tickf := m.workerTickf
	if tickf == nil {
		tickf = defaultWorkersTick
	}

	return tickf(workerTickInterval, func() tea.Msg { return workerRuntimeTickMsg{seq: seq} })
}

// applyWorkersRuntimeTick drops stale seqs; a fresh tick just re-syncs
// (syncWorkers re-derives RUNTIME/ETA from the injectable clock).
func (m *RootModel) applyWorkersRuntimeTick(msg workerRuntimeTickMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.workerTickSeq {
		return m, nil
	}
	m.workerTickWait = false
	m.debug.logf("workers runtime tick seq=%d", msg.seq)

	return m, nil
}
