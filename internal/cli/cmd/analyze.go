package cmd

import (
	cfg "jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	cmdpkg "jiso/internal/command"

	"github.com/moov-io/iso8583"
	"github.com/spf13/cobra"
)

func newAnalyzeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "analyze [pcap-file] [flags]",
		Aliases: []string{"pcap"},
		Short:   "Analyze stream/PCAP capture files to extract transaction templates & datasets",
		RunE: func(cmd *cobra.Command, args []string) error {
			unsecure, _ := cmd.Flags().GetBool("unsecure")
			scenario, _ := cmd.Flags().GetBool("scenario")

			var cleanArgs []string
			if unsecure {
				cleanArgs = append(cleanArgs, "--unsecure")
			}
			if scenario {
				cleanArgs = append(cleanArgs, "--scenario")
			}
			cleanArgs = append(cleanArgs, args...)

			return executeAnalyze(cleanArgs)
		},
	}

	cmd.Flags().BoolP("unsecure", "u", false, "Disable payload masking / security sanitization")
	cmd.Flags().BoolP("scenario", "S", false, "Analyze capture into scenario flow")
	return cmd
}



func executeAnalyze(args []string) error {
	specPath := cfg.GetConfig().GetSpec()
	txPath := cfg.GetConfig().GetFile()

	var spec *iso8583.MessageSpec
	if specPath != "" {
		if s, err := utils.CreateSpecFromFile(specPath); err == nil {
			spec = s
		}
	}

	var tcRepo transactions.Repository
	if txPath != "" && spec != nil {
		if tc, err := transactions.NewTransactionCollection(txPath, spec); err == nil {
			tcRepo = tc
		}
	}

	cmdObj := cmdpkg.NewAnalyzeCommand(spec, tcRepo)
	cmdObj.SetArgs(args)
	return cmdObj.Execute()
}
