// Package progress provides the TUI's duration-honest progress primitives
// (design contract §Progress: spinner glyph for unknown duration,
// determinate bar for known, never two competing bars):
//
//   - Bar: determinate width-aware bar (█ fill, # in ascii) with percent
//     and elapsed label; degrades to percent-only below the bar floor and
//     to spinner+count ("pulse") the moment Total becomes unknown — a
//     frozen full bar is never shown.
//
// The pulse animates from SpinIndex below so any renderer derives the
// frame from a clock value instead of hidden counters. Everything renders
// through theme tokens, so a colorless profile produces plain text and both
// glyph sets are golden-testable.
package progress

import "time"

// Frame sets and cadence defaults.
const (
	// DefaultSpinnerInterval is the frame cadence.
	DefaultSpinnerInterval = 120 * time.Millisecond

	// DefaultSpinFrames are the braille dots (10 frames).
	DefaultSpinFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	// ASCIISpinFrames is the NO_COLOR/JISO_ASCII fallback set.
	ASCIISpinFrames = "|/-\\"
)

// SpinIndex returns the frame index for an instant against interval, so any
// renderer (the bar's pulse degrade) animates deterministically from a clock
// value instead of hidden counters.
func SpinIndex(now time.Time, interval time.Duration, frames int) int {
	if frames <= 0 || interval <= 0 {
		return 0
	}
	elapsed := now.UnixNano()
	if elapsed < 0 {
		elapsed = 0
	}

	return int(uint64(elapsed/interval.Nanoseconds()) % uint64(frames))
}
