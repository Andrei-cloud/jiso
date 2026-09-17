// root_ctf_test.go proves the root contract with a fake façade
// (no real DB above the seam): entry onto §K arms the eligible-list
// query off the UI thread, Enter runs the dry preview (overlay opens
// with count + first/last records, nothing written), w stats root-side
// and either writes once with the previewed args or opens the §N3
// overwrite confirm (default No — cancel writes nothing, confirm
// proceeds), the write result lands as the toast line, a stale seq
// (page left mid-preview) is dropped, `r` re-queries, and typed DB
// errors render as empty-state text.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	app "jiso/internal/app"
	"jiso/internal/db"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
)

// fakeCtf is the injectable §K façade: canned rows, call counters,
// recorded args, and a virtual filesystem (statExists) — the fake
// never touches disk.
type fakeCtf struct {
	mu       sync.Mutex
	path     string
	sessions []app.CtfSessionView
	summary  *app.CtfExportSummary
	listErr  error
	writeErr error

	listN, previewN, writeN int
	previewArgs, writeArgs  []string
	statExists              map[string]bool
	statN                   int
}

func (f *fakeCtf) DBPath() string { return f.path }

func (f *fakeCtf) ListCtfSessions(_ context.Context) ([]app.CtfSessionView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listN++
	if f.listErr != nil {
		return nil, f.listErr
	}

	return append([]app.CtfSessionView(nil), f.sessions...), nil
}

func (f *fakeCtf) PreviewExport(_ context.Context, id, cib, bin string, batch int) (*app.CtfExportSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previewN++
	f.previewArgs = append(f.previewArgs, fmt.Sprintf("%s|%s|%s|%d", id, cib, bin, batch))
	if f.summary == nil {
		return nil, fmt.Errorf("no approved transactions found in session %s", id)
	}
	s := *f.summary
	s.SessionID = id

	return &s, nil
}

func (f *fakeCtf) WriteExport(_ context.Context, id, cib, bin string, batch int, out string) (*app.CtfExportSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeN++
	f.writeArgs = append(f.writeArgs, fmt.Sprintf("%s|%s|%s|%d|%s", id, cib, bin, batch, out))
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	s := *f.summary
	s.SessionID, s.OutputPath, s.Written, s.DryRun = id, out, true, false
	s.Overwrote = f.statExists[out]

	return &s, nil
}

func (f *fakeCtf) stat(path string) (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statN++
	if f.statExists[filepath.Clean(path)] {
		return nil, nil //nolint:nilnil // virtual "exists" verdict only
	}

	return nil, os.ErrNotExist
}

// fakeCtfFixture is the §K data in façade shapes.
func fakeCtfFixture() *fakeCtf {
	ts := func(h, m int) time.Time { return time.Date(2026, 9, 9, h, m, 0, 0, time.UTC) }

	return &fakeCtf{
		path: "./sessions.db",
		sessions: []app.CtfSessionView{
			{SessionID: "9f3ca1e2b7d84455a1", LastActiveTime: ts(12, 1), ApprovedCount: 148},
			{SessionID: "77b255c9e4d3", LastActiveTime: ts(9, 55), ApprovedCount: 22},
		},
		summary: &app.CtfExportSummary{
			Records: 3, MonetaryTransactions: 148, DestinationAmountSum: 1245000,
			FirstRecord: "05004242424242424242 FIRST", LastRecord: "920400129 LAST",
			RecordLines: []string{"05004242424242424242 FIRST", "02004242424242424242 SECOND", "920400129 LAST"},
			OutputPath:  "./out/CTF_001.dat",
		},
		statExists: map[string]bool{},
	}
}

type ctfTestRoot struct {
	m     *RootModel
	clock time.Time
}

func newCtfTestRoot(t *testing.T, fake *fakeCtf) *ctfTestRoot {
	t.Helper()
	r := &ctfTestRoot{m: NewRootModel(nil), clock: time.Date(2026, 9, 9, 12, 4, 11, 0, time.UTC)}
	t.Setenv("JISO_ASCII", "")
	r.m.theme = theme.NewWith(colorprofile.ASCII, true)
	r.m.now = func() time.Time { return r.clock }
	if fake != nil {
		r.m.ctfSrc = fake
		r.m.ctfStatFn = fake.stat
	}
	r.pump(tea.WindowSizeMsg{Width: 120, Height: 40}) // pumped (resize coalescer)

	return r
}

func (r *ctfTestRoot) pump(msg tea.Msg) { r.pumpN(msg, 0) }

// pumpN pumps msg (and results); when keep > 0 the first returned cmd
// is deferred and returned instead of being run.
func (r *ctfTestRoot) pumpN(msg tea.Msg, keep int) tea.Cmd {
	var deferred tea.Cmd
	queue := []tea.Msg{msg}
	for i := 0; i < 32 && len(queue) > 0; i++ {
		cmd := r.upd(queue[0])
		queue = queue[1:]
		if keep > 0 && deferred == nil && cmd != nil {
			deferred = cmd

			continue
		}
		queue = append(queue, flattenMsgs(cmd)...)
	}

	return deferred
}

func (r *ctfTestRoot) upd(msg tea.Msg) tea.Cmd {
	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		panic("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

// gotoPage jumps to §K through the palette resolution (no hotkey: the
// 1..8 slots keep their wire-compat targets) and nudges the size.
func (r *ctfTestRoot) gotoPage() {
	r.pump(palette.GoToPageMsg{ID: pages.CtfPageID})
	r.pump(tea.WindowSizeMsg{Width: 121, Height: 41})
}

func (r *ctfTestRoot) body() string { return r.m.View().Content }

func (r *ctfTestRoot) generate() {
	r.pump(pages.CtfGenerateMsg{SessionID: "9f3ca1e2b7d84455a1", Params: pages.CtfParams{
		CIB: "400129", Batch: "1", OutPath: "./out/CTF_001.dat",
	}})
}

func TestCtfEntryLoadsEligibleList(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()

	body := r.body()
	for _, want := range []string{
		"VISA BASE II - CTF EXPORT", "9f3c~a1", "today 12:01",
		"148 approved", "22 approved", "400129", "./out/CTF_001.dat",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page body lacks %q:\n%s", want, body)
		}
	}
	if fake.listN != 1 {
		t.Fatalf("list calls = %d, want 1", fake.listN)
	}
	if fake.previewN != 0 || fake.writeN != 0 {
		t.Fatalf("entry must not preview or write: %d/%d", fake.previewN, fake.writeN)
	}
}

func TestCtfGenerateOpensPreviewOverlay(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.generate()

	body := r.body()
	for _, want := range []string{
		"RECORDS", "05004242424242424242 FIRST",
		"920400129 LAST", "3 records", "./out/CTF_001.dat (3 records)",
		"0----+----1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overlay body lacks %q:\n%s", want, body)
		}
	}
	if !r.m.ctf.PreviewOpen() {
		t.Fatalf("overlay must open after the preview result")
	}
	if fake.previewN != 1 || fake.writeN != 0 {
		t.Fatalf("calls preview=%d write=%d, want 1/0", fake.previewN, fake.writeN)
	}
	if fake.previewArgs[0] != "9f3ca1e2b7d84455a1|400129||1" {
		t.Fatalf("preview args = %q, want the committed form values", fake.previewArgs[0])
	}
}

func TestCtfWriteRunsOnceWithPreviewedArgs(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.generate()
	r.pump(pages.CtfWriteMsg{})

	if fake.writeN != 1 {
		t.Fatalf("write calls = %d, want 1", fake.writeN)
	}
	if fake.writeArgs[0] != "9f3ca1e2b7d84455a1|400129||1|./out/CTF_001.dat" {
		t.Fatalf("write args = %q", fake.writeArgs[0])
	}
	body := r.body()
	if !strings.Contains(body, "wrote 3 records to ./out/CTF_001.dat") {
		t.Errorf("write result line missing:\n%s", body)
	}
	// The expectation reads the separator off the same theme the render used,
	// so it follows the policy instead of pinning one spelling of it.
	if want := "148 tx" + r.m.themeOrNil().Separator() + "$ 12,450.00 total"; !strings.Contains(body, want) {
		t.Errorf("SUMMARY line missing after the overlay closed:\n%s", body)
	}
}

func TestCtfOverwriteAsksConfirmAndCancelWritesNothing(t *testing.T) {
	fake := fakeCtfFixture()
	fake.statExists["out/CTF_001.dat"] = true
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.generate()
	r.pump(pages.CtfWriteMsg{})

	if fake.writeN != 0 {
		t.Fatalf("existing target must not be written silently")
	}
	if r.m.ctfConfirm == nil || !strings.Contains(r.body(), "overwrite ./out/CTF_001.dat?") {
		t.Fatalf("§N3 confirm must be pending, body:\n%s", r.body())
	}
	r.pump(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if fake.writeN != 0 || r.m.ctfConfirm != nil {
		t.Fatalf("n must cancel the write (writes=%d)", fake.writeN)
	}
}

func TestCtfOverwriteConfirmProceeds(t *testing.T) {
	fake := fakeCtfFixture()
	fake.statExists["out/CTF_001.dat"] = true
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.generate()
	r.pump(pages.CtfWriteMsg{})
	r.pump(tea.KeyPressMsg{Code: 'y', Text: "y"})

	if fake.writeN != 1 {
		t.Fatalf("confirmed overwrite must write once, got %d", fake.writeN)
	}
	if fake.writeArgs[0] != "9f3ca1e2b7d84455a1|400129||1|./out/CTF_001.dat" {
		t.Fatalf("confirmed write args = %q", fake.writeArgs[0])
	}
}

func TestCtfLeaveCancelsInFlightPreview(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()

	deferred := r.pumpN(pages.CtfGenerateMsg{
		SessionID: "9f3ca1e2b7d84455a1",
		Params:    pages.CtfParams{CIB: "400129", Batch: "1", OutPath: "./out/CTF_001.dat"},
	}, 1)
	if deferred == nil {
		t.Fatalf("generate must arm a preview cmd")
	}
	r.pump(ch('1')) // leave to the dashboard: the seq bump cancels
	r.pump(deferred())

	if r.m.ctf.PreviewOpen() || r.m.ctfPreviewID != 0 || r.m.ctfSummary != nil {
		t.Fatalf("stale preview result must be dropped after leaving §K")
	}
}

func TestCtfRefreshRequeries(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.pump(pages.CtfRefreshMsg{})
	if fake.listN != 2 {
		t.Fatalf("list calls = %d, want 2 after r", fake.listN)
	}
}

func TestCtfMissingDBIsEmptyStateText(t *testing.T) {
	fake := fakeCtfFixture()
	fake.listErr = &app.ConfigError{Path: "./gone.db", Err: db.ErrDBNotFound}
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	if body := r.body(); !strings.Contains(body, "no CTF-eligible sessions in ./gone.db") {
		t.Errorf("missing-db empty state missing:\n%s", body)
	}
}

func TestCtfWriteFailureLine(t *testing.T) {
	fake := fakeCtfFixture()
	fake.writeErr = fmt.Errorf("failed to write CTF file to ./out: is a directory")
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.generate()
	r.pump(pages.CtfWriteMsg{})
	if body := r.body(); !strings.Contains(body, "write failed: failed to write CTF file to ./out") {
		t.Errorf("write failure line missing:\n%s", body)
	}
}

func TestCtfNoLegStaysEmpty(t *testing.T) {
	r := newCtfTestRoot(t, nil)
	r.gotoPage()
	if body := r.body(); !strings.Contains(body, "database not configured") {
		t.Errorf("no-leg empty state missing:\n%s", body)
	}
}

// --- regression tests --------------------------------------

// TestCtfStaleListClearsWaitAndReArms: generate bumps the seq while a
// list load is in flight; the stale result must clear ctfListWait (the
// applySessionsDetail pattern) so the wrapper re-arms — the old
// stale-return kept the flag true and froze armCtf for the session.
func TestCtfStaleListClearsWaitAndReArms(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)

	listCmd := r.pumpN(palette.GoToPageMsg{ID: pages.CtfPageID}, 1) // arm & hold
	if listCmd == nil {
		t.Fatal("entry must arm the eligible-list load")
	}
	r.pump(tea.WindowSizeMsg{Width: 121, Height: 41})
	if !r.m.ctfListWait {
		t.Fatal("list load must be in flight")
	}

	r.generate() // Enter bumps the seq; the preview leg lands normally
	if fake.previewN != 1 {
		t.Fatalf("previewN = %d, want 1", fake.previewN)
	}

	r.pump(listCmd()) // the now-stale list result must NOT wedge the loader
	if fake.listN != 2 {
		t.Fatalf("list calls = %d, want the wrapper to re-arm (2)", fake.listN)
	}
	if !r.m.ctfListLoaded || len(r.m.ctfList) == 0 {
		t.Fatalf("re-armed list never landed: loaded=%v n=%d", r.m.ctfListLoaded, len(r.m.ctfList))
	}
}

// TestCtfPopBumpsSeqAndDropsStaleLeg: Esc-pop must run leaveCtf (the
// analyze handleAnalyzeAbort→doAnalyzeAbort precedent) — without the
// bump an in-flight leg lands on the page the user left.
func TestCtfPopBumpsSeqAndDropsStaleLeg(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)

	r.m.Push(r.m.ctf) // depth 2, ctf current
	listCmd := r.pumpN(tea.WindowSizeMsg{Width: 121, Height: 41}, 1)
	if listCmd == nil {
		t.Fatal("pushed §K must arm its list load")
	}
	seqBefore := r.m.ctfSeq

	r.pump(pages.CtfPopMsg{})
	if r.m.ctfSeq == seqBefore {
		t.Fatal("Esc-pop must bump the seq (leaveCtf)")
	}
	wantStack(t, r.m, "dashboard")

	r.pump(listCmd()) // stale: must not mutate state on the page left
	if r.m.ctfListLoaded || r.m.ctfList != nil {
		t.Fatalf("stale list folded after pop: loaded=%v list=%v", r.m.ctfListLoaded, r.m.ctfList)
	}
}

// TestCtfOverwritePathKeepsQuestionMark: the confirmed write uses the
// stored m.ctfParams.OutPath — an OutPath ending "?" survives intact
// (the old question-text round-trip mangled it).
func TestCtfOverwritePathKeepsQuestionMark(t *testing.T) {
	fake := fakeCtfFixture()
	out := "./out/CTF_001?.dat"
	fake.statExists["out/CTF_001?.dat"] = true // fake stat cleans the key
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.pump(pages.CtfGenerateMsg{SessionID: "9f3ca1e2b7d84455a1", Params: pages.CtfParams{
		CIB: "400129", Batch: "1", OutPath: out,
	}})
	r.pump(pages.CtfWriteMsg{})
	if r.m.ctfConfirm == nil || !r.m.ctfConfirm.Pending() {
		t.Fatalf("existing target must open the overwrite confirm:\n%s", r.body())
	}

	r.pump(ch('y'))
	if fake.writeN != 1 {
		t.Fatalf("write calls = %d, want 1", fake.writeN)
	}
	want := "9f3ca1e2b7d84455a1|400129||1|" + out
	if fake.writeArgs[0] != want {
		t.Fatalf("write args = %q, want %q (path round-trip mangling)", fake.writeArgs[0], want)
	}
}

// TestCtfBatchNonPositiveRejected: "0" and "-5" parse but must be
// rejected with the same inline error as a non-numeric batch.
func TestCtfBatchNonPositiveRejected(t *testing.T) {
	for _, batch := range []string{"0", "-5"} {
		fake := fakeCtfFixture()
		r := newCtfTestRoot(t, fake)
		r.gotoPage()

		r.pump(pages.CtfGenerateMsg{SessionID: "9f3ca1e2b7d84455a1", Params: pages.CtfParams{
			CIB: "400129", Batch: batch, OutPath: "./out/CTF_001.dat",
		}})
		if fake.previewN != 0 {
			t.Fatalf("batch %q ran the preview leg: %d", batch, fake.previewN)
		}
		if !strings.Contains(r.m.ctfNote, "batch number must be a positive integer") {
			t.Fatalf("batch %q note = %q", batch, r.m.ctfNote)
		}
	}
}

// TestCtfCursorMoveRunsDryLegWithoutOverlay: the SUMMARY
// follows the list cursor. Moving onto the second session runs the dry
// preview for THAT session and folds the summary with the viewer
// closed; Enter then REUSES the computed summary (no second query) and
// opens the viewer on it.
func TestCtfCursorMoveRunsDryLegWithoutOverlay(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.pump(tea.KeyPressMsg{Code: tea.KeyDown}) // cursor onto the 2nd session

	if fake.previewN != 1 {
		t.Fatalf("preview calls = %d, want the cursor-following dry leg", fake.previewN)
	}
	if fake.previewArgs[0] != "77b255c9e4d3|400129||1" {
		t.Fatalf("preview args = %q, want the session under the cursor", fake.previewArgs[0])
	}
	if r.m.ctf.PreviewOpen() {
		t.Fatal("the select leg must not open the viewer")
	}
	if !strings.Contains(r.body(), "148 tx") {
		t.Fatalf("SUMMARY did not fold for the cursor row:\n%s", r.body())
	}

	r.pump(pages.CtfGenerateMsg{SessionID: "77b255c9e4d3", Params: pages.CtfParams{
		CIB: "400129", Batch: "1", OutPath: "./out/CTF_001.dat",
	}})
	if fake.previewN != 1 {
		t.Fatalf("Enter re-ran the preview (%d), want the fresh summary reused", fake.previewN)
	}
	if !r.m.ctf.PreviewOpen() {
		t.Fatal("Enter must open the viewer on the reused summary")
	}
}

// TestCtfSelectStaleResultDropped: two dry legs in flight
// carry arm-time seqs; the first one's late result must not fold over
// the newer one (the serverStats seq lifecycle).
func TestCtfSelectStaleResultDropped(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	params := pages.CtfParams{CIB: "400129", Batch: "1", OutPath: "./out/CTF_001.dat"}

	cmdA := r.pumpN(pages.CtfSelectMsg{SessionID: "77b255c9e4d3", Params: params}, 1)
	if cmdA == nil {
		t.Fatal("the select must arm the root-side dry leg")
	}
	r.pump(pages.CtfSelectMsg{SessionID: "9f3ca1e2b7d84455a1", Params: params}) // leg B runs, folds
	if fake.previewN != 1 || r.m.ctfSummaryID != "9f3ca1e2b7d84455a1" {
		t.Fatalf("leg B: previewN=%d summaryID=%s, want 1/9f3c", fake.previewN, r.m.ctfSummaryID)
	}

	r.pump(cmdA()) // A's late result: stale seq, must be dropped
	if fake.previewN != 2 {
		t.Fatalf("previewN = %d, want A delivered late", fake.previewN)
	}
	if r.m.ctfSummaryID != "9f3ca1e2b7d84455a1" {
		t.Fatalf("summary id = %q after the stale fold, want the newer B result", r.m.ctfSummaryID)
	}
}

// TestCtfFormEditRerunsDryLeg: a PARAMETERS edit (here a
// CIB digit) re-runs the dry leg for the row under the cursor, so the
// SUMMARY tracks the form, not just the cursor.
func TestCtfFormEditRerunsDryLeg(t *testing.T) {
	fake := fakeCtfFixture()
	r := newCtfTestRoot(t, fake)
	r.gotoPage()
	r.pump(pages.PaneFocusMsg{}) // list -> params (focus ring lands on CIB)
	r.pump(tea.KeyPressMsg{Code: '7', Text: "7"})

	if fake.previewN != 1 {
		t.Fatalf("preview calls = %d, want the edit to re-run the dry leg", fake.previewN)
	}
	if fake.previewArgs[0] != "9f3ca1e2b7d84455a1|4001297||1" {
		t.Fatalf("preview args = %q, want the edited CIB under the cursor row", fake.previewArgs[0])
	}
}
