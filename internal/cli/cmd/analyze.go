package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"jiso/internal/cli/output"
)

func newAnalyzeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "analyze [pcap-file] [flags]",
		Aliases: []string{"pcap"},
		Short:   "Analyze stream/PCAP capture files to extract transaction templates & datasets",
		Long: `Analyze stream/PCAP capture files to extract transaction templates & datasets.

Headless contract (PAR-307 + UAT-04): this command NEVER prompts. A
headless selection is REQUIRED: --yes, or an explicit --mode/--flow.
Without one the command exits 2 naming --yes — the interactive wizard
lives only in the TUI (§J) and the removed REPL. Flows are enumerated from
the capture by destination port; --yes picks the highest-message flow and
--flow <dstport> selects one explicitly (unknown ports exit 3 listing the
available ones). Modes: tx (default), routes, scenario. --json prints the
AnalyzeOutput result on stdout, -o writes the same JSON atomically, and
-n/--dry-run prints the flow table and the plan without writing anything.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !analyzeWantsHeadless(cmd) {
				// UAT-04: the cobra path never launches the survey
				// wizard (it printed ANSI prompt bytes on stdout and
				// exited 1 on a piped stdin). Headless selection is
				// mandatory; missing input is a usage error naming it.
				_, _ = fmt.Fprintf(output.New(cmd).Err(), "Error: %s\n", analyzeMissingSelectionMessage)

				return &ExitCodeError{Code: ExitUsage}
			}

			return runAnalyzeHeadless(cmd, args)
		},
	}

	cmd.Flags().BoolP("unsecure", "u", false, "Disable payload masking / security sanitization")
	cmd.Flags().Bool("scenario", false, "Analyze capture into scenario flow")
	cmd.Flags().String("header", "", "Header format (ascii4, binary2, bcd2, binary4, NAPS, Visa)")

	// PAR-307 headless flags.
	cmd.Flags().Bool("yes", false, "Non-interactive run: auto-pick the highest-message flow, never prompt")
	cmd.Flags().Int("flow", 0, "Destination-port flow to analyze (must exist in the capture)")
	cmd.Flags().String("mode", "tx", "Headless analysis mode: tx, routes, or scenario")
	cmd.Flags().StringP("output", "o", "", "Write the AnalyzeOutput JSON report to this file (atomic)")

	return cmd
}

// analyzeWantsHeadless reports whether the PAR-307 headless contract applies:
// --yes given (the confirmation is a no-op because the path never prompts), or
// an explicit headless selection via --mode/--flow.
func analyzeWantsHeadless(cmd *cobra.Command) bool {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return true
	}

	f := cmd.Flags()

	return f.Changed("mode") || f.Changed("flow")
}

// analyzeMissingSelectionMessage is the UAT-04 usage error for a cobra
// analyze without a headless selection. It names the missing input
// (--yes); the survey wizard it replaced printed ANSI prompt bytes on
// stdout and exited 1 ("EOF") on a piped stdin, and it only ever existed
// for the now-removed REPL — the interactive flow lives in the TUI (§J).
const analyzeMissingSelectionMessage = "cannot analyze without a headless selection: pass --yes " +
	"(auto-pick the highest-message flow) or --flow <dstport> / --mode <tx|routes|scenario>"
