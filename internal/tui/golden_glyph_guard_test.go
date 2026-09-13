package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestASCIIGoldensAreSevenBit pins the ASCII glyph profile's contract: under
// JISO_ASCII=1 (and on a terminal that cannot do Unicode) every rendered byte
// is 7-bit. Unicode glyphs are invisible where they are written — a hardcoded
// "·" or "…" looks right in a truecolor golden and only betrays itself as a
// non-ASCII byte in the ASCII golden, which no test used to look at.
//
// It walks the whole internal/tui tree, so every page's testdata is covered,
// not just this package's. A new leak means the glyph came from a literal
// instead of theme.Ellipsis / theme.Separator / theme.Truncate.
func TestASCIIGoldensAreSevenBit(t *testing.T) {
	t.Parallel()

	var offenders []string

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".golden") ||
			!strings.Contains(d.Name(), "ascii") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		bad := nonASCII(data)
		if len(bad) > 0 {
			offenders = append(offenders, fmt.Sprintf("%s: %s", path, bad))
		}

		// The ASCII profile is the colourless, plain-glyph mode, so its frames
		// carry no SGR bytes either: a golden with escapes is a theme that was
		// built with the wrong profile, not a rendering bug.
		if i := strings.IndexAny(string(data), "\x1b\u009b"); i >= 0 {
			offenders = append(offenders, fmt.Sprintf("%s: escape code at byte %d", path, i+1))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk goldens: %v", err)
	}

	sort.Strings(offenders)
	for _, o := range offenders {
		t.Errorf("ASCII golden carries a non-ASCII byte — %s\n"+
			"take the glyph from the theme instead of writing the literal (Theme.Ellipsis, Theme.Separator, Theme.Truncate)", o)
	}
}

// nonASCII lists the offending code points with their counts, so the failure
// names the glyph rather than just "the file is not 7-bit".
func nonASCII(data []byte) string {
	counts := map[rune]int{}

	for _, r := range string(data) {
		if r > 0x7f {
			counts[r]++
		}
	}

	if len(counts) == 0 {
		return ""
	}

	glyphs := make([]string, 0, len(counts))
	for r := range counts {
		glyphs = append(glyphs, string(r))
	}
	sort.Strings(glyphs)

	parts := make([]string, 0, len(glyphs))
	for _, g := range glyphs {
		r := []rune(g)[0]
		parts = append(parts, fmt.Sprintf("%q U+%04X x%d", g, r, counts[r]))
	}

	return strings.Join(parts, ", ")
}
