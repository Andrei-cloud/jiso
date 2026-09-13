// output.go is the package's single system-output sink (UAT): the
// connection manager's lifecycle lines (unsafe read errors, reconnect
// chatter, route-match notices) used to write straight to os.Stderr,
// which corrupts the TUI's alternate screen mid-frame. The sink defaults
// to os.Stderr (CLI/REPL parity) and is swappable via SetOutput — the
// TUI installs a capture writer that routes every line into its
// bottom-of-screen console pane.
package connection

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// sink is the io.Writer every lifecycle line goes through; atomic so
// SetOutput is safe while manager goroutines are live.
var sink atomic.Pointer[io.Writer]

func init() {
	var w io.Writer = os.Stderr
	sink.Store(&w)
}

// SetOutput redirects the package's system output (nil = os.Stderr).
func SetOutput(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	sink.Store(&w)
}

// outputf writes one formatted lifecycle line to the sink.
func outputf(format string, a ...any) {
	if w := sink.Load(); w != nil {
		_, _ = fmt.Fprintf(*w, format, a...)
	}
}

// Output reports the current sink (for swap-and-restore owners).
func Output() io.Writer {
	if w := sink.Load(); w != nil {
		return *w
	}

	return os.Stderr
}
