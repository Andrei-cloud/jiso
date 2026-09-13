package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docs_tui_parity_test.go pins the key tables in docs/tui.md to the §M
// help registry.
//
// Direction chosen: docs -> registry. Every key token documented in a
// markdown table with a "Keys" column must appear in the registry-dump
// golden (testdata/help/registry_dump.golden). The ticket's invariant is
// "a doc key that isn't bound is a bug", and this direction checks
// exactly that. The reverse coverage direction (every dump binding must
// appear in the docs) was rejected: single-character bindings (k, q, s, t)
// collide as raw substrings with ordinary prose, so a meaningful reverse
// check needs the same tokenisation machinery and would add no new
// invariant beyond what the doc review already provides.
//
// Trusting the golden is safe: TestHelpRegistryDump regenerates/compares
// it against the LIVE registry (helpRegistryDump(m.registry, &m.keys)),
// so a stale dump fails that test first.
//
// Scope note: tables with a "Keys" header document router-level or page
// bindings — the registry's own content. Modal widgets (command palette,
// file picker, §N3 confirms, §N2 forms) own the keyboard wholesale while
// open and are deliberately NOT in the registry; docs/tui.md documents
// their keys in tables with a "Chord" header, pinned instead by the widget
// tests (palette, widgets).

// dumpKeyLine matches one registry-dump entry: "[group] keys\tnote".
var dumpKeyLine = regexp.MustCompile(`^\[[^\]]+\] (.+?)\t`)

// backtickToken matches a `code` span inside a docs table cell.
var backtickToken = regexp.MustCompile("`([^`]+)`")

// registryDumpKeySet collects every bindable token from the dump: each
// full keys string ("up/k", ":/ctrl+p", "1-8") plus its "/"- and
// " or "-separated components (":", "ctrl+p", "up", "k").
func registryDumpKeySet(t *testing.T, dump string) map[string]bool {
	t.Helper()

	set := map[string]bool{}
	add := func(k string) {
		if k = strings.TrimSpace(k); k != "" {
			set[k] = true
		}
	}
	for _, line := range strings.Split(dump, "\n") {
		m := dumpKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, alt := range strings.Split(m[1], " or ") {
			add(alt)
			for _, part := range strings.Split(alt, "/") {
				add(part)
			}
		}
	}

	return set
}

// docsKeyTokens parses docs/tui.md markdown tables and returns every
// backtick token found in a "Keys" column, as "line:token".
func docsKeyTokens(t *testing.T, body string) []string {
	t.Helper()

	isRow := func(line string) bool { return strings.HasPrefix(line, "|") }
	isSeparator := func(line string) bool {
		for _, r := range strings.ReplaceAll(line, "|", "") {
			if r != '-' && r != ':' && r != ' ' {
				return false
			}
		}

		return strings.Contains(line, "-")
	}
	cells := func(line string) []string {
		parts := strings.Split(strings.Trim(line, "|"), "|")
		for i, c := range parts {
			parts[i] = strings.TrimSpace(c)
		}

		return parts
	}

	lines := strings.Split(body, "\n")
	var tokens []string
	for i := 0; i < len(lines); i++ {
		if !isRow(lines[i]) {
			continue
		}
		head := cells(lines[i])
		keysCol := -1
		for j := range head {
			if strings.EqualFold(strings.ReplaceAll(head[j], "`", ""), "keys") {
				keysCol = j

				break
			}
		}
		if keysCol < 0 || i+1 >= len(lines) || !isSeparator(lines[i+1]) {
			// Not a key table: skip the whole block.
			for i < len(lines) && isRow(lines[i]) {
				i++
			}

			continue
		}
		i++ // consume the separator row
		for i < len(lines) && isRow(lines[i]) {
			row := cells(lines[i])
			if keysCol < len(row) {
				for _, m := range backtickToken.FindAllStringSubmatch(row[keysCol], -1) {
					tokens = append(tokens, itoaLine(i+1)+":"+m[1])
				}
			}
			i++
		}
		i-- // the outer loop re-increments
	}

	return tokens
}

// itoaLine formats a 1-based line number for failure messages.
func itoaLine(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	return string(buf[i:])
}

func TestDocsTuiKeyTablesAreBoundInTheHelpRegistry(t *testing.T) {
	t.Parallel()

	dumpPath := filepath.Join("testdata", "help", "registry_dump.golden")
	dump, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read registry dump: %v", err)
	}
	docsPath := filepath.Join("..", "..", "docs", "tui.md")
	body, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read docs/tui.md: %v", err)
	}

	keyset := registryDumpKeySet(t, string(dump))
	if len(keyset) == 0 {
		t.Fatalf("registry dump %s yielded no key tokens", dumpPath)
	}

	tokens := docsKeyTokens(t, string(body))
	// Guard against a silent parser break: the guide documents ~100 key
	// tokens; a parse that finds far fewer means the table format drifted
	// and the check would pass vacuously.
	if len(tokens) < 40 {
		t.Fatalf("parsed only %d key tokens from docs/tui.md — table parser broken?", len(tokens))
	}

	var unbound []string
	for _, tok := range tokens {
		key := tok[strings.Index(tok, ":")+1:]
		if !keyset[key] {
			unbound = append(unbound, tok)
		}
	}
	if len(unbound) > 0 {
		t.Errorf("docs/tui.md documents keys that are not bound in the help registry dump:\n  %s",
			strings.Join(unbound, "\n  "))
	}
}
