// root_analyze_unparsable_test.go pins the UAT round 6/7 reviewer data
// path on the root side: the enumeration fold stores the collected
// samples as a reviewer roster (and bumps its id). The hexdump/describe
// rendering itself lives in the pages package.
package tui

import (
	"strings"
	"testing"

	"jiso/internal/analyzer"
)

func TestRootAnalyzeEnumStoresUnparsableSamples(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	f.enum.Samples = []analyzer.UnparsableSample{
		{Offset: 56, Length: 44, Reason: "field 2: unexpected EOF", Head: []byte("0200")},
	}

	r.walkToRun(t) // the enumeration folds on entry to the run step

	if len(r.m.analyzeUnparsableRows) != 1 {
		t.Fatalf("enum fold stored %d reviewer rows, want 1", len(r.m.analyzeUnparsableRows))
	}
	if got := r.m.analyzeUnparsableRows[0].Offset; got != "56" {
		t.Errorf("roster offset = %q, want 56", got)
	}
	if r.m.analyzeUnparsableID != 1 {
		t.Fatalf("analyzeUnparsableID = %d, want 1 (bumped per enumeration)", r.m.analyzeUnparsableID)
	}
	if v := r.view(); !strings.Contains(v, "[u] review") {
		t.Errorf("the flows line must offer [u] review once samples exist:\n%s", v)
	}
}
