// output.go is the package's single system-output seam (UAT): the
// connection manager's lifecycle lines (unsafe read errors, reconnect
// chatter, route-match notices) go straight to os.Stderr, as they
// always did for the CLI/REPL. The TUI no longer swaps the sink: its
// bottom console strip is gone, so these plain writes land in the
// terminal's scrollback instead of an owned in-frame pane.
package connection

import (
	"fmt"
	"os"
)

// outputf writes one formatted lifecycle line to the system output.
func outputf(format string, a ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format, a...)
}
