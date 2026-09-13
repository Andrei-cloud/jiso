package app

import "time"

// Worker progress throttle policy, applied per worker:
//
//   - At most one WorkerProgress per progressInterval (250 ms) is
//     published while completions keep arriving.
//   - A completion is forced out early once progressEveryCompletions (100)
//     completions accumulated since the previous publish, bounding staleness
//     during bursts faster than the interval.
//   - A final WorkerProgress snapshot is published at finish whenever the
//     worker recorded at least one completion, immediately before the
//     WorkerStopped event, so observers always see the closing totals.
//
// Progress is completion-driven: a worker with no completions publishes no
// progress events.
const (
	progressInterval         = 250 * time.Millisecond
	progressEveryCompletions = 100
)

// progressThrottle implements the per-worker WorkerProgress throttle
// documented on progressInterval. The zero value publishes on the first
// completion.
type progressThrottle struct {
	last  time.Time
	since int
}

// note records one completion tick with the worker's running total and
// reports whether a WorkerProgress event should be published now.
func (p *progressThrottle) note(now time.Time) bool {
	if !p.last.IsZero() && now.Sub(p.last) < progressInterval && p.since < progressEveryCompletions {
		p.since++

		return false
	}

	p.last = now
	p.since = 0

	return true
}
