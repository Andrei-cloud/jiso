package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"jiso/internal/app"
	clicmd "jiso/internal/cli/cmd"
	cfg "jiso/internal/config"
	"jiso/internal/tui"
	"jiso/internal/utils"
)

// runTUI builds the internal/app façade, runs the Bubble Tea v2 session, and
// returns the process exit code. The deferred Close() runs before the code
// is returned (same rule as runREPL, M1 review #13); the TUI itself never
// touches os.Stdout outside the program loop. Its error is checked (see the
// defer) because a failed shutdown is a real failure the user should see.
func runTUI(ctx context.Context) (code int) {
	application, err := app.New(cfg.GetConfig())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error initializing app for TUI: %v\n", err)

		return clicmd.ExitError
	}
	// App.Close reports worker-stop timeouts, serve-engine stop failures,
	// and the service close failure; a clean TUI exit must not hide them,
	// so a close failure on an otherwise-OK exit is logged and downgrades
	// the exit code.
	defer func() {
		if err := application.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error closing app: %v\n", err)

			if code == clicmd.ExitOK {
				code = clicmd.ExitError
			}
		}
	}()
	// The worker manager needs a sender resolver (the cobra CLI and the
	// service path wire their own); without it background-send and
	// stress starts fail (UAT round 4).
	clicmd.WireWorkerSender(application)

	if err := tui.Run(ctx, application); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)

		return clicmd.ExitError
	}

	return clicmd.ExitOK
}

func main() {
	os.Exit(run())
}

// run executes the CLI and flushes the STAN counter to the persistent
// state dir on every normal exit path (os.Exit skips defers, so the
// flush lives here rather than in main).
func run() int {
	ctx := context.Background()

	// Configure TUI runner callback for the full-screen session (TUI-401).
	clicmd.SetTUIRunner(func(tuiCtx context.Context) error {
		if code := runTUI(tuiCtx); code != clicmd.ExitOK {
			return &clicmd.ExitCodeError{Code: code}
		}

		return nil
	})

	// Execute Cobra root command structure; SIGINT/SIGTERM handling lives in
	// the root command's PersistentPreRunE (exit 128+signal).
	defer utils.StopPersistWorker()

	if err := clicmd.ExecuteContext(ctx); err != nil {
		var exitErr *clicmd.ExitCodeError
		if !errors.As(err, &exitErr) {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}

		return clicmd.ExitCodeForError(err)
	}

	return clicmd.ExitOK
}
