package repohealth

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Line budget (the file-hygiene decision: prod <= 500, test <= 600 lines).
//
// A file this size is a review-unit problem, not a style preference: root.go at
// 1,524 lines holds a ~200-field struct, a 426-line update method and a 208-line
// keymap, so no reviewer (human or agent) can hold it at once, and every feature
// reaches into the same struct.
const (
	prodLineBudget = 500
	testLineBudget = 600
)

// overBudget is the ratchet: files already over budget, each capped at the size
// it had when it was listed. It fails when a listed file grows past its ceiling,
// when an unlisted file goes over budget, when a listed file drops back inside
// the budget (the entry must then be deleted, so the list only ever shrinks),
// and when a listed file disappears.
//
// Splitting a file means deleting its entry in the same commit — that is the
// point of the ratchet.
var overBudget = map[string]int{
	"internal/cli/goldentest/golden_test.go":        1070,
	"internal/transactions/scenario_runner_test.go": 772,
	// root.go's 487-line RootModel struct moved to root_model.go (UAT
	// round 5 split); root.go itself is back inside the budget.
	"internal/tui/root_analyze_test.go": 792,
}

// TestSourceFileLineBudget walks internal/ and cmd/ and applies the budget to
// every Go file. It reports all offenders, so one run shows the whole picture.
func TestSourceFileLineBudget(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	remaining := maps.Clone(overBudget)

	for _, dir := range []string{"internal", "cmd"} {
		err := walkSourceFiles(filepath.Join(root, dir), func(path string) error {
			checkFileLineBudget(t, root, path, remaining)

			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	stale := make([]string, 0, len(remaining))
	for path := range remaining {
		stale = append(stale, path)
	}
	slices.Sort(stale)

	for _, path := range stale {
		t.Errorf("ratchet lists %s, which is gone — drop the entry", path)
	}
}

// checkFileLineBudget applies the budget for the file's kind and consumes its
// ratchet entry, so whatever is left in remaining afterwards is stale.
func checkFileLineBudget(t *testing.T, root, path string, remaining map[string]int) {
	t.Helper()

	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("rel %s: %v", path, err)
	}

	lines := lineCount(t, path)

	budget := prodLineBudget
	if strings.HasSuffix(rel, "_test.go") {
		budget = testLineBudget
	}

	ceiling, listed := remaining[rel]
	delete(remaining, rel)

	switch {
	case lines <= budget:
		if listed {
			t.Errorf("%s is %d lines, back inside the %d-line budget: delete its ratchet entry (was capped at %d) so the list only shrinks",
				rel, lines, budget, ceiling)
		}
	case listed && lines <= ceiling:
		// Listed and not grown: tolerated, and tracked.
	case listed:
		t.Errorf("%s grew from %d to %d lines; the ratchet only shrinks — split it (budget is %d)",
			rel, ceiling, lines, budget)
	default:
		t.Errorf("%s is %d lines, over the %d-line budget — split it along its existing seams instead of listing it",
			rel, lines, budget)
	}
}

// walkSourceFiles visits every .go file under dir, skipping hidden directories
// and testdata (fixtures are data, not source).
func walkSourceFiles(dir string, visit func(path string) error) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path == dir {
				return nil
			}

			if name := d.Name(); strings.HasPrefix(name, ".") || name == "testdata" {
				return fs.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		return visit(path)
	})
}

// lineCount counts the lines in a file, counting a final line that has no
// trailing newline as a line.
func lineCount(t *testing.T, path string) int {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	lines := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}

	return lines
}

// repoRoot resolves the module root from the test's working directory, and
// refuses to run a tree-wide guard on a tree it cannot prove is the repo.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working dir: %v", err)
	}

	root, err := filepath.Abs(filepath.Join(dir, "..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s is not the module root (no go.mod): %v", root, err)
	}

	return root
}
