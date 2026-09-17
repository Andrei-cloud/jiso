// save.go is the §L settings persistence leg: Save merges
// ONLY the changed keys into the user config file, in the same YAML
// format Load reads. The existing file is first decoded into a generic
// mapping so untouched keys (including ones this File struct does not
// model) survive the rewrite; changed keys are typed per their schema
// (reconnect_attempts int, hex/json bool, durations and paths strings)
// so a round-trip through Load yields the same pointers. Keys are
// written sorted for deterministic bytes; comments in the previous
// file are not preserved (accepted trade-off of the minimal merge —
// the file is machine-owned once the TUI writes it).
package userconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// boolKeys are the schema keys decoded as YAML bools; intKeys as ints.
// Everything else in the §L key space persists as a string.
var (
	boolKeys = map[string]bool{"json": true, "quiet": true, "debug": true, "unsecure": true, "hex": true}
	intKeys  = map[string]bool{"reconnect_attempts": true}
)

// Save merges updates (schema key -> textual value) into the config
// file at path, creating parent directories as needed. An unparsable
// int/bool value is an error naming the key and nothing is written; a
// malformed existing file is an error naming the path (same CLI-105
// class Load reports). Unknown keys pass through as strings.
func Save(path string, updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}

	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("failed to parse config file: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	for key, value := range updates {
		typed, err := typedValue(key, value)
		if err != nil {
			return err
		}
		doc[key] = typed
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal config file: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return os.WriteFile(path, out, 0o644)
}

// typedValue converts one textual update to its schema type.
func typedValue(key, value string) (any, error) {
	switch {
	case boolKeys[key]:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean for %s: %q", key, value)
		}

		return b, nil
	case intKeys[key]:
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid integer for %s: %q", key, value)
		}

		return n, nil
	default:
		return value, nil
	}
}
