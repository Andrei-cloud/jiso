// root_server_detail.go owns the §G root-side route→display derivation:
// the row summary cells and the full strings of the Enter-on-route detail.
// Field lists arrive in numeric reading order (sortFieldKeys), values are
// capped for their cell, and a pathological multi-line scalar collapses onto
// one display line.
package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// routeCellValueCells caps one rendered value in a §G route cell: longer
// strings take the theme's ellipsis and the full value belongs in the
// detail, which is where the whole pair lives.
const routeCellValueCells = 24

// routeMatchCell is the compact MATCH cell: the route name plus the MTI it
// matches, with the processing code when the route narrows to one
// ("Purchase Auth 0200/000000"), or the bare name plus "any" when the route
// declares no match fields (the catch-all — `serve routes` says ANY).
// Match pairs never render here: a row summarises, the detail reveals.
func routeMatchCell(th *theme.Theme, r config.MockRouteConfig) string {
	mti, hasMTI := routeScalar(th, r.MatchFields["0"])
	de3, hasDE3 := routeScalar(th, r.MatchFields["3"])

	switch {
	case len(r.MatchFields) == 0:
		return r.Name + " any"
	case hasMTI && hasDE3:
		return r.Name + " " + mti + "/" + de3
	case hasMTI:
		return r.Name + " " + mti
	default:
		// Matches on other fields only: the name is all a row can say about
		// it, and it never claims the catch-all "any".
		return r.Name
	}
}

// routeScalar reads one match criterion for the row summary. Only a scalar
// summarises a route — a nested payload is no summary, so the caller falls
// back to the route name and the pair stays in the detail.
func routeScalar(th *theme.Theme, v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return th.Truncate(val, routeCellValueCells), true
	case float64, int, bool:
		return fmt.Sprintf("%v", val), true
	}

	return "", false
}

// routeLatencyCell is the LATENCY cell: the effective base delay
// (delay_ms, else latency_ms — the engine's own fallback) with the
// jitter suffix the design shows ("100±25ms").
func routeLatencyCell(r config.MockRouteConfig) string {
	base := routeBaseDelayMs(r)
	if r.JitterMs > 0 {
		return strconv.Itoa(base) + "\xc2\xb1" + strconv.Itoa(r.JitterMs) + "ms"
	}

	return strconv.Itoa(base) + "ms"
}

// routeDetail builds the Enter-on-route detail view from the route
// config (internal/config MockRouteConfig — the tx file mock_routes
// schema): match/required/echo/response fields, latency, drop flag, and
// the spec the server was started with (the caller's stamp — a route
// config declares none). Every list arrives numerically ordered for the
// reading order of field numbers.
func routeDetail(th *theme.Theme, r config.MockRouteConfig, spec string) pages.RouteDetail {
	d := pages.RouteDetail{
		Name:           r.Name,
		Description:    oneLine(r.Description),
		MatchLines:     sortedFieldPairs(th, r.MatchFields),
		RequiredLines:  sortedFieldKeys(r.RequiredFields),
		RespMTI:        r.ResponseMTI,
		RespLines:      sortedFieldPairs(th, r.ResponseFields),
		Spec:           spec,
		Latency:        routeLatencyDetail(r),
		DropConnection: r.DropConnection,
	}
	for _, e := range r.EchoFields {
		d.EchoLines = append(d.EchoLines, strconv.Itoa(e))
	}
	sortFieldKeys(d.EchoLines)

	return d
}

// routeLatencyDetail is the detail view's long-form latency line.
func routeLatencyDetail(r config.MockRouteConfig) string {
	base := routeBaseDelayMs(r)
	if r.JitterMs > 0 {
		return strconv.Itoa(base) + "ms \xc2\xb1" + strconv.Itoa(r.JitterMs) + "ms"
	}

	return strconv.Itoa(base) + "ms"
}

// routeBaseDelayMs mirrors MockRouteConfig.GetTotalDelay's base-delay
// fallback (delay_ms wins, latency_ms is the alias) without its jitter
// random draw — the cell shows the configured values, not a sample.
func routeBaseDelayMs(r config.MockRouteConfig) int {
	if r.DelayMs == 0 && r.LatencyMs > 0 {
		return r.LatencyMs
	}

	return r.DelayMs
}

// sortedFieldPairs renders a match/response field map in reading order
// ("11=000000" per entry — see sortFieldKeys), capping each value: the
// full value is the detail's job, a %v dump of a nested map is not
// readable.
func sortedFieldPairs(th *theme.Theme, fields map[string]any) []string {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sortFieldKeys(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+fieldValue(th, fields[k]))
	}

	return pairs
}

// sortFieldKeys orders route field keys the way their numbers read:
// numeric keys ascending ("3" before "11" — lexicographic order answers
// "11" first), non-numeric keys after them, lexicographic among those.
// One sorter for the match pairs, the response-field pairs, and the
// required and echo lists.
func sortFieldKeys(keys []string) {
	number := func(k string) (int, bool) {
		n, err := strconv.Atoi(k)

		return n, err == nil
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, iNum := number(keys[i])
		nj, jNum := number(keys[j])
		switch {
		case iNum && jNum:
			if ni != nj {
				return ni < nj
			}

			return keys[i] < keys[j]
		case iNum:
			return true
		case jNum:
			return false
		default:
			return keys[i] < keys[j]
		}
	})
}

// sortedFieldKeys returns a numerically ordered copy of a field-key list.
func sortedFieldKeys(keys []string) []string {
	out := append([]string(nil), keys...)
	sortFieldKeys(out)

	return out
}

// oneLine folds a pathological multi-line scalar (a description carrying
// newlines) onto one display line: newlines become spaces, and the
// detail never gains phantom lines from a value.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\n\r") {
		return s
	}

	return strings.Join(strings.Fields(s), " ")
}

// fieldValue renders one field-map value for display: a long string is
// clipped with the theme's ellipsis (ASCII profile included), a map or
// slice is counted rather than dumped, and JSON null is the unknown the
// theme renders as its dash — never Go's "<nil>".
func fieldValue(th *theme.Theme, v any) string {
	switch val := v.(type) {
	case nil:
		return plainDash(th, "")
	case string:
		return th.Truncate(oneLine(val), routeCellValueCells)
	case map[string]any:
		return compositeCount(len(val))
	case []any:
		return compositeCount(len(val))
	default:
		return fmt.Sprintf("%v", v)
	}
}

// compositeCount names the size of a nested value that a cell cannot show
// in full ("2 values", "1 value").
func compositeCount(n int) string {
	if n == 1 {
		return "1 value"
	}

	return strconv.Itoa(n) + " values"
}
