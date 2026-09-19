// root_scenario_specseat_test.go pins the F12 seating: a gated scenario
// whose item records its capture spec (or whose steps all declare the
// same one) opens the spec browse seated on that file — directory and
// list cursor — while an unstamped scenario keeps the old root start.
package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// scenarioSeatJSON: "Stamped" names its spec on the scenario item with a
// bare step (gate open, seats on the item spec); "Shared" has a bare step
// (gate open) plus a step that declares chosen, so the declaring steps
// agree and seat on it; "Plain" has only a bare step and declares nothing.
func scenarioSeatJSON(chosen string) string {
	return `[
 {"type":"transaction","name":"Bare","description":"no spec","fields":{"0":"0800"}},
 {"type":"transaction","name":"Signed","description":"declares","fields":{"0":"0800"},"spec":"` + chosen + `"},
 {"type":"scenario","name":"Stamped","description":"item records its spec","spec":"` + chosen + `","steps":[
   {"name":"Bare Step","use_transaction_id":"Bare"}]},
 {"type":"scenario","name":"Shared","description":"declaring steps agree","steps":[
   {"name":"Bare Step","use_transaction_id":"Bare"},
   {"name":"Signed Step","use_transaction_id":"Signed"}]},
 {"type":"scenario","name":"Plain","description":"nothing recorded","steps":[
   {"name":"Bare Step","use_transaction_id":"Bare"}]}
]`
}

func newScenarioSeatFix(t *testing.T) (*specGateRoot, string) {
	t.Helper()

	t.Setenv("JISO_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	chosen := filepath.Join(dir, "captured-spec.json")
	txs := filepath.Join(dir, "scenarios.json")

	flex, err := os.ReadFile(filepath.Join("..", "..", "specs", "flex.json"))
	if err != nil {
		t.Fatalf("read flex spec: %v", err)
	}
	if err := os.WriteFile(chosen, flex, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(txs, []byte(scenarioSeatJSON(chosen)), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	time.Sleep(20 * time.Millisecond)
	if errs := a.ApplySettings(context.Background(), map[string]string{app.SettingTxFile: txs}); len(errs) != 0 {
		t.Fatalf("land scenarios: %v", errs)
	}

	r := &specGateRoot{t: t, m: NewRootModel(a)}
	r.m.toastTickf = func(time.Duration, func() tea.Msg) tea.Cmd { return nil }
	r.pump(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.pump(ch('3'))

	return r, chosen
}

func TestScenarioSpecGateSeatsOnRecordedSpec(t *testing.T) {
	r, chosen := newScenarioSeatFix(t)

	// An item-stamped scenario seats the browse on its directory.
	r.pump(pages.ScenarioRunMsg{ID: "Stamped"})
	if r.m.filePick == nil {
		t.Fatal("the specless step must still gate")
	}
	if got := r.m.filePick.CurrentDir(); got != filepath.Dir(chosen) {
		t.Errorf("browse dir = %q, want the recorded spec's dir %q", got, filepath.Dir(chosen))
	}
	if !strings.Contains(ansi.Strip(r.m.View().Content), filepath.Base(chosen)) {
		t.Errorf("the recorded file must be visible/positioned in the browse:\n%s", ansi.Strip(r.m.View().Content))
	}

	// Steps that agree on one spec seat the same way.
	r.pump(special(tea.KeyEscape))
	r.pump(pages.ScenarioRunMsg{ID: "Shared"})
	if r.m.filePick == nil {
		t.Fatal("shared-step scenario must gate")
	}
	if got := r.m.filePick.CurrentDir(); got != filepath.Dir(chosen) {
		t.Errorf("shared browse dir = %q", got)
	}

	// Nothing recorded: the classic root start stands.
	r.pump(special(tea.KeyEscape))
	r.pump(pages.ScenarioRunMsg{ID: "Plain"})
	if r.m.filePick == nil {
		t.Fatal("plain scenario must gate")
	}
	if got := r.m.filePick.CurrentDir(); got != "/" {
		t.Errorf("plain browse dir = %q, want the root", got)
	}
}
