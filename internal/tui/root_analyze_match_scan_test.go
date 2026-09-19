// root_analyze_match_scan_test.go pins the matching wizard's root side:
// the goal teleport, the scan-once cache behind every live-line edit, the
// seed-once starter conditions, the step walk through the interleaved
// rail, and the inline warnings (card data, invalid regex).
package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/utils"
)

func (f *fakeAnalyze) ScanForMatch(_ context.Context, opts app.AnalyzeScanOptions) (*app.AnalyzeScan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scanN++
	f.scanOpts = append(f.scanOpts, opts)
	if f.scanErr != nil {
		return nil, f.scanErr
	}

	return f.scan, nil
}

// fakeScanFixture pairs two exchanges that vary DE4 (100/200) and DE39
// (00/05) with a constant DE0/DE3 — the matching wizard's group-by
// suggestions and seed source.
func fakeScanFixture() *app.AnalyzeScan {
	// fakeAnalyzeFixture has no *testing.T: the fixture spec guarantees
	// these fields, so a Field error is a fixture bug and a panic
	// (test-visible) beats a silently missing pair.
	build := func(mti string, f map[int]string) *iso8583.Message {
		m := iso8583.NewMessage(utils.GetDefaultSpec())
		m.MTI(mti)
		for id, v := range f {
			if err := m.Field(id, v); err != nil {
				panic("scan fixture: " + err.Error())
			}
		}

		return m
	}
	pair := func(amount, rc string) analyzer.MatchPair {
		return analyzer.MatchPair{
			Request:  build("0200", map[int]string{3: "000000", 4: amount}),
			Response: build("0210", map[int]string{39: rc}),
		}
	}

	return &app.AnalyzeScan{
		Pairs: []analyzer.MatchPair{pair("100", "00"), pair("200", "05")},
		Variances: []app.FieldVariance{
			{Field: "4", Side: analyzer.CondSideReq, Distinct: 2, Sample: []string{"100", "200"}},
			{Field: "39", Side: analyzer.CondSideResp, Distinct: 2, Sample: []string{"00", "05"}},
		},
	}
}

// walkToMatch drives the classic walk, switches to the mock-routes goal
// (the teleport), and lands on the matching step with the scan folded.
func walkToMatch(t *testing.T) (*analyzeTestRoot, *fakeAnalyze) {
	t.Helper()
	fake := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, fake)
	r.walkToRun(t)
	r.pump(pages.AnalyzeChooseGoalMsg{Goal: pages.AnalyzeGoalMockRoutes})
	r.mustStep(t, pages.StepMatching)

	return r, fake
}

func TestAnalyzeMatchTeleportScansAndSeeds(t *testing.T) {
	r, fake := walkToMatch(t)

	if fake.scanN != 1 {
		t.Fatalf("scan legs = %d, want exactly 1", fake.scanN)
	}
	if len(r.m.analyzeConds) != 2 {
		t.Fatalf("seed conds = %+v, want the headline 0/3 pair", r.m.analyzeConds)
	}
	if c := r.m.analyzeConds[0]; c.Field != "0" || c.Value != "0200" || c.Cond != "equals" {
		t.Errorf("seed MTI cond = %+v", c)
	}
	if c := r.m.analyzeConds[1]; c.Field != "3" || c.Value != "000000" {
		t.Errorf("seed DE3 cond = %+v", c)
	}
	if r.m.analyzeMatchLine != "~2 pairs match · 1 route(s)" {
		t.Errorf("live line = %q", r.m.analyzeMatchLine)
	}
}

func TestAnalyzeMatchEditsRecomputeWithoutRescan(t *testing.T) {
	r, fake := walkToMatch(t)

	// Group by request DE4: the two fixture pairs split into two routes.
	r.pump(pages.AnalyzeGroupToggleMsg{Field: "4", Side: analyzer.CondSideReq})
	if r.m.analyzeMatchLine != "~2 pairs match · 2 route(s)" {
		t.Errorf("after group: %q", r.m.analyzeMatchLine)
	}

	// A response-side cond 39=05 keeps only the RC-05 pair.
	r.pump(pages.AnalyzeCondAddMsg{})
	r.pump(pages.AnalyzeCondSetFieldMsg{Index: 2, Value: "39"})
	r.pump(pages.AnalyzeCondSideMsg{Index: 2})
	r.pump(pages.AnalyzeCondSetValueMsg{Index: 2, Value: "05"})
	if r.m.analyzeMatchLine != "~1 pairs match · 1 route(s)" {
		t.Errorf("after resp cond: %q", r.m.analyzeMatchLine)
	}

	// Every edit was answered from the scan cache: exactly one leg ran.
	if fake.scanN != 1 {
		t.Fatalf("scan legs = %d, want 1 (the cache must serve every edit)", fake.scanN)
	}
	if !r.m.analyzeRunStale {
		t.Error("match edits must mark the run stale")
	}
}

func TestAnalyzeMatchCondWhenCarriesValueAcrossListBoundary(t *testing.T) {
	r, _ := walkToMatch(t)

	// Cycle the seed DE3 cond (equals "000000") along the ladder across
	// the scalar/list boundary: entering a list condition carries the
	// scalar into a one-value list, leaving carries it back.
	r.pump(pages.AnalyzeCondWhenMsg{Index: 1}) // -> exists
	r.pump(pages.AnalyzeCondWhenMsg{Index: 1}) // -> prefix
	r.pump(pages.AnalyzeCondWhenMsg{Index: 1}) // -> one-of
	if c := r.m.analyzeConds[1]; c.Cond != "oneof" || len(c.Values) != 1 || c.Values[0] != "000000" {
		t.Fatalf("cycle into list = %+v", c)
	}
	r.pump(pages.AnalyzeCondWhenMsg{Index: 1}) // -> regex
	r.pump(pages.AnalyzeCondWhenMsg{Index: 1}) // -> not-in
	r.pump(pages.AnalyzeCondWhenMsg{Index: 1}) // -> equals: value returns
	if c := r.m.analyzeConds[1]; c.Cond != "equals" || c.Value != "000000" {
		t.Errorf("cycle back to equals = %+v, want the value carried", c)
	}
}

func TestAnalyzeMatchWarningsInline(t *testing.T) {
	r, _ := walkToMatch(t)

	// A card-data condition names itself in the wizard.
	r.pump(pages.AnalyzeCondAddMsg{})
	r.pump(pages.AnalyzeCondSetFieldMsg{Index: 2, Value: "2"})
	r.pump(pages.AnalyzeCondSetValueMsg{Index: 2, Value: "4111111111111111"})
	if !strings.Contains(r.m.analyzeMatchWarn, "anonymized") {
		t.Errorf("card cond warn = %q", r.m.analyzeMatchWarn)
	}
	if !strings.Contains(ansi.Strip(r.view()), "anonymized") {
		t.Error("the wizard body must show the card warning")
	}

	// An invalid regex is named, never silently zero-matching.
	r.pump(pages.AnalyzeCondSetFieldMsg{Index: 2, Value: "0"})
	r.pump(pages.AnalyzeCondWhenMsg{Index: 2}) // -> exists
	r.pump(pages.AnalyzeCondWhenMsg{Index: 2}) // -> prefix
	r.pump(pages.AnalyzeCondWhenMsg{Index: 2}) // -> one-of
	r.pump(pages.AnalyzeCondWhenMsg{Index: 2}) // -> regex
	r.pump(pages.AnalyzeCondSetValueMsg{Index: 2, Value: "[("})
	if !strings.Contains(r.m.analyzeMatchWarn, "invalid regex") {
		t.Errorf("regex warn = %q", r.m.analyzeMatchWarn)
	}
}

func TestAnalyzeMatchStepWalkAndTeleportAway(t *testing.T) {
	r, fake := walkToMatch(t)

	// Esc from matching walks back to header (the interleaved rail).
	r.pump(special(tea.KeyEscape))
	r.mustStep(t, pages.StepHeader)

	// Forward from header enters matching (routes rail position).
	r.pump(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.mustStep(t, pages.StepMatching)

	// Changing goal to transactions relocates to run; the rail is back
	// to four steps and the scan leg ran once more for the re-entry.
	if fake.scanN < 2 {
		t.Fatalf("scan legs = %d, want a fresh arm per arrival", fake.scanN)
	}
	r.pump(pages.AnalyzeChooseGoalMsg{Goal: pages.AnalyzeGoalTransactions})
	r.mustStep(t, pages.StepRun)
}
