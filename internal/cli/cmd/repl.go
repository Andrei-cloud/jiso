package cmd

import (
	"github.com/spf13/cobra"
)

func newREPLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repl",
		Short: "Start interactive REPL terminal prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			if defaultREPLRunner != nil {
				return defaultREPLRunner(cmd.Context())
			}
			return nil
		},
	}
}
