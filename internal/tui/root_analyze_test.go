// root_analyze_test.go proves the root contract with a fake
// §J façade (no real pcap/engine above the seam): the wizard walks
// capture ▸ spec ▸ header ▸ run; the capture commit validates the file
// inline (a missing path stays on the step); entering the run step
// enumerates flows off the UI thread; Enter runs with the folded
// inline options (the "/" filter mapped to the dst port set, "" = all,
// a filter matching none a note, never a fabricated selection); the
// preview precedes any write; w writes exactly once behind the §N3
// overwrite confirm; abort over an in-flight leg asks confirm first;
// step jumps / page leaves / aborts cancel in-flight legs via the seq
// token; enumeration and spec errors surface as inline text; and a
// root without any leg just reports the missing leg.
package tui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
	"jiso/internal/utils"
)

// fakeAnalyze is the injectable §J engine façade: canned enumeration
// and run results, call counters, and recorded arguments. It never
// blocks (the tests run the returned cmds).
type fakeAnalyze struct {
	mu sync.Mutex

	enum     *app.AnalyzeEnumeration
	enumErr  error
	statErrs map[string]error
	runOut   *app.AnalyzeOutput
	runErr   error
	writeErr error

	statN, enumN, runN, writeN int
	enumPaths                  []string
	enumHeaders                []string
	enumSpecs                  []string
	runOpts                    []app.AnalyzeRunOptions
	written                    []string
	writtenSel                 []int
}

func (f *fakeAnalyze) StatPath(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statN++
	if err, ok := f.statErrs[path]; ok {
		return err
	}

	return nil
}

func (f *fakeAnalyze) EnumerateFlows(_ context.Context, pcapPath, headerType, specPath string) (*app.AnalyzeEnumeration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enumN++
	f.enumPaths = append(f.enumPaths, pcapPath)
	f.enumHeaders = append(f.enumHeaders, headerType)
	f.enumSpecs = append(f.enumSpecs, specPath)
	if f.enumErr != nil {
		return nil, f.enumErr
	}

	return f.enum, nil
}

func (f *fakeAnalyze) RunAnalyze(_ context.Context, opts app.AnalyzeRunOptions) (*app.AnalyzeOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runN++
	f.runOpts = append(f.runOpts, opts)
	if f.runErr != nil {
		return nil, f.runErr
	}

	return f.runOut, nil
}

func (f *fakeAnalyze) WriteAnalyze(_ context.Context, out *app.AnalyzeOutput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeN++
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written = append(f.written, out.OutputFile)
	f.writtenSel = append(f.writtenSel, len(out.SelectedItems()))

	return nil
}

// fakeAnalyzeFixture is the §J enumeration: two dst flows
// (8080 dominant, 9999 small) each with the src response half.
func fakeAnalyzeFixture() *fakeAnalyze {
	f := &fakeAnalyze{
		enum: &app.AnalyzeEnumeration{
			Parsed: 412, Unparsable: 3,
			Flows: []app.AnalyzeFlowView{
				{
					Key: "a", Direction: "dst", ServerPort: 8080, Count: 205,
					MTIHistogram: []app.AnalyzeMTICount{{MTI: "0200", Count: 180}, {MTI: "0800", Count: 25}},
					SignonCount:  25,
				},
				{
					Key: "b", Direction: "src", ServerPort: 8080, Count: 184,
					MTIHistogram: []app.AnalyzeMTICount{{MTI: "0210", Count: 178}, {MTI: "0810", Count: 6}},
				},
				{
					Key: "c", Direction: "dst", ServerPort: 9999, Count: 23,
					MTIHistogram: []app.AnalyzeMTICount{{MTI: "0200", Count: 23}},
				},
				{
					Key: "d", Direction: "src", ServerPort: 9999, Count: 23,
					MTIHistogram: []app.AnalyzeMTICount{{MTI: "0210", Count: 23}},
				},
			},
		},
		runOut: &app.AnalyzeOutput{Mode: app.AnalyzeModeTx, OutputFile: "transactions/transaction.json"},
	}
	// The generated-item picker needs a realistic roster.
	f.runOut.AttachGeneratedItems([]config.Item{
		{Type: config.TypeTransaction, Name: "Captured Flow 0200_0", DatasetName: "dataset_0200_"},
		{Type: config.TypeDataset, Name: "dataset_0200_"},
		{Type: config.TypeMockRoute, Name: "route-0200-00"},
	})

	return f
}

type analyzeTestRoot struct {
	m    *RootModel
	pcap string // a REAL file: the capture commit stats inline
}

func newAnalyzeTestRoot(t *testing.T, fake *fakeAnalyze) *analyzeTestRoot {
	t.Helper()
	r := &analyzeTestRoot{m: NewRootModel(nil)}
	t.Setenv("JISO_ASCII", "")
	r.m.theme = theme.NewWith(colorprofile.ASCII, true)
	if fake != nil {
		r.m.analyzeSrc = fake
	}
	// Default the §N3 overwrite stat to "target absent" so the write
	// tests stay disk-free and land straight on the write leg; the
	// confirm test overrides it.
	r.m.analyzeStatFn = func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist }
	// The capture commit is a synchronous os.Stat (the wizardChooseFile
	// idiom), so the typed path must exist on disk.
	r.pcap = filepath.Join(t.TempDir(), "cap.pcap")
	if err := os.WriteFile(r.pcap, []byte("d4c3b2a1"), 0o644); err != nil {
		t.Fatalf("temp pcap: %v", err)
	}
	r.pump(tea.WindowSizeMsg{Width: 120, Height: 40})

	return r
}

// pump feeds msg and every resulting command result back through
// Update until quiescent (shared semantics with the §I harness).
func (r *analyzeTestRoot) pump(msg tea.Msg) {
	queue := []tea.Msg{msg}
	for i := 0; i < 48 && len(queue) > 0; i++ {
		cmd := r.upd(queue[0])
		queue = queue[1:]
		queue = append(queue, flattenMsgs(cmd)...)
	}
}

func (r *analyzeTestRoot) upd(msg tea.Msg) tea.Cmd {
	next, cmd := r.m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		panic("root Update returned a non-root model")
	}
	r.m = rm

	return cmd
}

func (r *analyzeTestRoot) typeText(s string) {
	for _, c := range s {
		r.pump(ch(c))
	}
}

func (r *analyzeTestRoot) enter() { r.pump(tea.KeyPressMsg{Code: tea.KeyEnter}) }

// closePicker dismisses the generated-item picker a run auto-presents
// so run-step keys below it can be driven. Guarded: it
// only pumps Esc when the run step actually claims the keyboard (the
// picker or the filter), never backing out of a step.
func (r *analyzeTestRoot) closePicker() {
	t := r.m.analyzeStep == pages.StepRun && r.m.analyze != nil && r.m.analyze.ClaimsKeyboard()
	if t {
		r.pump(tea.KeyPressMsg{Code: tea.KeyEscape})
	}
}

func (r *analyzeTestRoot) view() string { return r.m.View().Content }

// gotoAnalyze jumps to §J (hotkey 7) and nudges the size so the fresh
// page lays out wide.
func (r *analyzeTestRoot) gotoAnalyze() {
	r.pump(ch('7'))
	r.pump(tea.WindowSizeMsg{Width: 121, Height: 41})
}

// commitCapture types the capture path and Enters (the commit stats
// the file inline and advances to the spec step).
func (r *analyzeTestRoot) commitCapture(path string) {
	r.typeText(path)
	r.enter()
}

// walkToRun enters the wizard, commits the real temp capture, passes
// the spec step on the engine default (""), and Enters through the
// header step; the pump folds the enumeration armed on run-step entry.
func (r *analyzeTestRoot) walkToRun(t *testing.T) {
	t.Helper()
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	r.mustStep(t, pages.StepSpec)
	r.enter() // "" spec = the engine default
	r.mustStep(t, pages.StepHeader)
	r.enter() // → run: enumeration armed and folded by the pump
	r.mustStep(t, pages.StepRun)
}

// async folds one key through the page's message-emitter cmd and
// returns the root-side leg cmd it armed, leaving that leg unrun so
// tests can hold it in flight.
func (r *analyzeTestRoot) async(msg tea.Msg) tea.Cmd {
	emit := r.upd(msg)
	if emit == nil {
		return nil
	}
	inner := emit()
	if inner == nil {
		return nil
	}

	return r.upd(inner)
}

// enterAsync Enters and returns the (unrun) leg cmd the transition
// armed, so tests can hold a leg in flight.
func (r *analyzeTestRoot) enterAsync() tea.Cmd {
	return r.async(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func (r *analyzeTestRoot) mustStep(t *testing.T, want int) {
	t.Helper()
	if r.m.analyzeStep != want {
		t.Fatalf("step = %d, want %d\n%s", r.m.analyzeStep, want, r.view())
	}
}

// on the navigate-mode capture step "?" reaches the §M overlay
// (every-page contract); once a path is being typed, every
// colliding key reaches the draft.
func TestAnalyzeHelpOpensOnFreshCaptureStep(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.gotoAnalyze()
	r.mustStep(t, pages.StepCapture)

	_, _ = r.m.Update(ch('?'))
	if r.m.help == nil {
		t.Fatalf("'?' on the navigate capture step must open help:\n%s", r.view())
	}
	_, _ = r.m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // close it
	r.m.help = nil

	r.pump(ch('/')) // the first path byte enters edit mode
	for _, c := range "q4:?" {
		r.pump(ch(c))
	}
	if r.m.help != nil {
		t.Fatal("? while typing a path must type, not open help")
	}
	if d, editing := r.m.analyze.Draft(); !editing || !strings.HasPrefix(d, "/q4:?") {
		t.Fatalf("draft = %q/%v, want /q4:?...", d, editing)
	}
}

func TestAnalyzeWalksFlowAndEnumerates(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)

	if f.enumN != 1 {
		t.Fatalf("enumeration ran %d times, want 1", f.enumN)
	}
	// no header prefill: the unchosen header rides the leg as ""
	if f.enumPaths[0] != r.pcap || f.enumHeaders[0] != "" {
		t.Fatalf("enumeration args = %q/%q, want the capture and the unset header (engine default)",
			f.enumPaths[0], f.enumHeaders[0])
	}
	// Enumeration seeds the run set with the request (dst)
	// directions; responses (src) wait for an explicit pick (Option A).
	if got := fmt.Sprint(r.m.analyzeSelected); got != "[{8080 dst} {9999 dst}]" {
		t.Fatalf("selection = %s, want the seeded [{8080 dst} {9999 dst}]", got)
	}
	v := r.view()
	for _, want := range []string{"412 msgs parsed, 3 unparsable", "0200(180) 0800(25)", "8080"} {
		if !strings.Contains(v, want) {
			t.Errorf("run step lacks %q:\n%s", want, v)
		}
	}
}

func TestAnalyzeRunStartsWithAllFlows(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)
	r.enter() // empty filter = every enumerated dst flow

	if f.runN != 1 {
		t.Fatalf("engine ran %d times, want 1", f.runN)
	}
	opts := f.runOpts[0]
	if len(opts.Flows) != 2 || opts.Flows[0].Port != 8080 || opts.Flows[0].Dir != "dst" ||
		opts.Flows[1].Port != 9999 || opts.Flows[1].Dir != "dst" {
		t.Fatalf("run flows = %v, want both dst flows", opts.Flows)
	}
	if r.m.analyzeStatus != pages.AnalyzeStatusDone {
		t.Fatalf("status = %q, want done", r.m.analyzeStatus)
	}
}

func TestAnalyzeFlowFilterMapsToPorts(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)

	r.pump(ch('/'))
	r.typeText("9999")
	r.enter()
	if f.runN != 1 {
		t.Fatalf("engine ran %d times, want 1", f.runN)
	}
	if flows := f.runOpts[0].Flows; len(flows) != 1 || flows[0].Port != 9999 || flows[0].Dir != "dst" {
		t.Fatalf("filtered run flows = %v, want [9999/dst]", flows)
	}
	if r.m.analyzeFlowFilter != "9999" {
		t.Fatalf("committed filter = %q, want 9999", r.m.analyzeFlowFilter)
	}
}

func TestAnalyzeFlowFilterMatchNoneIsNote(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)

	r.pump(ch('/'))
	r.typeText("zzz")
	r.enter()
	if f.runN != 0 {
		t.Fatalf("a filter matching none must not run the engine (%d)", f.runN)
	}
	if !strings.Contains(r.view(), "no flows match") {
		t.Fatalf("match-none note missing:\n%s", r.view())
	}
}

func TestAnalyzeRunErrorIsStepNote(t *testing.T) {
	f := fakeAnalyzeFixture()
	f.runErr = fmt.Errorf("no request/response pairs correlated")
	r := newAnalyzeTestRoot(t, f)
	r.walkToRun(t)
	r.enter()

	if r.m.analyzeStatus != pages.AnalyzeStatusError || r.m.analyzeNote == "" {
		t.Fatalf("status/note = %q/%q, want error/text", r.m.analyzeStatus, r.m.analyzeNote)
	}
	if !strings.Contains(r.view(), "correlated") {
		t.Fatalf("note not rendered:\n%s", r.view())
	}
}

func TestAnalyzeAbortOverInflightConfirms(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	r.enter() // spec "" → header
	r.mustStep(t, pages.StepHeader)

	cmd := r.enterAsync() // header → run arms the enumeration leg
	if cmd == nil || !r.m.analyzeEnumWait {
		t.Fatalf("enumeration leg not in flight: cmd=%v wait=%v", cmd != nil, r.m.analyzeEnumWait)
	}
	r.pump(pages.AnalyzeAbortMsg{})
	if r.m.analyzeConfirm == nil {
		t.Fatalf("abort over an in-flight leg must ask confirm:\n%s", r.view())
	}
	if !strings.Contains(r.view(), "abort analyze run?") {
		t.Fatalf("confirm overlay missing:\n%s", r.view())
	}

	r.pump(ch('n')) // keep running
	if r.m.analyzeConfirm != nil {
		t.Fatal("n must dismiss the confirm")
	}
	r.pump(cmd()) // the leg lands normally
	r.mustStep(t, pages.StepRun)
	if f.enumN != 1 || len(r.m.analyzeFlows) == 0 {
		t.Fatalf("leg lost: enumN=%d flows=%d", f.enumN, len(r.m.analyzeFlows))
	}
}

func TestAnalyzeAbortCancelsInflight(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.m.Push(r.m.analyze) // depth 2 so a confirmed abort pops
	r.pump(tea.WindowSizeMsg{Width: 121, Height: 41})
	r.commitCapture(r.pcap)
	r.enter() // spec "" → header
	r.mustStep(t, pages.StepHeader)

	cmd := r.enterAsync() // enumeration in flight
	r.pump(pages.AnalyzeAbortMsg{})
	r.pump(ch('y'))

	if r.m.StackDepth() != 1 {
		t.Fatalf("confirmed abort must leave the wizard, depth = %d", r.m.StackDepth())
	}
	if cmd == nil {
		t.Fatal("enumeration cmd missing")
	}
	r.pump(cmd()) // stale result must be dropped
	if len(r.m.analyzeFlows) != 0 || r.m.analyzeStep != pages.StepCapture {
		t.Fatalf("stale enumeration folded: flows=%d step=%d", len(r.m.analyzeFlows), r.m.analyzeStep)
	}
}

func TestAnalyzeLeavePageCancelsInflight(t *testing.T) {
	f := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, f)
	r.walkToRun(t)
	cmd := r.enterAsync() // the run leg, held in flight
	if cmd == nil || r.m.analyzeStep != pages.StepRun || !r.m.analyzeRunWait {
		t.Fatalf("run leg not in flight: cmd=%v step=%d wait=%v", cmd != nil, r.m.analyzeStep, r.m.analyzeRunWait)
	}
	r.pump(ch('1')) // jump to Status: the leave bumps the seq
	r.pump(cmd())
	if r.m.analyzeOutput != nil || r.m.analyzePreview != "" {
		t.Fatalf("stale run folded after leave: out=%v preview=%q", r.m.analyzeOutput, r.m.analyzePreview)
	}
	if r.m.analyzeRunWait {
		t.Fatal("leave must clear the wait flag")
	}
	if r.m.Current().ID() != PageIDs[0] {
		t.Fatalf("current = %q, want status", r.m.Current().ID())
	}
}

func TestAnalyzeHotkeyLandsOnCaptureStep(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.walkToRun(t)
	r.pump(ch('1')) // leave mid-wizard
	r.pump(ch('7')) // hotkey 7 again
	if r.m.analyzeStep != pages.StepCapture {
		t.Fatalf("re-entry step = %d, want the capture step", r.m.analyzeStep)
	}
}

func TestAnalyzeMissingCaptureStaysInline(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.gotoAnalyze()
	r.commitCapture("/tmp/gone-xyz.pcap")

	r.mustStep(t, pages.StepCapture)
	if !strings.Contains(r.view(), "no such file") {
		t.Fatalf("inline capture error missing:\n%s", r.view())
	}
}

// TestAnalyzeItemPickerCouplesDatasetWrite pins end to end:
// deselecting a transaction in the picker also deselects the dataset it draws
// from, and backing out with Esc APPLIES the selection, so the write carries
// only the included set (the unrelated mock route survives).
func TestAnalyzeItemPickerCouplesDatasetWrite(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.walkToRun(t)
	r.enter() // run → items attach, the picker auto-opens (cursor at row 0)

	r.pump(ch(' '))                              // deselect the transaction (row 0); its dataset follows
	r.pump(tea.KeyPressMsg{Code: tea.KeyEscape}) // esc backs out — and applies

	for _, want := range []string{"transaction|Captured Flow 0200_0", "dataset|dataset_0200_"} {
		if !slices.Contains(r.m.analyzeExcluded, want) {
			t.Errorf("deselecting the transaction must exclude %q too: %v", want, r.m.analyzeExcluded)
		}
	}
	if slices.Contains(r.m.analyzeExcluded, "mock_route|route-0200-00") {
		t.Errorf("the unrelated route must stay included: %v", r.m.analyzeExcluded)
	}
	if sel := r.m.analyzeOutput.SelectedItems(); len(sel) != 1 || sel[0].Name != "route-0200-00" {
		t.Errorf("SelectedItems = %+v, want only the route", sel)
	}
}

func TestAnalyzeMissingSpecIsInlineFieldError(t *testing.T) {
	f := fakeAnalyzeFixture()
	f.statErrs = map[string]error{"./nope.json": fmt.Errorf("spec file not found: ./nope.json")}
	r := newAnalyzeTestRoot(t, f)
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	r.mustStep(t, pages.StepSpec)
	r.typeText("./nope.json")
	r.enter() // commit → stat leg fails

	r.mustStep(t, pages.StepSpec)
	if !strings.Contains(r.view(), "spec file not found") {
		t.Fatalf("inline spec error missing:\n%s", r.view())
	}

	r.typeText("./ok.json")
	r.enter()
	r.mustStep(t, pages.StepHeader)
}

func TestAnalyzeGoalAndMaskReachEngine(t *testing.T) {
	f := fakeAnalyzeFixture()
	r := newAnalyzeTestRoot(t, f)
	r.walkToRun(t)
	r.pump(ch('s')) // scenario goal
	r.pump(ch('m')) // security toggle → raw

	r.enter()
	if f.runN != 1 {
		t.Fatalf("engine ran %d times, want 1", f.runN)
	}
	if f.runOpts[0].Mode != app.AnalyzeModeScenario || !f.runOpts[0].Unsecure {
		t.Fatalf("run opts = %+v, want scenario + unsecure", f.runOpts[0])
	}
}

func TestAnalyzeNoLegReportsMissingEngine(t *testing.T) {
	r := newAnalyzeTestRoot(t, nil)
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	r.enter() // spec "" → header
	r.enter() // header → run: no engine to enumerate

	if r.m.analyzeSrc != nil {
		t.Fatal("fake leaked")
	}
	if !strings.Contains(r.view(), "no analyze engine available") {
		t.Fatalf("missing-leg error not shown:\n%s", r.view())
	}
	r.mustStep(t, pages.StepRun)
}

// --- regression tests -------------------------------------

// TestAnalyzeHeaderListMatchesEngineSet: the header list must be
// exactly the set utils.SelectLength accepts (the old
// hardcoded list offered "bit31"/"llvm", which the engine rejects),
// with the current/effective framing first (the wizard radio idiom).
func TestAnalyzeHeaderListMatchesEngineSet(t *testing.T) {
	m := NewRootModel(nil)
	m.analyzeHeader = "bcd2"
	items := m.analyzeHeaderList()

	want := utils.LengthTypeOptions()
	if len(items) != len(want) {
		t.Fatalf("header list = %d items, want the engine set %d", len(items), len(want))
	}
	if items[0].Header != "bcd2" || !items[0].Selected {
		t.Fatalf("current framing must lead the list: %+v", items[0])
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.Header] = true
		if _, err := utils.SelectLength(it.Header); err != nil {
			t.Errorf("engine rejects offered header %q: %v", it.Header, err)
		}
	}
	for _, h := range want {
		if !seen[h] {
			t.Fatalf("header list lacks %q (%v)", h, items)
		}
	}
	for _, bogus := range []string{"bit31", "llvm"} {
		if _, err := utils.SelectLength(bogus); err == nil {
			t.Errorf("SelectLength(%q) must fail (it must never be offered)", bogus)
		}
	}
}

// TestAnalyzeEnumErrorNamesPath: an enumeration failure whose
// ConfigError names the SPEC path must surface as the run-step note
// (the typed error text, never a crash).
func TestAnalyzeEnumErrorNamesPath(t *testing.T) {
	f := fakeAnalyzeFixture()
	f.enumErr = &app.ConfigError{Path: "./spec.json", Err: fmt.Errorf("spec file unreadable")}
	r := newAnalyzeTestRoot(t, f)
	r.walkToRun(t)

	if r.m.analyzeStatus != pages.AnalyzeStatusError {
		t.Fatalf("status = %q, want error", r.m.analyzeStatus)
	}
	if !strings.Contains(r.view(), "spec file unreadable") {
		t.Fatalf("enum error not rendered at the run step:\n%s", r.view())
	}
	if len(r.m.analyzeFlows) != 0 {
		t.Fatalf("failed enumeration fabricated flows: %v", r.m.analyzeFlows)
	}
}

// TestAnalyzeDraftRendersWhileEditing: typed text at the spec step
// must be visible BEFORE Enter commits (the old view rendered only the
// last-committed path with a caret, keeping the draft invisible).
func TestAnalyzeDraftRendersWhileEditing(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.gotoAnalyze()
	r.commitCapture(r.pcap)
	r.mustStep(t, pages.StepSpec)
	r.typeText("my-spec.json")

	if r.m.analyzeSpecPath != "" {
		t.Fatal("typing must not commit the spec path")
	}
	if !strings.Contains(r.view(), "my-spec.json") {
		t.Fatalf("draft text not rendered while editing:\n%s", r.view())
	}
}

// TestAnalyzeFlowSpaceToggle: space toggles the flow DIRECTION under the run
// step's flow cursor INDEPENDENTLY (selecting a src response
// half no longer mirrors its dst half); a runs all/none; the engine receives
// the included ∩ visible set, and an empty inclusion is a note, never a
// fabricated run.
func TestAnalyzeFlowSpaceToggle(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	f := r.fakeSrc(t)
	r.walkToRun(t)

	// Default (Option A): requests (dst) selected, responses (src) not.
	if got := fmt.Sprint(r.m.analyzeSelected); got != "[{8080 dst} {9999 dst}]" {
		t.Fatalf("default selection = %s, want [{8080 dst} {9999 dst}]", got)
	}

	// Cursor leads at dst :8080; space excludes it.
	r.pump(ch(' '))
	if got := fmt.Sprint(r.m.analyzeSelected); got != "[{9999 dst}]" {
		t.Fatalf("after space selection = %s, want [{9999 dst}]", got)
	}
	r.enter()
	r.closePicker() // the run re-presented the item picker
	if f.runN != 1 {
		t.Fatalf("engine ran %d times, want 1", f.runN)
	}
	if flows := f.runOpts[0].Flows; len(flows) != 1 || flows[0].Port != 9999 || flows[0].Dir != "dst" {
		t.Fatalf("run flows = %v, want the included [{9999 dst}]", flows)
	}

	// J onto the src:8080 response half; space selects it
	// ALONE (its dst half stays excluded) — directions are independent.
	r.pump(ch('j')) // -> src :8080
	r.pump(ch(' '))
	if !r.m.selectedFlow(8080, "src") || r.m.selectedFlow(8080, "dst") {
		t.Fatalf("src must toggle independently of dst: %v", r.m.analyzeSelected)
	}
	// space again clears the src half, leaving only dst :9999.
	r.pump(ch(' '))
	if r.m.selectedFlow(8080, "src") {
		t.Fatalf("space again must clear src :8080: %v", r.m.analyzeSelected)
	}

	// j -> dst :9999; space excludes it too, leaving nothing selected.
	r.pump(ch('j')) // -> dst :9999
	r.pump(ch(' '))
	if len(r.m.analyzeSelected) != 0 {
		t.Fatalf("selection = %v, want empty", r.m.analyzeSelected)
	}
	before := f.runN
	r.enter()
	r.closePicker() // the run attached again -> picker re-presented
	if f.runN != before {
		t.Fatal("Enter with no flows included must not run the engine")
	}
	if !strings.Contains(r.m.analyzeNote, "no flows selected") {
		t.Fatalf("note = %q, want the no-flows-selected hint", r.m.analyzeNote)
	}

	// a includes every direction (dst AND src of both ports); Enter runs all.
	r.pump(ch('a'))
	if len(r.m.analyzeSelected) != 4 {
		t.Fatalf("a = %v, want all four directions", r.m.analyzeSelected)
	}
	r.enter()
	r.closePicker() // the all-directions run attached -> picker re-presented
	if f.runN != before+1 {
		t.Fatalf("engine ran %d times, want %d", f.runN, before+1)
	}
	if len(f.runOpts[f.runN-1].Flows) != 4 {
		t.Fatalf("run flows = %v, want all four directions", f.runOpts[f.runN-1].Flows)
	}

	// a again clears all; the view shows the ○ exclusion glyph.
	r.pump(ch('a'))
	if len(r.m.analyzeSelected) != 0 {
		t.Fatalf("a again = %v, want cleared", r.m.analyzeSelected)
	}
	if v := r.view(); !strings.Contains(v, "[ ]") {
		t.Errorf("view must show the ascii exclusion box:\n%s", v)
	}
}

// TestAnalyzeScenarioTogglesPortUnit pins the scenario unit: in
// scenario mode a port is correlated as a whole, so space on either direction
// toggles BOTH halves together (unlike the per-direction transactions goal).
func TestAnalyzeScenarioTogglesPortUnit(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.walkToRun(t)
	r.pump(ch('s')) // scenario goal — the selection stays as seeded (dst only)

	if !r.m.selectedFlow(8080, "dst") || r.m.selectedFlow(8080, "src") {
		t.Fatalf("scenario default = %v, want dst seeded only", r.m.analyzeSelected)
	}

	// Cursor leads at dst :8080; scenario space drops the WHOLE port.
	r.pump(ch(' '))
	if r.m.selectedPort(8080) {
		t.Fatalf("scenario space must drop the whole 8080 port: %v", r.m.analyzeSelected)
	}
	// Space again re-includes both halves of the port together.
	r.pump(ch(' '))
	if !r.m.selectedFlow(8080, "dst") || !r.m.selectedFlow(8080, "src") {
		t.Fatalf("scenario space must re-include both halves of 8080: %v", r.m.analyzeSelected)
	}
}
