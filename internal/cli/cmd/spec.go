package cmd

import (
	"github.com/spf13/cobra"

	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
)

func newSpecCmd() *cobra.Command {
	specCmd := &cobra.Command{
		Use:   "spec",
		Short: "Manage ISO8583 message specifications",
	}

	specCmd.AddCommand(newSpecInitCmd())
	return specCmd
}

func newSpecInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [output-path]",
		Short: "Generate a default ISO8583 specification file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var path string
			if len(args) > 0 {
				path = args[0]
			}

			out := output.New(cmd)
			if out.DryRun() {
				if path == "" {
					path = cmdpkg.DefaultSpecInitPath
				}

				return previewFileWrite(out, "default specification file", path)
			}

			initCmd := &cmdpkg.InitSpecCommand{OutputPath: path}

			return initCmd.Execute()
		},
	}
}
