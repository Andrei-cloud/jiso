package command

import "jiso/internal/config"

// SaveConfigItems is the exported entry point of the analyze persistence step
// It merges items into filename with duplicate-name filtering and
// deterministic ordering, delegating to the single generated-items writer
// (config.SaveItems, extraction) so the headless analyze path, the
// interactive wizard and the TUI §J write leg all land on identical merge and
// ordering bytes. The headless CLI path calls it instead of re-implementing the
// merge, and never passes through a survey prompt.
func SaveConfigItems(filename string, items []config.Item) error {
	return config.SaveItems(filename, items)
}
