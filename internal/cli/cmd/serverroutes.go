// serverroutes.go holds PAR-309's `serve start` foreground-run path: the
// --routes-file mock-route resolution (precedence table below), the command
// Long help pinning the systemd exit-code contract, and executeServerStart
// wiring both into command.ServerCommand.RunDirectServer.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/moov-io/iso8583"
	"github.com/spf13/cobra"

	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// serveStartLongHelp is the `serve start` Long help. It pins the PAR-309
// contract: routes precedence, the graceful-stop exit 0 (and why it differs
// from one-shot commands' 130), and --json stdout purity during the run.
const serveStartLongHelp = `Start the embedded ISO8583 mock server in the foreground and block until
SIGINT/SIGTERM stops it.

Mock routes precedence: --routes-file > the --file tx file's mock_routes
entries > no routes. --routes-file must be a JSON array of mock route
objects (the same shape as the tx file's mock_route entries; a missing or
malformed file fails with exit 3 naming the path, per the config-error
taxonomy).

Graceful stop: on SIGINT/SIGTERM the server stops cleanly and the PAR-304
state/snapshot files in the jiso state dir are removed; with --report <path>
the final ServerStats JSON is written there atomically. A clean signal stop
then exits 0: for a foreground server (systemd, nohup, tmux) being stopped
by a signal is success, deliberately unlike one-shot commands where an
interrupted run means unfinished work and exits 130.

--json does not change the blocking nature: stdout stays EMPTY while the
server runs (all logs go to stderr), and the final ServerStats JSON is
printed to stdout on a clean stop, so ` + "`jiso serve start --json | jq`" + `
receives the summary when the server is stopped.`

// resolveServeRoutes implements the documented precedence
// --routes-file > the tx file's mock_routes > no routes.
//
// An explicit --routes-file that cannot be read or parsed is always a
// config error (exit 3) naming the path — the user asked for exactly that
// file, so silently serving without routes would be a lie. The tx-file
// source keeps the pre-PAR-309 silent fallback: a tx file that fails to
// load contributes zero routes (its parse failures surface in the commands
// that actually consume transactions).
func resolveServeRoutes(routesFile, txPath string, spec *iso8583.MessageSpec) ([]cfg.MockRouteConfig, transactions.Repository, error) {
	if routesFile = strings.TrimSpace(routesFile); routesFile != "" {
		routes, err := loadServeRoutesFile(routesFile)
		if err != nil {
			return nil, nil, err
		}

		return routes, nil, nil
	}

	if txPath != "" && spec != nil {
		if tc, err := transactions.NewTransactionCollection(txPath, spec); err == nil {
			return tc.GetMockRoutes(), tc, nil
		}
	}

	return nil, nil, nil
}

// loadServeRoutesFile parses an explicit mock-routes JSON file: an array of
// route objects shaped like the tx file's mock_route entries (their "type"
// field is simply ignored). Every failure names the path so the exit-3
// message identifies the file the user pointed at.
func loadServeRoutesFile(path string) ([]cfg.MockRouteConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &ExitConfigError{Path: path, Err: fmt.Errorf("failed to read mock routes file: %w", err)}
	}

	var routes []cfg.MockRouteConfig
	if err := json.Unmarshal(raw, &routes); err != nil {
		return nil, &ExitConfigError{Path: path, Err: fmt.Errorf("malformed mock routes file: %w", err)}
	}

	for i := range routes {
		if strings.TrimSpace(routes[i].Name) == "" {
			return nil, &ExitConfigError{Path: path, Err: fmt.Errorf("mock route #%d is missing a name", i+1)}
		}
	}

	return routes, nil
}

// executeServerStart resolves routes with the PAR-309 precedence BEFORE
// binding (a broken --routes-file fails with exit 3 without ever opening
// the listening socket), then runs the foreground server.
func executeServerStart(cmd *cobra.Command, port, headerType string) error {
	out := output.New(cmd)
	routesFile, _ := cmd.Flags().GetString("routes-file")
	reportPath, _ := cmd.Flags().GetString("report")

	specPath := cfg.GetConfig().GetSpec()
	txPath := cfg.GetConfig().GetFile()

	var spec *iso8583.MessageSpec
	if specPath != "" {
		if s, err := utils.CreateSpecFromFile(specPath); err == nil {
			spec = s
		}
	}
	if spec == nil {
		spec = utils.GetDefaultSpec()
	}

	routes, tcRepo, err := resolveServeRoutes(routesFile, txPath, spec)
	if err != nil {
		return err
	}

	cmdObj := cmdpkg.NewServerCommand(spec, routes, tcRepo)

	return cmdObj.RunDirectServer(cmdpkg.DirectServerOptions{
		Port:       port,
		HeaderType: headerType,
		ReportPath: reportPath,
		JSONStdout: out.JSON(),
		Stdout:     out.Out(),
		Stderr:     out.Err(),
	})
}
