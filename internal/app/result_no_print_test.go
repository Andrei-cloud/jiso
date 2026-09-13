package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInternalAppHasNoTerminalPrints enforces the package rule that all
// terminal I/O lives in frontends: no non-test file under internal/app may
// reference fmt.Print* or write to os.Stdout/os.Stderr.
func TestInternalAppHasNoTerminalPrints(t *testing.T) {
	t.Parallel()

	forbidden := []string{"fmt.Print", "os.Stdout", "os.Stderr"}

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, pattern := range forbidden {
			if strings.Contains(string(data), pattern) {
				t.Errorf("%s contains forbidden reference %q", path, pattern)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/app failed: %v", err)
	}
}
