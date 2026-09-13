// sparkline.go is the §H TPS strip: a pure normalizer plus the glyph
// renderers. NormalizeSparkline maps a sample window to block heights
// 0..7 relative to the window maximum (TPS bottoms at 0, so max-relative
// beats min-max: a constant series is a flat full line, not a division
// by zero). RenderSparkline draws the heights with the eight block
// glyphs; under theme.ASCII the 0..7 ramp degrades onto the five-cell
// ".:-=#" ladder (level*4/7), the same richest-first degradation the
// progress bar practices. Root owns the ring (last ~24 samples); this
// file owns the math — table-driven tested, no clock, no I/O.
package pages

import (
	"strings"

	"jiso/internal/tui/theme"
)

// SparkBlocks are the eight block heights (index = level 0..7).
const SparkBlocks = "▁▂▃▄▅▆▇█"

// SparkASCII is the five-glyph ascii fallback ladder.
const SparkASCII = ".:-=#"

// SparkTop is the highest block level.
const SparkTop = 7

// SparkWidth is the sparkline window the root ring keeps (last ~24
// progress samples, oldest first).
const SparkWidth = 24

// NormalizeSparkline maps samples (oldest first) to block heights 0..7:
// level = round(sample/max*7). Empty input yields nil; a max of zero
// (all-zero series) yields all-0; a constant series maps to a flat line
// of its own level. Samples beyond SparkWidth keep the newest window
// (the ring caps at root, but the function stays total).
func NormalizeSparkline(samples []float64) []int {
	if len(samples) == 0 {
		return nil
	}
	if len(samples) > SparkWidth {
		samples = samples[len(samples)-SparkWidth:]
	}

	peak := samples[0]
	for _, s := range samples[1:] {
		if s > peak {
			peak = s
		}
	}

	levels := make([]int, len(samples))
	if peak <= 0 {
		return levels
	}
	for i, s := range samples {
		if s < 0 {
			s = 0
		}
		levels[i] = int((s*float64(SparkTop) + peak/2) / peak)
	}

	return levels
}

// RenderSparkline draws block heights as one line ("" when empty);
// unknown levels clamp to the ramp. ASCII themes get the five-cell
// ladder so ascii goldens stay 7-bit.
func RenderSparkline(th *theme.Theme, levels []int) string {
	if len(levels) == 0 {
		return ""
	}
	if th != nil && th.ASCII {
		return sparklineASCII(levels)
	}

	var b strings.Builder
	for _, l := range levels {
		b.WriteRune(sparkRunes[clampLevel(l)])
	}

	return b.String()
}

var sparkRunes = []rune(SparkBlocks)

// clampLevel keeps a level inside the [0,7] ramp.
func clampLevel(l int) int {
	if l < 0 {
		return 0
	}
	if l > SparkTop {
		return SparkTop
	}

	return l
}

// sparklineASCII renders levels onto the five-cell ascii ladder
// (level*len(ladder)/(SparkTop+1) keeps 0→"." and 7→"#").
func sparklineASCII(levels []int) string {
	if len(levels) == 0 {
		return ""
	}
	out := make([]byte, 0, len(levels))
	for _, l := range levels {
		out = append(out, SparkASCII[clampLevel(l)*len(SparkASCII)/(SparkTop+1)])
	}

	return string(out)
}
