package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"jiso/internal/cli/userconfig"
	cfg "jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/version"
)

// NewRootCmd creates and configures the root Cobra command for jiso.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "jiso [command]",
		Short: "jiso ISO8583 message tool and simulator",
		Long: `jiso is a powerful CLI tool and full-screen TUI for inspecting, ` +
			`generating, and simulating ISO8583 messages and test scenarios.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			printUsageHint(cmd)

			return nil
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return persistentPreRun(cmd)
		},
	}

	// -v/--version prints stamped build info to stdout, exit 0 (v2 flag policy).
	rootCmd.Version = version.Version
	rootCmd.SetVersionTemplate(versionString())

	// Persistent Global Flags
	pflags := rootCmd.PersistentFlags()
	pflags.StringP("spec", "s", "", "ISO8583 specification file path")
	pflags.StringP("file", "f", "", "Transaction payload file path")
	pflags.StringP("db", "d", "", "SQLite database file path")
	pflags.String("db-path", "", "Legacy alias for --db")
	_ = pflags.MarkHidden("db-path")
	pflags.BoolP("hex", "x", false, "Enable hex dump output for messages")
	pflags.StringP("host", "H", "", "Target server host address")
	pflags.StringP("port", "p", "", "Target server port")
	pflags.String("header", "", "ISO8583 message length header type (ascii4, binary2, bcd2, binary4, NAPS, Visa)")
	pflags.IntP("reconnect-attempts", "r", 3, "Number of reconnection attempts on failure")
	pflags.Duration("connect-timeout", 5*time.Second, "Timeout for individual connection attempts")
	pflags.Duration("total-connect-timeout", 10*time.Second, "Total timeout for connection establishment")
	pflags.Duration("response-timeout", 5*time.Second, "Timeout waiting for async message responses")
	pflags.Duration("listen-timeout", 5*time.Minute, "Timeout for waiting for incoming client connections in listener mode")
	pflags.String("visa-station-id", "", "VISA Local Station ID (6-digit hex or decimal)")
	pflags.String("tls-config", "", "Path to consolidated TLS/mTLS configuration file (JSON)")

	// v2 output flags (-o stays = output file on commands that have it).
	pflags.Bool("json", false, "Machine-readable JSON output on stdout")
	pflags.BoolP("quiet", "q", false, "Suppress non-essential stdout notices")
	pflags.BoolP("dry-run", "n", false, "Show what would happen; write/send nothing")

	// Register subcommands
	rootCmd.AddCommand(newSpecCmd())
	rootCmd.AddCommand(newTxCmd())
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.AddCommand(newSendCmd())
	rootCmd.AddCommand(newInspectCmd())
	rootCmd.AddCommand(newScenarioCmd())
	rootCmd.AddCommand(newServerCmd())
	rootCmd.AddCommand(newStressCmd())
	rootCmd.AddCommand(newAnalyzeCmd())
	rootCmd.AddCommand(newCTFCmd())
	rootCmd.AddCommand(newDbCmd())
	rootCmd.AddCommand(newREPLCmd())
	rootCmd.AddCommand(newTUICmd())
	rootCmd.AddCommand(newVersionCmd())

	return rootCmd
}

// offendingFilePath returns the file a config validation error names (PAR-300),
// or "" when the failure is about a value and no file can be named.
// persistentPreRun is the root PersistentPreRunE: it starts the signal watcher,
// resolves flag/config precedence onto the global config, and validates the
// resulting configuration files.
func persistentPreRun(cmd *cobra.Command) error {
	if !globalSignalWatchSkipped(cmd) {
		watchSignals(cmd)
	}

	// CLI-104: --flag > $JISO_* > user config > default. A
	// malformed user config file fails with exit 3 naming it
	// (CLI-105 pattern); a missing one is fine.
	uc, ucPath, err := userconfig.Load()
	if err != nil {
		return &ExitConfigError{Path: ucPath, Err: err}
	}
	resolvePrecedence(cmd, uc)

	c := cfg.GetConfig()
	c.EnsureSessionID()
	c.EnsureDefaults()

	if err := applyPersistentFlags(cmd, c); err != nil {
		return err
	}

	if configFileValidationSkipped(cmd) {
		// repl/stubs/completion/help/version run independently of the
		// spec/tx files: validate values only (CLI-105). A bad value
		// may come from the user config file, so name it when known.
		if err := c.ValidateValues(); err != nil {
			return &ExitConfigError{Path: ucPath, Err: err}
		}

		// UAT-01: the TUI runs independently of the spec/tx files but
		// still owns session-DB writes, so the seam applies here too.
		return ensureSessionDB(cmd, c)
	}

	if err := c.Validate(); err != nil {
		// PAR-300: name the file the validation error is actually
		// about; the user config path is unrelated here (it was
		// already named by its own load error above).
		return &ExitConfigError{Path: offendingFilePath(err), Err: err}
	}

	if err := loadValidateConfigFiles(c); err != nil {
		return err
	}

	return ensureSessionDB(cmd, c)
}

// applyPersistentFlags maps the resolved persistent flags onto the config, using
// non-empty-value checks for the file/target flags and Changed() for the timeouts.
// It returns a config-class error when the TLS config path fails to load.
func applyPersistentFlags(cmd *cobra.Command, c *cfg.Config) error {
	// Accepted debt (M1 review #6, deferred by orchestrator): these
	// mappings use non-empty-value checks while the timeouts below use
	// Changed(); safe only while every resolved persistent flag keeps
	// an empty default.
	if spec, _ := cmd.Flags().GetString("spec"); spec != "" {
		c.SetSpec(spec)
	}
	if file, _ := cmd.Flags().GetString("file"); file != "" {
		c.SetFile(file)
	}
	if dbPath, _ := cmd.Flags().GetString("db"); dbPath != "" {
		c.SetDbPath(dbPath)
	} else if dbPath, _ := cmd.Flags().GetString("db-path"); dbPath != "" {
		c.SetDbPath(dbPath)
	}
	if hex, _ := cmd.Flags().GetBool("hex"); hex {
		c.SetHex(hex)
	}
	if host, _ := cmd.Flags().GetString("host"); host != "" {
		c.SetHost(host)
	}
	if port, _ := cmd.Flags().GetString("port"); port != "" {
		c.SetPort(port)
	}
	if attempts, err := cmd.Flags().GetInt("reconnect-attempts"); err == nil && cmd.Flags().Changed("reconnect-attempts") {
		c.SetReconnectAttempts(attempts)
	}
	if timeout, err := cmd.Flags().GetDuration("connect-timeout"); err == nil && cmd.Flags().Changed("connect-timeout") {
		c.SetConnectTimeout(timeout)
	}
	if timeout, err := cmd.Flags().GetDuration("total-connect-timeout"); err == nil &&
		cmd.Flags().Changed("total-connect-timeout") {
		c.SetTotalConnectTimeout(timeout)
	}
	if timeout, err := cmd.Flags().GetDuration("response-timeout"); err == nil && cmd.Flags().Changed("response-timeout") {
		c.SetResponseTimeout(timeout)
	}
	if timeout, err := cmd.Flags().GetDuration("listen-timeout"); err == nil && cmd.Flags().Changed("listen-timeout") {
		c.SetListenTimeout(timeout)
	}
	if visaID, _ := cmd.Flags().GetString("visa-station-id"); visaID != "" {
		c.SetVisaStationID(visaID)
	}
	if hdr, _ := cmd.Flags().GetString("header"); hdr != "" {
		c.SetHeader(hdr)
	}
	if tlsPath, _ := cmd.Flags().GetString("tls-config"); tlsPath != "" {
		if err := c.SetTLSConfigPath(tlsPath); err != nil {
			return &ExitConfigError{Path: tlsPath, Err: err}
		}
	}

	return nil
}

func offendingFilePath(err error) string {
	var missing *cfg.MissingFileError
	if errors.As(err, &missing) {
		return missing.Path
	}

	return ""
}

// printUsageHint writes a short v2 usage hint (naked invocation) to stdout.
// Accepted debt (M1 review #15, deferred by orchestrator): bare invocation
// and group help stay plain text even under --json; --json is documented as
// applying to data commands only.
func printUsageHint(cmd *cobra.Command) {
	w := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(w, "jiso — ISO8583 message tool and simulator")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  jiso [command]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")

	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-10s %s\n", sub.Name(), sub.Short)
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "run 'jiso tui' for interactive mode")
	_, _ = fmt.Fprintln(w, "run 'jiso --help' for details")
}

// skipGlobalSignalWatcherAnnotation marks commands that own their SIGINT/
// SIGTERM handling end-to-end (PAR-309 `serve start`): the fail-fast
// 128+signal watcher would kill them before their clean shutdown, so they
// opt out and define their own exit contract (serve start: exit 0).
const skipGlobalSignalWatcherAnnotation = "jiso/skip-global-signal-watcher"

// globalSignalWatchSkipped reports whether cmd or an ancestor owns its
// signal handling and must not get the fail-fast watcher.
// annotationSet is the value every "this command opts out" cobra annotation
// carries. Annotations are map[string]string, so the write side (a command
// declaring the skip) and the read side (the pre-run walking up the parent chain
// looking for it) only agree by spelling: a typo on the write side is a command
// that quietly does not skip, which surfaces as validation or a signal watcher
// firing on a command that asked it not to.
const annotationSet = "true"

// subCmdList is the "list" verb shared by the db, ctf, scenario and server
// command trees. It is the string operators type and the dispatcher matches, so
// the Use: entry and the arg switch are the same name, not two literals.
const subCmdList = "list"

func globalSignalWatchSkipped(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[skipGlobalSignalWatcherAnnotation] == annotationSet {
			return true
		}
	}

	return false
}

// watchSignals installs a SIGINT/SIGTERM watcher for the duration of the
// command: on signal it prints a one-line notice to stderr and exits with the
// 128+signal code (SIGINT -> 130), without a stack trace.
//
// Accepted debt (M1 review #5, partially addressed by PAR-309): the watcher
// still skips RunE defers via os.Exit; commands that need a clean shutdown
// (serve start) opt out through skipGlobalSignalWatcherAnnotation and own
// their exit code, so the race is gone for them.
func watchSignals(cmd *cobra.Command) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\nreceived signal %v, exiting\n", sig)

		code := ExitSIGINT
		if term, ok := sig.(syscall.Signal); ok {
			code = 128 + int(term)
		}
		os.Exit(code)
	}()
}

// ExecuteContext runs the root Cobra command with the given context.
func ExecuteContext(ctx context.Context) error {
	rootCmd := NewRootCmd()

	err := rootCmd.ExecuteContext(ctx)

	// UAT-01: drain the async session-DB logger before the process exits —
	// on the error path too, so rows recorded by a failing scenario or
	// stress run still land. A no-op when no --db was ever configured.
	db.FlushTransactions()

	return err
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Aliases: []string{"v"},
		Short:   "Print version information",
		Annotations: map[string]string{
			skipConfigValidationAnnotation: annotationSet,
			skipSessionDBInitAnnotation:    annotationSet,
		},
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), versionString())
		},
	}
}

// versionString renders the stamped build info (version, commit, built-at),
// one per line, for -v/--version and the version subcommand.
func versionString() string {
	return fmt.Sprintf("jiso version %s\ncommit %s\nbuilt at %s\n",
		version.Version, version.Commit, version.BuiltAt)
}
