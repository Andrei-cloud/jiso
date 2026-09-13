package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// replRemovedNotice is the REL-604 removal notice for the legacy REPL: the
// readline loop is gone in v2.0.0. It goes to stderr (CLI-102: stdout is
// machine output and --json stdout must stay pure), is suppressed by
// -q/--quiet, and the command exits 2 (command unavailable in the E1 exit
// taxonomy).
const replRemovedNotice = "REPL was removed in v2.0.0 — use 'jiso tui' or the v2 command tree"

func newREPLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repl",
		Short: "Removed at v2.0.0 — use 'jiso tui' or the v2 command tree",
		Annotations: map[string]string{
			skipConfigValidationAnnotation: annotationSet,
			skipSessionDBInitAnnotation:    annotationSet,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			printREPLRemovedNotice(cmd)

			return &ExitCodeError{Code: ExitUsage}
		},
	}
}

// printREPLRemovedNotice writes the one-line removal notice to stderr,
// unless --quiet is resolved on (the flag value is already
// flag > $JISO_QUIET > user config by PersistentPreRunE).
func printREPLRemovedNotice(cmd *cobra.Command) {
	if quiet, err := cmd.Flags().GetBool("quiet"); err == nil && quiet {
		return
	}

	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), replRemovedNotice)
}
