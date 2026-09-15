// serverroutes.go holds PAR-309's `serve start` foreground-run path: the
// --routes-file mock-route resolution (delegated to app.ResolveRoutes, with
// the exit-code mapping kept here), the command Long help pinning the
// systemd exit-code contract, and executeServerStart wiring both into
// command.ServerCommand.RunDirectServer.
package cmd

import (
	"errors"

	"github.com/moov-io/iso8583"
	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
	cfg "jiso/internal/config"
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

// serveRoutesError translates a loader failure from app.ResolveRoutes into
// the CLI's exit-code taxonomy: every routes-file failure is a config-class
// error (exit 3) naming the path (PAR-309). The precedence and parsing live
// in internal/app (shared with the TUI); only this mapping stays here.
func serveRoutesError(err error) error {
	var appCfgErr *app.ConfigError
	if errors.As(err, &appCfgErr) {
		return &ExitConfigError{Path: appCfgErr.Path, Err: appCfgErr.Err}
	}

	return err
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

	routes, tcRepo, err := app.ResolveRoutes(routesFile, txPath, spec)
	if err != nil {
		return serveRoutesError(err)
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
