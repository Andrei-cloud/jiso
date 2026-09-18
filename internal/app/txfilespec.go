// txfilespec.go counts the entries a transaction file leaves without a
// specification — the load-time signal that deciding whether picking the
// file would silently bind work to the engine's compiled-in default spec.
package app

import (
	"encoding/json"
	"fmt"
	"os"

	"jiso/internal/config"
	"jiso/internal/transactions"
)

// CountTransactionsWithoutSpec parses path with the collection loader's own
// shapes and counts entries declaring no specification: transaction items
// (typed or type-less) with neither "spec" nor "spec_file" set, per the
// addItem semantics; datasets, scenarios and mock routes never count. A file
// that parses only as a legacy plain array counts every entry — the loader
// reads those as specless transactions.
func CountTransactionsWithoutSpec(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read file: %w", err)
	}

	var items []transactions.ConfigItem
	if err := json.Unmarshal(data, &items); err != nil {
		var legacyTx []transactions.Transaction
		if errLegacy := json.Unmarshal(data, &legacyTx); errLegacy != nil {
			return 0, fmt.Errorf("failed to unmarshal data: %w", err)
		}

		return len(legacyTx), nil
	}

	n := 0
	for _, item := range items {
		switch item.Type {
		case "", config.TypeTransaction:
			if item.Spec == "" && item.SpecFile == "" {
				n++
			}
		}
	}

	return n, nil
}
