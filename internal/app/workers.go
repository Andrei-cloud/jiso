package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"jiso/internal/app/events"
)

// WorkerSender executes one background transaction per iteration. It is
// the seam between the App worker manager and the send pipeline: the CLI
// injects its SendCommand through SetWorkerSenderResolver so the manager
// stays free of cobra/command-package dependencies. ExecuteBackground
// returns the response code string, the execution time, and the failure
// (nil on success).
type WorkerSender interface {
	// StartClock marks the beginning of a worker run for sender-side
	// statistics.
	StartClock()
	// ExecuteBackground runs one transaction send.
	ExecuteBackground(trxnName string, skipValidation bool, sessionID string) (string, time.Duration, error)
}

// SetWorkerSenderResolver injects the factory the worker manager calls at
// every start to obtain the live sender (the CLI resolves its current
// "send" command, so reloads that rebuild commands are picked up
// automatically). Passing nil clears the resolver; starts then fail with
// "send command not found or has wrong type", matching the legacy text.
func (a *App) SetWorkerSenderResolver(resolve func() WorkerSender) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	a.resolveSender = resolve
}

const (
	// CircuitBreakerFailures is the consecutive-failure count that stops a
	// worker. It is exported because the §H CIRCUIT column shows this denominator:
	// the TUI used to mirror it as its own const, which reads as a fact about the
	// UI while being a second copy of a rule that can move.
	CircuitBreakerFailures = 10

	workerStopTimeout       = 5 * time.Second
	stopAllWorkersTimeout   = 10 * time.Second
	maxRememberedStressRuns = 64
)

// WorkerView is the typed row of the App worker list (Workers). It
// replaces the untyped map[string]any rows the legacy CLI manager exposed.
// Stress-specific fields are zero for background workers and vice versa.
type WorkerView struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Type                string        `json:"type"` // "background" | "stress_test"
	Status              string        `json:"status"`
	Workers             int           `json:"workers"`
	Interval            time.Duration `json:"interval,omitempty"`
	Runtime             time.Duration `json:"runtime"`
	Successful          int           `json:"successful"`
	Failed              int           `json:"failed"`
	ConsecutiveFailures int           `json:"consecutive_failures"`

	TargetTPS      float64       `json:"target_tps,omitempty"`
	CurrentTPS     float64       `json:"current_tps,omitempty"`
	ActualTPS      float64       `json:"actual_tps,omitempty"`
	InstantTPS     float64       `json:"instant_tps,omitempty"`
	RampUpProgress float64       `json:"ramp_up_progress,omitempty"`
	RampUpDuration time.Duration `json:"ramp_up_duration,omitempty"`
	Duration       time.Duration `json:"duration,omitempty"`
}

// backgroundWorker holds the state of one periodic background sender
// ("bgsend") worker.
type backgroundWorker struct {
	id        string
	name      string
	count     int
	interval  time.Duration
	startTime time.Time
	endTime   time.Time
	ctx       context.Context
	cancel    context.CancelFunc

	mu                  sync.Mutex
	successful          int
	failed              int
	consecutiveFailures int
	completed           bool
	stopReason          string
	throttle            progressThrottle
	wg                  sync.WaitGroup
}

// WorkerStart launches a background worker that sends transaction name
// every interval, count times per tick, until WorkerStop. It publishes
// WorkerStarted{ID, "bgsend"} plus throttled progress and a stop event on
// the App event bus.
func (a *App) WorkerStart(name string, count int, interval time.Duration) (string, error) {
	if interval <= 0 {
		return "", fmt.Errorf("interval must be greater than 0, got %s", interval)
	}
	if count < 1 {
		count = 1
	}

	sender, err := a.workerSender()
	if err != nil {
		return "", err
	}

	w := &backgroundWorker{
		id:        shortWorkerID(),
		name:      name,
		count:     count,
		interval:  interval,
		startTime: time.Now(),
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())

	a.wmu.Lock()
	if a.isClosed() {
		a.wmu.Unlock()
		w.cancel()

		return "", ErrClosed
	}
	a.workers[w.id] = w
	a.wmu.Unlock()

	a.events.Publish(events.WorkerStarted{ID: w.id, Kind: "bgsend"})

	w.wg.Add(1)
	go a.runBackgroundWorker(w, sender)

	return w.id, nil
}

// runBackgroundWorker is the periodic send loop. Terminal output belongs
// to the frontends; this loop only records state and publishes events.
func (a *App) runBackgroundWorker(w *backgroundWorker, sender WorkerSender) {
	defer w.wg.Done()

	sender.StartClock()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			a.finishBackgroundWorker(w, StatusStopped)

			return
		case <-ticker.C:
			for i := 0; i < w.count; i++ {
				if w.ctx.Err() != nil {
					a.finishBackgroundWorker(w, StatusStopped)

					return
				}

				_, _, err := sender.ExecuteBackground(w.name, false, "")
				total, tripped, shouldPublish := a.recordSendResult(w, err, time.Now())

				if shouldPublish {
					a.events.Publish(events.WorkerProgress{
						ID:    w.id,
						Done:  total,
						Total: 0,
						Note:  fmt.Sprintf("ok=%d err=%d", total-w.failed, w.failed),
					})
				}

				if tripped {
					w.cancel()
					a.finishBackgroundWorker(w, "failed")

					return
				}
			}
		}
	}
}

// recordSendResult folds one send's outcome into the worker under its lock:
// it bumps the success/failure counters, records a circuit-breaker trip once
// the consecutive-failure threshold is crossed, and reports the running total,
// whether the circuit tripped, and whether the throttled progress event is due
// now. The counters and the throttle share the lock, exactly as when they sat
// inline in the send loop.
func (a *App) recordSendResult(w *backgroundWorker, err error, now time.Time) (total int, tripped, shouldPublish bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err == nil {
		w.successful++
		w.consecutiveFailures = 0
	} else {
		w.failed++
		w.consecutiveFailures++
	}
	if w.consecutiveFailures >= CircuitBreakerFailures {
		tripped = true
		w.stopReason = fmt.Sprintf("failed: %d consecutive failures", w.consecutiveFailures)
		if stats := a.NetworkingStats(); stats != nil {
			stats.RecordCircuitBreakerTrip()
		}
	}

	return w.successful + w.failed, tripped, w.throttle.note(now)
}

// finishBackgroundWorker closes out the worker once: records completion,
// publishes the final progress snapshot (when any completion was
// recorded) and the WorkerStopped event.
func (a *App) finishBackgroundWorker(w *backgroundWorker, reason string) {
	w.mu.Lock()
	if w.completed {
		w.mu.Unlock()

		return
	}
	w.completed = true
	w.endTime = time.Now()
	if w.stopReason == "" {
		w.stopReason = reason
	}
	total := w.successful + w.failed
	successful, failed := w.successful, w.failed
	reason = w.stopReason
	w.mu.Unlock()

	if total > 0 {
		a.events.Publish(events.WorkerProgress{
			ID:    w.id,
			Done:  total,
			Total: 0,
			Note:  fmt.Sprintf("ok=%d err=%d", successful, failed),
		})
	}

	a.events.Publish(events.WorkerStopped{ID: w.id, Reason: reason})
}

// WorkerStop gracefully stops the worker (background or stress) with the
// given ID: it removes the worker from the live list, cancels its context
// and waits up to workerStopTimeout for its goroutines to exit. The
// worker's own WorkerStopped event follows asynchronously.
func (a *App) WorkerStop(id string) error {
	a.wmu.Lock()

	if w, exists := a.workers[id]; exists {
		delete(a.workers, id)
		a.wmu.Unlock()

		w.mu.Lock()
		if w.stopReason == "" {
			w.stopReason = StatusStopped
		}
		w.mu.Unlock()

		w.cancel()
		waitWorkerGroup(&w.wg, workerStopTimeout)

		return nil
	}

	if w, exists := a.stressWorkers[id]; exists {
		delete(a.stressWorkers, id)
		a.wmu.Unlock()

		return a.stopStressWorker(w)
	}

	a.wmu.Unlock()

	return &WorkerNotFoundError{ID: id}
}

// WorkerStopAll gracefully stops every background and stress worker and
// waits (bounded) for their goroutines to exit.
func (a *App) WorkerStopAll() error {
	return a.stopAllWorkers(stopAllWorkersTimeout)
}

// stopAllWorkers collects the live workers under the lock, clears the
// maps, cancels everything and waits up to timeout. It returns an error
// only when the wait timed out.
func (a *App) stopAllWorkers(timeout time.Duration) error {
	a.wmu.Lock()
	bg := make([]*backgroundWorker, 0, len(a.workers))
	for _, w := range a.workers {
		bg = append(bg, w)
	}
	stress := make([]*stressWorker, 0, len(a.stressWorkers))
	for _, w := range a.stressWorkers {
		stress = append(stress, w)
	}
	a.workers = make(map[string]*backgroundWorker)
	a.stressWorkers = make(map[string]*stressWorker)
	a.wmu.Unlock()

	for _, w := range bg {
		w.mu.Lock()
		if w.stopReason == "" {
			w.stopReason = StatusStopped
		}
		w.mu.Unlock()
		w.cancel()
	}
	for _, w := range stress {
		w.mu.Lock()
		if w.stopReason == "" {
			w.stopReason = StatusStopped
		}
		w.mu.Unlock()
		w.cancel()
	}

	done := make(chan struct{})
	go func() {
		for _, w := range bg {
			w.wg.Wait()
		}
		for _, w := range stress {
			w.wg.Wait()
			w.requestsWg.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("some workers did not stop cleanly within timeout")
	}
}

// Workers returns a snapshot of every live worker (background and stress,
// running or completed-but-not-yet-stopped), longest-runtime first.
func (a *App) Workers() []WorkerView {
	a.wmu.Lock()
	bg := make([]*backgroundWorker, 0, len(a.workers))
	for _, w := range a.workers {
		bg = append(bg, w)
	}
	stress := make([]*stressWorker, 0, len(a.stressWorkers))
	for _, w := range a.stressWorkers {
		stress = append(stress, w)
	}
	a.wmu.Unlock()

	views := make([]WorkerView, 0, len(bg)+len(stress))
	for _, w := range bg {
		views = append(views, w.view())
	}
	for _, w := range stress {
		views = append(views, w.view())
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Runtime == views[j].Runtime {
			return views[i].ID < views[j].ID
		}

		return views[i].Runtime > views[j].Runtime
	})

	return views
}

// WorkerViewFor returns the live snapshot of one worker, and whether the id is
// known at all -- the caller has to tell a finished worker from one that never
// existed.
func (a *App) WorkerViewFor(id string) (WorkerView, bool) {
	a.wmu.Lock()
	defer a.wmu.Unlock()

	if w, exists := a.workers[id]; exists {
		return w.view(), true
	}
	if w, exists := a.stressWorkers[id]; exists {
		return w.view(), true
	}

	return WorkerView{}, false
}

// view snapshots the background worker state.
func (w *backgroundWorker) view() WorkerView {
	w.mu.Lock()
	defer w.mu.Unlock()

	status := StatusRunning
	if w.completed {
		status = StatusCompleted
	}

	runtime := w.endTimeLocked()

	return WorkerView{
		ID:                  w.id,
		Name:                w.name,
		Type:                "background",
		Status:              status,
		Workers:             w.count,
		Interval:            w.interval,
		Runtime:             runtime,
		Successful:          w.successful,
		Failed:              w.failed,
		ConsecutiveFailures: w.consecutiveFailures,
	}
}

// endTimeLocked returns the run length: time since start while running, frozen
// at the completion instant once finished (mirrors the stress worker's view so
// a completed worker's Runtime stops inflating across reads).
func (w *backgroundWorker) endTimeLocked() time.Duration {
	if w.completed && !w.endTime.IsZero() {
		return w.endTime.Sub(w.startTime)
	}

	return time.Since(w.startTime)
}

// workerSender resolves the injected sender, failing with the legacy text
// when none is wired.
func (a *App) workerSender() (WorkerSender, error) {
	a.wmu.Lock()
	resolve := a.resolveSender
	a.wmu.Unlock()

	if resolve == nil {
		return nil, errors.New("send command not found or has wrong type")
	}
	sender := resolve()
	if sender == nil {
		return nil, errors.New("send command not found or has wrong type")
	}

	return sender, nil
}

// shortWorkerID returns the 8-character worker ID the CLI displays.
func shortWorkerID() string {
	return uuid.New().String()[:8]
}

// waitWorkerGroup waits for wg up to timeout, dropping the wait
// afterwards (the caller already removed the worker from the live list).
func waitWorkerGroup(wg *sync.WaitGroup, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
	}
}
