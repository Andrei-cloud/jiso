// sparkline_test.go pins the §H TPS normalization contract: max-
// relative 0..7 block heights with a table-driven matrix (empty,
// constant, single, descending, spiky, one-max, all-zero, over-width)
// plus the ascii/truecolor renderers.
package pages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestNormalizeSparkline(t *testing.T) {
	t.Parallel()

	runes := func(levels []int) string {
		out := make([]rune, 0, len(levels))
		for _, l := range levels {
			out = append(out, []rune(SparkBlocks)[clampLevel(l)])
		}

		return string(out)
	}

	tests := []struct {
		name string
		in   []float64
		want string // block-glyph string of the levels
	}{
		{"empty", nil, ""},
		{"single max", []float64{5}, "█"},
		{"all zero", []float64{0, 0}, "▁▁"},
		{"constant", []float64{4, 4, 4}, "███"},
		{"half", []float64{6, 3}, "█▅"},
		{"one max at end", []float64{1, 2, 8}, "▂▃█"},
		{"one max at start", []float64{8, 1, 2}, "█▂▃"},
		{"descending", []float64{8, 6, 4, 2, 0}, "█▆▅▃▁"},
		{"spiky", []float64{0, 10, 0, 10}, "▁█▁█"},
		{"negative clamps to floor", []float64{-5, 10}, "▁█"},
		{
			"over width keeps newest", append(repeatF(23, 1.0), 0),
			strings.Repeat("█", 23) + "▁",
		}, // 23 old + the newest sample
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeSparkline(tc.in)
			if tc.in == nil {
				if got != nil {
					t.Fatalf("nil input → %v, want nil", got)
				}

				return
			}
			if len(got) != min(len(tc.in), SparkWidth) {
				t.Fatalf("len(levels) = %d, want %d", len(got), min(len(tc.in), SparkWidth))
			}
			for _, l := range got {
				if l < 0 || l > SparkTop {
					t.Fatalf("level %d outside [0,%d]", l, SparkTop)
				}
			}
			if s := runes(got); s != tc.want {
				t.Errorf("levels %v render %q, want %q", got, s, tc.want)
			}
		})
	}
}

func repeatF(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}

	return out
}

func TestRenderSparklineModes(t *testing.T) {
	t.Parallel()

	levels := NormalizeSparkline([]float64{0, 4, 2, 4})
	uni := testTheme(t, colorprofile.TrueColor)
	asc := testTheme(t, colorprofile.ASCII)

	if got := RenderSparkline(uni, levels); got != "▁█▅█" {
		t.Errorf("truecolor render = %q, want ▁█▅█", got)
	}
	// The ascii ladder is the five-glyph ".:-=#" ramp: 0→".", 7→"#".
	ascii := RenderSparkline(asc, levels)
	if ascii != ".#-#" {
		t.Errorf("ascii render = %q, want .#-#", ascii)
	}
	for _, r := range ascii {
		if !strings.ContainsRune(SparkASCII, r) {
			t.Errorf("ascii render %q leaves the %q ladder", ascii, SparkASCII)
		}
	}
	if got := RenderSparkline(uni, nil); got != "" {
		t.Errorf("empty levels render %q, want \"\"", got)
	}
}

func TestRenderSparklineClampsUnknownLevels(t *testing.T) {
	t.Parallel()

	uni := testTheme(t, colorprofile.TrueColor)
	if got := RenderSparkline(uni, []int{-3, 99}); got != "▁█" {
		t.Errorf("clamped render = %q, want ▁█", got)
	}
}
