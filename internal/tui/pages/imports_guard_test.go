package pages

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPagesForbiddenImports enforces the SCR-501 data-flow fence: pages
// consume only theme/widgets/frame/palette/events plus bubbletea/lipgloss
// and stdlib. They must never import internal/app (root owns the App),
// internal/cli, internal/command, cobra, or internal/tui itself (the parent
// imports pages; a back-edge would be a cycle). os.Exit is scanned as a
// substring because the rule is about Update purity, not imports.
func TestPagesForbiddenImports(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"jiso/internal/app",
		"jiso/internal/cli",
		"jiso/internal/command",
		"jiso/internal/tui",
		"github.com/spf13/cobra",
	}
	forbiddenRefs := []string{"os.Exit", "time.Now"}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	scanned := 0

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++

		path := filepath.Join(".", name)

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, imp := range f.Imports {
			imported, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s in %s: %v", imp.Path.Value, path, err)
			}

			for _, bad := range forbidden {
				// "jiso/internal/app" forbids the package itself but
				// allows the events taxonomy sub-package;
				// "jiso/internal/tui" forbids the parent package and
				// every leaf except the allow-list (theme/widgets/frame/
				// palette/progress/bridge are page-building blocks).
				if imported == bad {
					t.Errorf("%s imports forbidden package %q", path, imported)
				}
				if strings.HasPrefix(imported, bad+"/") && !allowedSub(imported) {
					t.Errorf("%s imports forbidden subpackage %q", path, imported)
				}
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		for _, line := range strings.Split(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}

			for _, ref := range forbiddenRefs {
				if strings.Contains(line, ref) {
					t.Errorf("%s contains forbidden reference %q", path, ref)
				}
			}
		}
	}

	if scanned == 0 {
		t.Fatal("guard scanned no source files; did the package move?")
	}
}

// allowedSub is the import allow-list for subpackages of the forbidden
// prefixes: the events taxonomy plus the leaf TUI building blocks pages are
// assembled from (fence from the SCR-501 ticket).
func allowedSub(imported string) bool {
	switch imported {
	case "jiso/internal/app/events",
		"jiso/internal/tui/theme",
		"jiso/internal/tui/widgets",
		"jiso/internal/tui/frame",
		"jiso/internal/tui/palette",
		"jiso/internal/tui/progress":
		return true
	}

	return false
}
