package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// TUIRunner defines the function signature to launch the interactive TUI.
type TUIRunner func(ctx context.Context) error

var defaultTUIRunner TUIRunner

// SetTUIRunner sets the function used to launch the interactive TUI:
// cmd tests never exec the program through this seam.
func SetTUIRunner(runner TUIRunner) { defaultTUIRunner = runner }

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:         "tui",
		Short:       "Launch the interactive Bubble Tea TUI",
		Annotations: map[string]string{skipConfigValidationAnnotation: annotationSet},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The TUI owns the alternate screen and raw mode; without a
			// terminal that session cannot exist, so fail fast and truth
			// (exit 2, like any usage error) instead of hanging on stdin.
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
					"jiso tui requires an interactive terminal (TTY); use the headless commands instead")

				return &ExitCodeError{Code: ExitUsage}
			}

			if defaultTUIRunner == nil {
				return errors.New("tui runner is not configured")
			}

			return defaultTUIRunner(cmd.Context())
		},
	}
}
