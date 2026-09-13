package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGoModDependencyHygiene pins the REL-604 dependency contract: the
// readline dependency died with the legacy REPL loop and must not come
// back, while survey (interactive selectors) and shellquote (command
// palette tokenization) stay as declared v2 dependencies.
func TestGoModDependencyHygiene(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "go.mod"))
	require.NoError(t, err, "module root go.mod must be readable from the cmd package")

	mod := string(raw)
	assert.NotContains(t, mod, "github.com/chzyer/readline",
		"readline must stay out of go.mod: the REPL loop was removed at v2.0.0")

	for _, kept := range []string{"github.com/AlecAivazis/survey/v2", "github.com/kballard/go-shellquote"} {
		assert.True(t, strings.Contains(mod, kept), "%s must stay a declared dependency", kept)
	}
}
