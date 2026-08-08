package cmd

import (
	cmdpkg "jiso/internal/command"

	"github.com/spf13/cobra"
)

func newTxCmd() *cobra.Command {
	txCmd := &cobra.Command{
		Use:   "tx",
		Short: "Manage ISO8583 transaction payloads",
	}

	txCmd.AddCommand(newTxInitCmd())
	return txCmd
}

func newTxInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [output-path]",
		Short: "Generate a comprehensive sample transaction configuration file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var path string
			if len(args) > 0 {
				path = args[0]
			}
			initCmd := &cmdpkg.InitTxCommand{OutputPath: path}
			return initCmd.Execute()
		},
	}
}
