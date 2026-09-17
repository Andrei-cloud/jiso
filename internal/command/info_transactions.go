package command

import (
	"fmt"
	"sort"
	"strings"
)

// FormatFieldsForInfo renders the declared fields map the way REPL info has
// always rendered its "Message:" section: one field per line, keys sorted
// numerically, strings quoted. Shared with `jiso inspect` so both
// paths present the same composition.
func FormatFieldsForInfo(fields map[string]any) string {
	// Get keys and sort them numerically
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}

	// Custom sort for field numbers
	sort.Slice(keys, func(i, j int) bool {
		// Convert to integers for numeric comparison, but treat errors as string comparison
		numI, errI := parseFieldNumber(keys[i])
		numJ, errJ := parseFieldNumber(keys[j])

		if errI == nil && errJ == nil {
			return numI < numJ
		}

		return keys[i] < keys[j]
	})

	// Build the formatted string
	var sb strings.Builder
	for _, k := range keys {
		value := fields[k]
		fmt.Fprintf(&sb, "%q: %v\n", k, formatValue(value))
	}

	return sb.String()
}

// parseFieldNumber attempts to convert a field key to an integer.
func parseFieldNumber(key string) (int, error) {
	var num int
	_, err := fmt.Sscanf(key, "%d", &num)

	return num, err
}

// formatValue formats a value properly for display.
func formatValue(value any) string {
	switch v := value.(type) {
	case string:
		return fmt.Sprintf("%q", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
