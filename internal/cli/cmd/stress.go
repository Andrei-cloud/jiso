package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/cli/output"
	cfg "jiso/internal/config"
	"jiso/internal/transactions"
)

// stressCompletionGrace bounds the wait for the final WorkerStopped after
// the planned ramp+duration window: in-flight async sends finish on their
// response or the response timeout, so a healthy run never reaches it.
const stressCompletionGrace = 30 * time.Second

// newStressCmd builds the headless stress command: it drives the
// App worker manager (the same StressStart/StressStop/StressSummaryByID
// entry points the TUI worker screens use), blocks until the duration
// elapses or SIGINT, and prints the final StressSummary.
func newStressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stress",
		Short: "Run a headless stress test against the target",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeStress(cmd)
		},
	}

	cmd.Flags().StringSlice("tx", nil, "Transaction names to stress (comma-separated, from the loaded tx file)")
	cmd.Flags().Int("tps", 10, "Target transactions per second")
	cmd.Flags().Duration("ramp", 30*time.Second, "TPS ramp-up duration (e.g. 30s, 1m)")
	cmd.Flags().Duration("duration", time.Minute, "Test duration after ramp-up (e.g. 1m, 5m)")
	cmd.Flags().Int("workers", 1, "Concurrent sender workers (1-50)")
	cmd.Flags().StringP("report", "R", "", "Path to export the test report JSON")
	cmd.Flags().BoolP("yes", "y", false, "No-op confirmation: this command is non-interactive and never prompts")

	return cmd
}

// stressPlan is the machine-readable --dry-run plan of a stress run.
type stressPlan struct {
	DryRun   bool          `json:"dry_run"`
	Tx       []string      `json:"tx"`
	TPS      int           `json:"tps"`
	Ramp     time.Duration `json:"ramp"`
	Duration time.Duration `json:"duration"`
	Workers  int           `json:"workers"`
	Target   string        `json:"target"`
	Report   string        `json:"report,omitempty"`
}

func executeStress(cmd *cobra.Command) error {
	out := output.New(cmd)
	c := cfg.GetConfig()

	// Data command: PersistentPreRunE already load-validated the resolved
	// files; this names missing paths as the usage-class error (send
	// precedent).
	tc, err := requireSpecAndTx()
	if err != nil {
		return err
	}

	txs, _ := cmd.Flags().GetStringSlice("tx")
	tps, _ := cmd.Flags().GetInt("tps")
	ramp, _ := cmd.Flags().GetDuration("ramp")
	duration, _ := cmd.Flags().GetDuration("duration")
	workers, _ := cmd.Flags().GetInt("workers")
	reportPath, _ := cmd.Flags().GetString("report")

	txs = cleanTxNames(txs)
	if len(txs) == 0 {
		_, _ = fmt.Fprintln(out.Err(), "Error: --tx is required: comma-separated transaction names from the loaded tx file (e.g. --tx Echo,Purchase)")

		return &ExitCodeError{Code: ExitUsage}
	}
	if code, err := validateStressNumbers(tps, workers, ramp, duration); err != nil {
		_, _ = fmt.Fprintf(out.Err(), "Error: %v\n", err)

		return &ExitCodeError{Code: code}
	}

	// Static validation first: an unknown name fails as exit 3 naming it,
	// before any target or connection concern.
	if err := validateStressTxNames(tc, txs, c.GetFile()); err != nil {
		return err
	}

	host, port := strings.TrimSpace(c.GetHost()), strings.TrimSpace(c.GetPort())
	target := "(not configured)"
	if host != "" && port != "" {
		target = host + ":" + port
	}

	if out.DryRun() {
		plan := &stressPlan{
			DryRun: true, Tx: txs, TPS: tps, Ramp: ramp,
			Duration: duration, Workers: workers, Target: target, Report: reportPath,
		}

		return out.Data(plan, func() { printStressPlan(out, plan) })
	}

	if host == "" || port == "" {
		// Config-class failure naming the resolution sources (send/
		// connect-check message precedent, taxonomy: exit 3).
		return &ExitConfigError{Err: errors.New(missingTargetMessage("stress", host, port))}
	}

	return runStress(out, tc, c, txs, tps, ramp, duration, workers, reportPath, target)
}

// validateStressTxNames fails as a config-class error naming the tx file when a
// --tx name is not defined in the loaded transaction file.
func validateStressTxNames(tc *transactions.TransactionCollection, txs []string, filePath string) error {
	for _, name := range txs {
		if _, err := tc.Info(name); err != nil {
			return &ExitConfigError{
				Path: filePath,
				Err:  fmt.Errorf("unknown transaction '%s' in --tx: not defined in the transaction file", name),
			}
		}
	}

	return nil
}

// runStress creates the app, connects, runs the stress worker to completion and
// hands the outcome to finishStress, disconnecting on every exit path.
func runStress(out *output.Renderer, tc *transactions.TransactionCollection, c *cfg.Config, txs []string, tps int, ramp, duration time.Duration, workers int, reportPath, target string) error {
	a, err := app.New(c)
	if err != nil {
		var appCfgErr *app.ConfigError
		if errors.As(err, &appCfgErr) {
			return &ExitConfigError{Path: appCfgErr.Path, Err: err}
		}

		return err
	}
	defer func() { _ = a.Close() }()

	a.Service().SetDebugMode(false)

	sender := newStressSender(a, tc)
	a.SetWorkerSenderResolver(func() app.WorkerSender { return sender })

	out.Noticef("Connecting to server at %s...", target)
	if err := a.Connect(); err != nil {
		// Runtime network failure: exit 1 naming the target (send precedent).
		return fmt.Errorf("failed to connect to %s: %w", target, err)
	}
	defer func() {
		if err := a.Disconnect(); err != nil {
			_, _ = fmt.Fprintf(out.Err(), "Warning: disconnect: %v\n", err)
		}
	}()

	// Subscribe before start so no lifecycle event can be missed.
	ch, unsub := a.Events().Subscribe()
	defer unsub()

	workerID, err := a.StressStart(txs, tps, ramp, duration, workers)
	if err != nil {
		return fmt.Errorf("failed to start stress worker: %w", err)
	}
	out.Noticef("Stress worker %s started: txs=%s target-tps=%d ramp=%s duration=%s workers=%d",
		workerID, strings.Join(txs, ","), tps, ramp, duration, workers)

	interrupted, timedOut := waitForStressWorker(out, a, workerID, ch, ramp+duration+stressCompletionGrace)

	return finishStress(out, a, stressOutcome{workerID: workerID, interrupted: interrupted, timedOut: timedOut}, reportPath, target)
}

// stressOutcome is the terminal state of a stress worker run.
type stressOutcome struct {
	workerID    string
	interrupted bool
	timedOut    bool
}

// finishStress fetches the worker summary, renders it (and saves the optional
// report), and maps the outcome to the process exit code.
func finishStress(out *output.Renderer, a *app.App, o stressOutcome, reportPath, target string) error {
	summary, err := a.StressSummaryByID(o.workerID)
	if err != nil {
		if o.timedOut {
			return fmt.Errorf("stress worker %s did not finish and its summary is unavailable: %w", o.workerID, err)
		}

		return err
	}

	if err := out.Data(summary, func() { printStressSummary(out, summary, target) }); err != nil {
		return err
	}

	if reportPath != "" {
		if err := saveStressReport(out, reportPath, summary); err != nil {
			_, _ = fmt.Fprintf(out.Err(), "Warning: Failed to save test report: %v\n", err)
		}
	}

	switch {
	case o.interrupted:
		return &ExitCodeError{Code: ExitSIGINT}
	case o.timedOut:
		return fmt.Errorf("stress worker %s did not finish within ramp+duration+grace", o.workerID)
	case summary.Failed > 0:
		// E1-FIX #2 typed-error path (scenario-run precedent): a completed
		// run with failures exits ExitTestFailure.
		return &ExitCodeError{Code: ExitTestFailure}
	default:
		return nil
	}
}

// cleanTxNames trims --tx entries and drops empty ones (StringSlice
// tolerates "--tx a,,b" and surrounding spaces).
func cleanTxNames(txs []string) []string {
	cleaned := make([]string, 0, len(txs))
	for _, name := range txs {
		if name = strings.TrimSpace(name); name != "" {
			cleaned = append(cleaned, name)
		}
	}

	return cleaned
}

// validateStressNumbers applies the legacy worker-prompt bounds as flag
// validation: usage-class failures (exit 2).
func validateStressNumbers(tps, workers int, ramp, duration time.Duration) (int, error) {
	switch {
	case tps <= 0:
		return ExitUsage, errors.New("TPS must be greater than 0")
	case tps > 100000:
		return ExitUsage, errors.New("TPS cannot exceed 100000")
	case workers <= 0:
		return ExitUsage, errors.New("workers must be greater than 0")
	case workers > 50:
		return ExitUsage, errors.New("workers cannot exceed 50")
	case ramp < 0:
		return ExitUsage, fmt.Errorf("ramp must not be negative, got %s", ramp)
	case duration <= 0:
		return ExitUsage, fmt.Errorf("duration must be greater than 0, got %s", duration)
	}

	return ExitOK, nil
}

// waitForStressWorker blocks until the worker publishes its final
// WorkerStopped, a signal arrives, or the grace deadline fires. It returns
// (interrupted, timedOut); on signal it stops the worker gracefully first
// so the final summary is complete when it returns.
//
// The stress command owns SIGINT/SIGTERM here: the root watcher's
// immediate os.Exit would skip the graceful stop and the partial summary,
// so this command takes the handlers over for the duration of the run
// (signal.Reset drops the root watcher's channels, then Notify installs
// this one; signal.Stop restores the process default afterwards).
func waitForStressWorker(
	out *output.Renderer,
	a *app.App,
	workerID string,
	ch <-chan events.Event,
	timeout time.Duration,
) (interrupted, timedOut bool) {
	signal.Reset(os.Interrupt, syscall.SIGTERM)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return false, true
			}
			if stopped, isStopped := ev.(events.WorkerStopped); isStopped && stopped.ID == workerID {
				return false, false
			}
		case sig := <-sigCh:
			_, _ = fmt.Fprintf(out.Err(), "\nreceived signal %v, stopping stress worker %s gracefully\n", sig, workerID)
			if err := a.StressStop(workerID); err != nil {
				_, _ = fmt.Fprintf(out.Err(), "Warning: stop stress worker: %v\n", err)
			}

			return true, false
		case <-deadline.C:
			_, _ = fmt.Fprintf(out.Err(), "Warning: stress worker %s did not finish within %s, stopping\n", workerID, timeout)
			if err := a.StressStop(workerID); err != nil {
				_, _ = fmt.Fprintf(out.Err(), "Warning: stop stress worker: %v\n", err)
			}

			return false, true
		}
	}
}
