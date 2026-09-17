package cmd

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	cfg "jiso/internal/config"
)

func newServerCmd() *cobra.Command {
	serverCmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"serve"},
		Short:   "Manage embedded ISO8583 mock server",
	}

	serverCmd.AddCommand(newServerStartCmd())
	serverCmd.AddCommand(newServerStatsCmd())
	serverCmd.AddCommand(newServerRoutesCmd())
	return serverCmd
}

func newServerStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [port] [headerType]",
		Short: "Start embedded ISO8583 mock server in direct mode",
		Long:  serveStartLongHelp,
		// Serve start owns SIGINT/SIGTERM (clean stop, exit 0);
		// the fail-fast 128+signal watcher must not race it.
		Annotations: map[string]string{skipGlobalSignalWatcherAnnotation: annotationSet},
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
				if sub := strings.ToLower(args[0]); sub == "routes" || sub == subCmdList {
					return executeServeRoutes(cmd, nil)
				}
			}

			var err error
			port, headerType, err = resolveServeStartArgs(args, port, headerType)
			if err != nil {
				return err
			}

			return executeServerStart(cmd, port, headerType)
		},
	}

	cmd.Flags().StringP("port", "p", "9999", "Port number to listen on")
	cmd.Flags().StringP("header", "m", "binary2", "TCP header length type (binary2, ascii4, bcd2, NAPS, visa)")
	cmd.Flags().String("routes-file", "", "Explicit mock routes JSON file; overrides the tx file's mock_routes (precedence: --routes-file > tx file mock_routes > none)")
	cmd.Flags().StringP("report", "R", "", "Write final ServerStats JSON to this path on a clean SIGINT/SIGTERM stop")
	return cmd
}

// resolveServeStartArgs maps serve-start positional args onto a port and header
// type, overriding the given defaults. It returns an error for the 'stop'
// subcommand, which is only meaningful in interactive REPL mode.
func resolveServeStartArgs(args []string, port, headerType string) (outPort, outHeader string, err error) {
	if len(args) == 0 {
		return port, headerType, nil
	}

	if isNumeric(args[0]) {
		port = args[0]
		if len(args) > 1 {
			headerType = args[1]
		}

		return port, headerType, nil
	}

	if sub := strings.ToLower(args[0]); sub == "stop" {
		return "", "", errors.New("'serve stop' is only applicable in interactive REPL mode. Stop standalone server with Ctrl+C")
	}

	if len(args) > 1 {
		port = args[1]
	}
	if len(args) > 2 {
		headerType = args[2]
	}

	return port, headerType, nil
}

// newServerStatsCmd builds the out-of-process stats query:
//
//	jiso serve stats [port] [--json]
//
// It reads the side-channel files the running server publishes in the
// jiso state dir ($JISO_STATE_DIR override, else $XDG_STATE_HOME or
// ~/.local/state/jiso): serve-<port>.json (pid/host/port/started_at/db)
// and serve-<port>.stats.json (a ServerStats snapshot rewritten atomically
// every second while the server runs). The snapshot file was chosen over
// an admin endpoint: no extra port to juggle, rename-per-write gives
// readers a consistent view, and 1 s freshness fits a stats view.
//
// Port resolution: positional [port] > --port/-p > $JISO_PORT > user
// config. A state file whose PID is dead (crash, kill -9) is treated as
// "not running"; the stale file is reported, not deleted (the next
// `serve start` overwrites it).
//
// Exit codes: 0 stats printed; 1 no running server on the port (missing
// state file, dead PID, or unreadable snapshot) with the message on
// stderr — under --json stdout stays EMPTY there, deliberately unlike
// `connect check` (the probe result was that command's data; here no
// stats data exists when nothing runs); 2 port not configured.
func newServerStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "stats [port]",
		Short:       "Show live statistics of a running mock server",
		Long:        "Query a RUNNING mock server (started by `jiso serve start` or the\nREPL `serve`) from outside its process via the state/snapshot files it\npublishes in the jiso state dir ($JISO_STATE_DIR override, else\n$XDG_STATE_HOME or ~/.local/state/jiso):\n\n  serve-<port>.json        pid/host/port/started_at, written at start\n  serve-<port>.stats.json  stats snapshot, refreshed every 1s while running\n\nThe snapshot file is the channel (no admin port). Clean stops remove both\nfiles; after a crash the state file is reported as stale, not deleted.\n\nPort resolution: positional [port] > --port/-p > $JISO_PORT > user config.\n\nExit codes: 0 stats printed; 1 no running server on the port (message on\nstderr; with --json stdout stays empty — no stats data exists to report,\nunlike `connect check` where the probe itself is the data); 2 port not\nconfigured.\n\n--json prints the ServerStats snapshot: uptime, active connections,\nmessages served, TPS, and per-MTI / route / response-code (DE 39) counts.",
		Annotations: map[string]string{skipConfigValidationAnnotation: annotationSet},
		Args:        cobra.MaximumNArgs(1),
		RunE:        executeServeStats,
	}
}

func executeServeStats(cmd *cobra.Command, args []string) error {
	out := output.New(cmd)

	port := ""
	if len(args) > 0 {
		port = strings.TrimSpace(args[0])
	}
	if port == "" {
		port = strings.TrimSpace(cfg.GetConfig().GetPort())
	}
	if port == "" {
		_, _ = fmt.Fprintf(out.Err(), "Error: cannot serve stats: port is not configured "+
			"(use a positional port, --port/-p, $JISO_PORT, or the user config file)\n")

		return &ExitCodeError{Code: ExitUsage}
	}
	if !isNumeric(port) {
		_, _ = fmt.Fprintf(out.Err(), "Error: cannot serve stats: invalid port %q (must be a number)\n", port)

		return &ExitCodeError{Code: ExitUsage}
	}

	state, statePath, err := app.ReadServeState(port)
	if err != nil {
		_, _ = fmt.Fprintf(out.Err(), "no running jiso server on port %s (state file %s unreadable: %v)\n", port, statePath, err)

		return &ExitCodeError{Code: ExitError}
	}
	if state == nil {
		_, _ = fmt.Fprintf(out.Err(), "no running jiso server on port %s (state file %s not found; start one with `jiso serve start --port %s`)\n",
			port, statePath, port)

		return &ExitCodeError{Code: ExitError}
	}
	if !app.PIDAlive(state.PID) {
		_, _ = fmt.Fprintf(out.Err(), "no running jiso server on port %s (stale state file %s: pid %d is not alive)\n",
			port, statePath, state.PID)

		return &ExitCodeError{Code: ExitError}
	}

	view, statsPath, err := app.ReadServeStatsSnapshot(port)
	if err != nil {
		_, _ = fmt.Fprintf(out.Err(), "jiso server on port %s (pid %d) is running but its stats snapshot %s is unreadable: %v\n",
			port, state.PID, statsPath, err)

		return &ExitCodeError{Code: ExitError}
	}

	if err := out.Data(view, func() { printServeStatsHuman(out, state, view) }); err != nil {
		return err
	}

	return nil
}

// printServeStatsHuman renders the snapshot without absolute timestamps so
// the output stays golden-stable; freshness is shown as an age.
func printServeStatsHuman(out *output.Renderer, state *app.ServeState, view *app.ServerStats) {
	w := out.Out()

	age := "unknown age"
	if view.SnapshotAt != nil {
		age = time.Since(*view.SnapshotAt).Round(time.Second).String() + " ago"
	}

	_, _ = fmt.Fprintf(w, "MOCK SERVER STATISTICS — port %s (pid %d, header %s)\n", view.Port, state.PID, view.HeaderType)
	_, _ = fmt.Fprintf(w, "Status:              running (snapshot %s)\n", age)
	_, _ = fmt.Fprintf(w, "Uptime:              %s\n", view.Uptime.Round(time.Second))
	_, _ = fmt.Fprintf(w, "Sessions:            %d active TCP connection(s)\n", view.ActiveConnections)
	_, _ = fmt.Fprintf(w, "Messages served:     %d (in: %d, out: %d)\n", view.TotalServed, view.TotalServed, view.TotalServed)
	_, _ = fmt.Fprintf(w, "Throughput:          instant %.1f | peak %.1f | avg %.1f msg/s\n",
		view.InstantTPS, view.PeakTPS, view.AverageTPS)

	printServeStatsCounts(w, "ROUTES", view.RouteCounts)
	printServeStatsCounts(w, "MTIS", view.MTICounts)
	printServeStatsCounts(w, "RESPONSE CODES (DE 39)", view.ResponseCodes)
}

func printServeStatsCounts(w io.Writer, title string, counts map[string]int64) {
	_, _ = fmt.Fprintf(w, "%s:\n", title)
	if len(counts) == 0 {
		_, _ = fmt.Fprintln(w, "  (none yet)")

		return
	}

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "  %-30s %d\n", k, counts[k])
	}
}

// newServerRoutesCmd builds the `serve routes [port]`: the
// route set of the RUNNING server, read from the state file where
// the server persists it at start — the same set it matches incoming
// messages against. It never re-derives routes from the querying
// process's own spec/tx slots (the old behavior printed "No mock routes
// configured" while tx-file routes were live and matched).
//
// Port resolution matches `serve stats`: positional [port] > --port/-p >
// $JISO_PORT > user config.
//
// Exit codes: 0 route set printed (a running server without routes prints
// "No mock routes configured"); 1 no running server on the port (message
// on stderr, stdout stays clean); 2 port not configured or not a number.
func newServerRoutesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "routes [port]",
		Aliases: []string{subCmdList},
		Short:   "List active mock routes for server",
		Args:    cobra.MaximumNArgs(1),
		RunE:    executeServeRoutes,
	}
}

func executeServeRoutes(cmd *cobra.Command, args []string) error {
	out := output.New(cmd)

	port := ""
	if len(args) > 0 {
		port = strings.TrimSpace(args[0])
	}
	if port == "" {
		port = strings.TrimSpace(cfg.GetConfig().GetPort())
	}
	if port == "" {
		_, _ = fmt.Fprintf(out.Err(), "Error: cannot serve routes: port is not configured "+
			"(use a positional port, --port/-p, $JISO_PORT, or the user config file)\n")

		return &ExitCodeError{Code: ExitUsage}
	}
	if !isNumeric(port) {
		_, _ = fmt.Fprintf(out.Err(), "Error: cannot serve routes: invalid port %q (must be a number)\n", port)

		return &ExitCodeError{Code: ExitUsage}
	}

	state, statePath, err := app.ReadServeState(port)
	if err != nil {
		_, _ = fmt.Fprintf(out.Err(), "no running jiso server on port %s (state file %s unreadable: %v)\n", port, statePath, err)

		return &ExitCodeError{Code: ExitError}
	}
	if state == nil {
		_, _ = fmt.Fprintf(out.Err(), "no running jiso server on port %s (state file %s not found; start one with `jiso serve start --port %s`)\n",
			port, statePath, port)

		return &ExitCodeError{Code: ExitError}
	}
	if !app.PIDAlive(state.PID) {
		_, _ = fmt.Fprintf(out.Err(), "no running jiso server on port %s (stale state file %s: pid %d is not alive)\n",
			port, statePath, state.PID)

		return &ExitCodeError{Code: ExitError}
	}

	routes := state.Routes
	if routes == nil {
		// State file written by a legacy server: the persisted route
		// set is unavailable, so fall back to the route names the stats
		// snapshot proves were matched (post-traffic, names only) — never
		// to the querying process's local config.
		view, statsPath, err := app.ReadServeStatsSnapshot(port)
		if err != nil {
			_, _ = fmt.Fprintf(out.Err(), "jiso server on port %s (pid %d) is running but its stats snapshot %s is unreadable: %v\n",
				port, state.PID, statsPath, err)

			return &ExitCodeError{Code: ExitError}
		}
		routes = serveRoutesFromRouteCounts(view.RouteCounts)
	}

	return out.Data(routes, func() { printServeRoutesHuman(out.Out(), port, state.PID, routes) })
}

// serveRoutesFromRouteCounts derives displayable route entries from a
// stats snapshot's post-traffic route counts, dropping the engine's
// catch-all fallback key (a pseudo-route, not a configured one).
func serveRoutesFromRouteCounts(counts map[string]int64) []app.ServeRoute {
	names := make([]string, 0, len(counts))
	for name := range counts {
		if name == app.ServeFallbackRoute {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	routes := make([]app.ServeRoute, 0, len(names))
	for _, n := range names {
		routes = append(routes, app.ServeRoute{Name: n})
	}

	return routes
}

// printServeRoutesHuman renders the live route table on the command's
// stdout: the REPL ListRoutes table shape with match fields sorted for
// determinism and notices kept off stdout under --json via out.Data.
func printServeRoutesHuman(w io.Writer, port string, pid int, routes []app.ServeRoute) {
	if len(routes) == 0 {
		_, _ = fmt.Fprintln(w, "No mock routes configured")

		return
	}

	_, _ = fmt.Fprintf(w, "ACTIVE MOCK ROUTES — port %s (pid %d, %d route(s) loaded)\n", port, pid, len(routes))
	for i, r := range routes {
		match := "ANY"
		if len(r.MatchFields) > 0 {
			keys := make([]string, 0, len(r.MatchFields))
			for k := range r.MatchFields {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s=%v", k, r.MatchFields[k]))
			}
			match = strings.Join(parts, ", ")
		}

		delayStr := ""
		delayMs := r.DelayMs
		if delayMs == 0 && r.LatencyMs > 0 {
			delayMs = r.LatencyMs
		}
		if delayMs > 0 || r.JitterMs > 0 {
			delayStr = fmt.Sprintf(" | Latency: %dms (Jitter: ±%dms)", delayMs, r.JitterMs)
		}

		_, _ = fmt.Fprintf(w, " Route %d: %-25s | Match: %s | Resp MTI: %s%s\n", i+1, r.Name, match, r.ResponseMTI, delayStr)
	}
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
