// root_workers_fmt.go holds the §H display-string helpers:
// thousands-separated counts, TPS/latency cells, and the MM:SS clock the
// ETA → elapsed morph renders (the "ETA 01:12" / "elapsed
// 00:48" form — minutes may exceed 60, hours never shown).
package tui

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// countCell renders n with thousands separators ("1,802"); the same
// six-line helper the §G view uses, root-side for root-derived cells.
func countCell(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}

		return s
	}
	rem := len(s) % 3
	out := s[:rem]
	for i := rem; i < len(s); i += 3 {
		if out != "" {
			out += ","
		}
		out += s[i : i+3]
	}
	if neg {
		out = "-" + out
	}

	return out
}

// formatTps renders a TPS value with one decimal ("118.4").
func formatTps(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// workerCountCell renders the §A LAST STRESS card's concurrency cell
// ("1 worker" / "4 workers").
func workerCountCell(n int) string {
	if n == 1 {
		return "1 worker"
	}

	return strconv.Itoa(n) + " workers"
}

// msCell renders a latency millisecond value with one decimal ("3.4").
func msCell(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// hhmmss renders d as MM:SS (minutes may exceed 59; negatives clamp).
func hhmmss(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60

	return strconv.Itoa(m/10) + strconv.Itoa(m%10) + ":" +
		strconv.Itoa(s/10) + strconv.Itoa(s%10)
}

// sortedRCCodes lists response-code keys ascending ("00" before "96"),
// the stable order of the CLI summary's RC breakdown.
func sortedRCCodes(codes map[string]int) []string {
	if len(codes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(codes))
	for k := range codes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
