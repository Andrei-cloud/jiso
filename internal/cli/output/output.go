// Package output provides the v2 CLI output renderer.
//
// Contract: stdout carries command output only. Under
// --json stdout is pure parseable JSON with zero decoration (no colors, no
// table chars, no trailing prose); notices are suppressed under --quiet and
// --json; --dry-run is surfaced to commands as a resolved boolean so
// mutating commands can print their plan without writing or sending.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Renderer renders command output according to the resolved output flags
// (--json, --quiet, --dry-run).
type Renderer struct {
	out    io.Writer
	errOut io.Writer
	json   bool
	quiet  bool
	dryRun bool
}

// New builds a Renderer from the command's IO writers and the output flags
// resolved on it. Flags absent from the command default to off.
func New(cmd *cobra.Command) *Renderer {
	r := &Renderer{
		out:    cmd.OutOrStdout(),
		errOut: cmd.ErrOrStderr(),
		json:   flagBool(cmd, "json"),
		quiet:  flagBool(cmd, "quiet"),
		dryRun: flagBool(cmd, "dry-run"),
	}

	return r
}

func flagBool(cmd *cobra.Command, name string) bool {
	if cmd.Flags().Lookup(name) == nil {
		return false
	}

	v, _ := cmd.Flags().GetBool(name)

	return v
}

// JSON reports whether machine-readable JSON output is active.
func (r *Renderer) JSON() bool { return r.json }

// Quiet reports whether non-essential stdout notices are suppressed.
func (r *Renderer) Quiet() bool { return r.quiet }

// DryRun reports whether the command must show its plan without writing or
// sending anything.
func (r *Renderer) DryRun() bool { return r.dryRun }

// Out returns the stdout writer human printers must write to.
func (r *Renderer) Out() io.Writer { return r.out }

// Err returns the stderr writer for diagnostics.
func (r *Renderer) Err() io.Writer { return r.errOut }

// Data emits the command's data: under --json it JSON-encodes v to stdout
// with a two-space indent; otherwise it delegates to human, the
// caller-supplied human-readable printer. A nil slice/map is the caller's
// responsibility — pass an empty non-nil value to serialize as [] / {}
// instead of null.
func (r *Renderer) Data(v any, human func()) error {
	if r.json {
		enc := json.NewEncoder(r.out)
		enc.SetIndent("", "  ")

		return enc.Encode(v)
	}

	if human != nil {
		human()
	}

	return nil
}

// Noticef prints a non-essential notice to stdout. It is suppressed under
// --quiet and under --json, where stdout must stay pure data.
func (r *Renderer) Noticef(format string, args ...any) {
	if r.quiet || r.json {
		return
	}

	_, _ = fmt.Fprintf(r.out, format+"\n", args...)
}
