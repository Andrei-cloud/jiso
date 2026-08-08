package cmd

import (
	"context"
	"time"

	cfg "jiso/internal/config"

	"github.com/spf13/cobra"
)

// REPLRunner defines the function signature to launch the interactive REPL.
type REPLRunner func(ctx context.Context) error

var defaultREPLRunner REPLRunner

// SetREPLRunner sets the function used to start interactive REPL mode.
func SetREPLRunner(runner REPLRunner) {
	defaultREPLRunner = runner
}

// NewRootCmd creates and configures the root Cobra command for jiso.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "jiso [command]",
		Short:         "jiso ISO8583 message tool and simulator",
		Long:          `jiso is a powerful CLI tool and interactive REPL for inspecting, generating, and simulating ISO8583 messages and test scenarios.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if defaultREPLRunner != nil {
				return defaultREPLRunner(cmd.Context())
			}
			return nil
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			c := cfg.GetConfig()
			c.EnsureSessionId()
			c.EnsureDefaults()

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
			if timeout, err := cmd.Flags().GetDuration("total-connect-timeout"); err == nil && cmd.Flags().Changed("total-connect-timeout") {
				c.SetTotalConnectTimeout(timeout)
			}
			if timeout, err := cmd.Flags().GetDuration("response-timeout"); err == nil && cmd.Flags().Changed("response-timeout") {
				c.SetResponseTimeout(timeout)
			}
			if visaID, _ := cmd.Flags().GetString("visa-station-id"); visaID != "" {
				c.SetVisaStationId(visaID)
			}

			return c.Validate()
		},
	}

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
	pflags.IntP("reconnect-attempts", "r", 3, "Number of reconnection attempts on failure")
	pflags.Duration("connect-timeout", 5*time.Second, "Timeout for individual connection attempts")
	pflags.Duration("total-connect-timeout", 10*time.Second, "Total timeout for connection establishment")
	pflags.Duration("response-timeout", 5*time.Second, "Timeout waiting for async message responses")
	pflags.String("visa-station-id", "", "VISA Local Station ID (6-digit hex or decimal)")

	// Register subcommands
	rootCmd.AddCommand(newSpecCmd())
	rootCmd.AddCommand(newTxCmd())
	rootCmd.AddCommand(newScenarioCmd())
	rootCmd.AddCommand(newServerCmd())
	rootCmd.AddCommand(newAnalyzeCmd())
	rootCmd.AddCommand(newREPLCmd())
	rootCmd.AddCommand(newVersionCmd())

	return rootCmd
}

// ExecuteContext runs the root Cobra command with the given context.
func ExecuteContext(ctx context.Context) error {
	rootCmd := NewRootCmd()
	return rootCmd.ExecuteContext(ctx)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Aliases: []string{"v"},
		Short:   "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("jiso version v1.5.0\n")
		},
	}
}
