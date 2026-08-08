package cmd

import (
	"errors"
	"strings"

	cfg "jiso/internal/config"

	cmdpkg "jiso/internal/command"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	serverCmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"serve"},
		Short:   "Manage embedded ISO8583 mock server",
	}

	serverCmd.AddCommand(newServerStartCmd())
	serverCmd.AddCommand(newServerRoutesCmd())
	return serverCmd
}

func newServerStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [port] [headerType]",
		Short: "Start embedded ISO8583 mock server in direct mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			portFlag, _ := cmd.Flags().GetString("port")
			headerFlag, _ := cmd.Flags().GetString("header")

			port := "9999"
			headerType := "binary2"

			if portFlag != "" {
				port = portFlag
			}
			if headerFlag != "" {
				headerType = headerFlag
			}

			if len(args) > 0 {
				if isNumeric(args[0]) {
					port = args[0]
					if len(args) > 1 {
						headerType = args[1]
					}
				} else {
					sub := strings.ToLower(args[0])
					if sub == "stop" {
						return errors.New("'serve stop' is only applicable in interactive REPL mode. Stop standalone server with Ctrl+C")
					}
					if sub == "routes" || sub == "list" {
						return executeServerRoutes()
					}
					if len(args) > 1 {
						port = args[1]
					}
					if len(args) > 2 {
						headerType = args[2]
					}
				}
			}

			return executeServerStart(port, headerType)
		},
	}

	cmd.Flags().StringP("port", "p", "9999", "Port number to listen on")
	cmd.Flags().StringP("header", "m", "binary2", "TCP header length type (binary2, ascii4, bcd2, NAPS, visa)")
	return cmd
}

func newServerRoutesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "routes",
		Aliases: []string{"list"},
		Short:   "List active mock routes for server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeServerRoutes()
		},
	}
}



func executeServerStart(port, headerType string) error {
	specPath := cfg.GetConfig().GetSpec()
	txPath := cfg.GetConfig().GetFile()

	var spec *iso8583.MessageSpec
	var routes []cfg.MockRouteConfig
	var tcRepo transactions.Repository

	if specPath != "" {
		if s, err := utils.CreateSpecFromFile(specPath); err == nil {
			spec = s
		}
	}
	if spec == nil {
		spec = utils.GetDefaultSpec()
	}

	if txPath != "" && spec != nil {
		if tc, err := transactions.NewTransactionCollection(txPath, spec); err == nil {
			routes = tc.GetMockRoutes()
			tcRepo = tc
		}
	}

	cmdObj := cmdpkg.NewServerCommand(spec, routes, tcRepo)
	return cmdObj.RunDirectServer(port, headerType)
}

func executeServerRoutes() error {
	specPath := cfg.GetConfig().GetSpec()
	txPath := cfg.GetConfig().GetFile()

	var spec *iso8583.MessageSpec
	var routes []cfg.MockRouteConfig
	var tcRepo transactions.Repository

	if specPath != "" {
		if s, err := utils.CreateSpecFromFile(specPath); err == nil {
			spec = s
		}
	}
	if spec == nil {
		spec = utils.GetDefaultSpec()
	}

	if txPath != "" && spec != nil {
		if tc, err := transactions.NewTransactionCollection(txPath, spec); err == nil {
			routes = tc.GetMockRoutes()
			tcRepo = tc
		}
	}

	cmdObj := cmdpkg.NewServerCommand(spec, routes, tcRepo)
	cmdObj.ListRoutes()
	return nil
}

func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
