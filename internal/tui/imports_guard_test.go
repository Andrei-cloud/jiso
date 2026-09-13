package tui

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTuiForbiddenImports enforces the frontend contract: internal/tui
// consumes ONLY internal/app — never internal/cli, internal/command, or
// cobra (that is the CLI's job). Imports are parsed, not grepped, so
// comments and string literals cannot mask a violation. os.Exit is scanned
// as a substring because the rule is about Update purity, not imports.
func TestTuiForbiddenImports(t *testing.T) {
	t.Parallel()

	forbiddenImports := []string{
		"jiso/internal/cli",
		"jiso/internal/command",
		"github.com/spf13/cobra",
	}
	forbiddenRefs := []string{"os.Exit"}

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

			for _, forbidden := range forbiddenImports {
				if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
					t.Errorf("%s imports forbidden package %q", path, imported)
				}
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		for _, line := range strings.Split(string(data), "\n") {
			// Comments may document the rule; only code violates it.
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
