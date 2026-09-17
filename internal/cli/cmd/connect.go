package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	cfg "jiso/internal/config"
)

func newConnectCmd() *cobra.Command {
	connectCmd := &cobra.Command{
		Use:   "connect",
		Short: "Connection operations against the target",
	}

	connectCmd.AddCommand(newConnectCheckCmd())

	return connectCmd
}

// newConnectCheckCmd builds the scriptable health probe:
//
//	jiso connect check --host H --port P [--connect-timeout 2s] [--json]
//
// host/port resolve flag > JISO_* env > user config as everywhere else.
// The probe is ONE TCP dial bounded by --connect-timeout (plus a TLS client
// handshake when --tls-config enables TLS) with no prompts and no retries
// — the reconnect-attempts=0 semantics of a ping. The exit code IS the
// reachability verdict: 0 reachable, 1 connection refused/timeout/TLS
// failure, 2 missing host/port config (usage).
//
// --json contract (deliberate exception to the v2 "no data on failure"
// rule): the probe result {"target","reachable","latency_ms","error"} is
// the command's data and is printed to stdout whether the target is
// reachable or not; only then does the exit code carry the failure. It is
// the only command that emits JSON on stdout while exiting non-zero.
func newConnectCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Probe target reachability (exit code reflects reachability)",
		Long: "Probe the configured target with a single TCP dial (plus a TLS\n" +
			"handshake when a TLS config is enabled), bounded by --connect-timeout.\n" +
			"No prompts, no retries: exactly one attempt, reconnect-attempts=0\n" +
			"semantics.\n\n" +
			"Exit codes: 0 reachable, 1 unreachable (refused/timeout/TLS error,\n" +
			"message on stderr naming host:port), 2 host/port not configured.\n\n" +
			"--json prints {\"target\",\"reachable\",\"latency_ms\",\"error\"} on stdout\n" +
			"whether or not the target is reachable — the probe result IS the\n" +
			"data, and the exit code still reflects reachability. This is the one\n" +
			"command that emits JSON on stdout on failure.",
		Annotations: map[string]string{skipConfigValidationAnnotation: annotationSet},
		Args:        cobra.NoArgs,
		RunE:        executeConnectCheck,
	}
}

func executeConnectCheck(cmd *cobra.Command, _ []string) error {
	out := output.New(cmd)
	c := cfg.GetConfig()

	// A health probe consumes no spec/tx files; only the target matters.
	host, port := strings.TrimSpace(c.GetHost()), strings.TrimSpace(c.GetPort())
	if host == "" || port == "" {
		_, _ = fmt.Fprintf(out.Err(), "Error: %s\n", missingTargetMessage("check", host, port))

		return &ExitCodeError{Code: ExitUsage}
	}

	probe := app.ProbeTarget(cmd.Context(), host, port, c.GetConnectTimeout(), c.GetTLSConfig())

	if err := out.Data(probe, func() { printConnectCheckHuman(out, probe) }); err != nil {
		return err
	}

	if !probe.Reachable {
		if !out.JSON() {
			_, _ = fmt.Fprintf(out.Err(), "connect check failed: %s: %s\n", probe.Target, probe.Error)
		}

		return &ExitCodeError{Code: ExitError}
	}

	return nil
}

func printConnectCheckHuman(out *output.Renderer, probe *app.TargetProbe) {
	if !probe.Reachable {
		// The failure line goes to stderr in executeConnectCheck; stdout
		// stays empty so `connect check && ...` pipelines stay clean.
		return
	}

	_, _ = fmt.Fprintf(out.Out(), "ok %s (%s)\n", probe.Target, probe.Latency.Round(time.Microsecond))
}
