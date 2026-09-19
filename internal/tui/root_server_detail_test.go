// root_server_detail_test.go pins the root-side route→display derivation
// that lives in root_server_detail.go: the summary row cells, the full
// detail pairs in numeric reading order, the value caps, and the spec label
// of the started server (the lifecycle fixtures live in root_server_test.go).
package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"jiso/internal/config"
	"jiso/internal/tui/theme"
)

func TestRootServerStateDerivation(t *testing.T) {
	if got := matchPct(0, 0); got != "" {
		t.Errorf("matchPct(0,0) = %q, want unknown", got)
	}
	if got := matchPct(1198, 1204); got != "99.5%" {
		t.Errorf("matchPct(1198,1204) = %q, want 99.5%%", got)
	}
	// The MATCH cell is a summary: name + matched MTI (+ DE3 when the route
	// narrows one), "any" when the route matches everything.
	th := helpGoldenTheme(colorprofile.TrueColor)
	routes := serveFixtureRoutes()
	if got := routeMatchCell(th, routes[0]); got != "0200/proc 0200/000000" {
		t.Errorf("MTI+DE3 match cell = %q, want \"0200/proc 0200/000000\"", got)
	}
	if got := routeMatchCell(th, routes[1]); got != "0800/nmc any" {
		t.Errorf("catch-all match cell = %q", got)
	}
	if got := routeMatchCell(th, routes[2]); got != "0200/proc-mc 0200" {
		t.Errorf("MTI-only match cell = %q, want \"0200/proc-mc 0200\"", got)
	}
	if got := routeLatencyCell(routes[0]); !strings.Contains(got, "100") || !strings.Contains(got, "25ms") {
		t.Errorf("jitter latency cell = %q, want 100±25ms", got)
	}
	if got := routeBaseDelayMs(config.MockRouteConfig{LatencyMs: 30}); got != 30 {
		t.Errorf("latency_ms alias fallback = %d, want 30", got)
	}
}

// TestRouteMatchCellNeverDumpsPayload: a row cell carries no match pairs.
// Only the MTI and DE3 scalars summarize a route; a long value is clipped
// with the theme's own ellipsis and a nested payload is no summary at all,
// leaving the bare name (the pairs live in the detail).
func TestRouteMatchCellNeverDumpsPayload(t *testing.T) {
	th := helpGoldenTheme(colorprofile.TrueColor)

	cell := routeMatchCell(th, config.MockRouteConfig{
		Name: "Purchase Authorization Approval",
		MatchFields: map[string]any{
			"0": "0200", "3": "000000", "55": map[string]any{"9F26": "040A"},
		},
	})
	if cell != "Purchase Authorization Approval 0200/000000" {
		t.Errorf("match cell = %q, want the MTI/DE3 summary only", cell)
	}

	long := routeMatchCell(th, config.MockRouteConfig{
		Name:        "bulk",
		MatchFields: map[string]any{"0": strings.Repeat("0", 40)},
	})
	if !strings.HasPrefix(long, "bulk ") {
		t.Errorf("long-value cell = %q, want the name kept", long)
	}
	if !strings.HasSuffix(long, theme.GlyphEllipsis) {
		t.Errorf("long-value cell = %q, want it clipped with the theme ellipsis", long)
	}
	if lipgloss.Width(long) > len("bulk ")+routeCellValueCells {
		t.Errorf("long-value cell = %q (%d cells), want the value capped at %d",
			long, lipgloss.Width(long), routeCellValueCells)
	}

	if got := routeMatchCell(th, config.MockRouteConfig{
		Name: "odd", MatchFields: map[string]any{"0": map[string]any{"9F26": "040A"}},
	}); got != "odd" {
		t.Errorf("composite MTI criterion = %q, want the bare name", got)
	}

	ascii := routeMatchCell(theme.NewWith(colorprofile.ASCII, true), serveFixtureRoutes()[1])
	if ascii != "0800/nmc any" {
		t.Errorf("ascii catch-all cell = %q, want it 7-bit and unchanged", ascii)
	}
}

// TestSortedFieldPairsCapsValues: detail lines keep every pair but cap what
// a value can occupy — a long string takes the theme's ellipsis (its ASCII
// profile included), a map or slice is counted, never dumped.
func TestSortedFieldPairsCapsValues(t *testing.T) {
	fields := map[string]any{
		"11": "000000",
		"39": strings.Repeat("x", 30),
		"55": map[string]any{"9F26": "040A", "9F27": "00"},
		"70": []any{"one"},
	}

	uni := sortedFieldPairs(helpGoldenTheme(colorprofile.TrueColor), fields)
	wantUni := []string{
		"11=000000",
		"39=" + strings.Repeat("x", routeCellValueCells-1) + theme.GlyphEllipsis,
		"55=2 values",
		"70=1 value",
	}
	if !slices.Equal(uni, wantUni) {
		t.Errorf("unicode pairs = %q, want %q", uni, wantUni)
	}

	ascii := sortedFieldPairs(theme.NewWith(colorprofile.ASCII, true), fields)
	wantASCII := []string{
		"11=000000",
		"39=" + strings.Repeat("x", routeCellValueCells-1) + theme.ASCIIEllipsis,
		"55=2 values",
		"70=1 value",
	}
	if !slices.Equal(ascii, wantASCII) {
		t.Errorf("ascii pairs = %q, want %q", ascii, wantASCII)
	}
}

// TestSortFieldKeys: field keys order the way their numbers read —
// numeric keys ascending ("3" before "11"; plain lexicographic order
// answers "11" first, the trap the UAT screenshot caught), non-numeric
// keys after them, lexicographic among those.
func TestSortFieldKeys(t *testing.T) {
	keys := []string{"11", "3", "zebra", "0", "49", "apple", "2", "22"}
	sortFieldKeys(keys)

	want := []string{"0", "2", "3", "11", "22", "49", "apple", "zebra"}
	if !slices.Equal(keys, want) {
		t.Errorf("sorted = %q, want %q", keys, want)
	}
}

// TestRootServerDetailDerivation pins the whole routeDetail derivation for
// fixture route 0 — the root-side half of the pages/server_test.go
// serverRoutes() mirror (the import fence forbids sharing the source, so
// this literal must be kept in step with that one).
func TestRootServerDetailDerivation(t *testing.T) {
	th := helpGoldenTheme(colorprofile.TrueColor)

	d := routeDetail(th, serveFixtureRoutes()[0], "visa.json")
	if d.Name != "0200/proc" || d.Description != "purchase auth" {
		t.Errorf("detail head = %q/%q", d.Name, d.Description)
	}
	if want := []string{"0=0200", "3=000000", "11=000000"}; !slices.Equal(d.MatchLines, want) {
		t.Errorf("match lines = %q, want %q", d.MatchLines, want)
	}
	if want := []string{"3", "11"}; !slices.Equal(d.RequiredLines, want) {
		t.Errorf("required lines = %q, want numeric order %q", d.RequiredLines, want)
	}
	if want := []string{"11"}; !slices.Equal(d.EchoLines, want) {
		t.Errorf("echo lines = %q, want %q", d.EchoLines, want)
	}
	if want := []string{"39=00"}; !slices.Equal(d.RespLines, want) {
		t.Errorf("resp field lines = %q, want %q", d.RespLines, want)
	}
	if d.RespMTI != "0210" || d.Latency != "100ms \xc2\xb125ms" {
		t.Errorf("resp mti/latency = %q/%q", d.RespMTI, d.Latency)
	}
	if d.Spec != "visa.json" {
		t.Errorf("detail spec = %q, want the started server's spec", d.Spec)
	}
}

// TestRootServerDetailNamesTheServersSpec: the §9 complaint — the frame
// chip shows the GLOBAL config spec, but the route detail must carry the
// spec the server was actually started with (the fixture injects a
// different one than the config holds).
func TestRootServerDetailNamesTheServersSpec(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	if got := filepath.Base(r.m.app.Config().GetSpec()); got == "visa.json" {
		t.Fatal("fixture must keep the config spec distinct from the server's")
	}
	for _, row := range r.m.serverState().Routes {
		if row.Detail.Spec != "visa.json" {
			t.Errorf("route %q detail spec = %q, want the started spec, not the config's",
				row.ID, row.Detail.Spec)
		}
	}
}

// TestRouteDetailSortsRequiredAndEchoNumerically: the UAT screenshot's
// required list (0 2 3 4 7 11 14 41 49) must come out numerically sorted
// from an unsorted file order — lexicographic sorting puts "11" before
// "2" and "41" before "49".
func TestRouteDetailSortsRequiredAndEchoNumerically(t *testing.T) {
	th := helpGoldenTheme(colorprofile.TrueColor)

	d := routeDetail(th, config.MockRouteConfig{
		RequiredFields: []string{"41", "3", "11", "0", "49", "2", "7", "14", "4"},
		EchoFields:     []int{41, 3, 11, 2},
	}, "")
	if want := []string{"0", "2", "3", "4", "7", "11", "14", "41", "49"}; !slices.Equal(d.RequiredLines, want) {
		t.Errorf("required = %q, want numerically sorted %q", d.RequiredLines, want)
	}
	if want := []string{"2", "3", "11", "41"}; !slices.Equal(d.EchoLines, want) {
		t.Errorf("echo = %q, want numerically sorted %q", d.EchoLines, want)
	}
}

// TestFieldValueNullAndMultiline: a JSON null value is unknown, not
// Go's "<nil>" — it renders the theme dash; and a pathological newline
// inside a scalar collapses to spaces so the detail never gains phantom
// lines.
func TestFieldValueNullAndMultiline(t *testing.T) {
	uni := sortedFieldPairs(helpGoldenTheme(colorprofile.TrueColor), map[string]any{"4": nil})
	if len(uni) != 1 || uni[0] != "4=—" {
		t.Errorf("null pair = %q, want \"4=—\" (the theme dash, not <nil>)", uni)
	}

	as := sortedFieldPairs(theme.NewWith(colorprofile.ASCII, true), map[string]any{"4": nil})
	if len(as) != 1 || as[0] != "4=-" {
		t.Errorf("ascii null pair = %q, want \"4=-\" (7-bit)", as)
	}

	if got := fieldValue(theme.NewWith(colorprofile.ASCII, true), "two\nlines"); got != "two lines" {
		t.Errorf("multi-line scalar = %q, want it folded onto one line", got)
	}

	d := routeDetail(helpGoldenTheme(colorprofile.TrueColor), config.MockRouteConfig{
		Name: "odd", Description: "a\nb",
	}, "")
	if d.Description != "a b" {
		t.Errorf("multi-line description = %q, want \"a b\"", d.Description)
	}
}
