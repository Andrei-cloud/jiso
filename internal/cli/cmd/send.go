package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	cfg "jiso/internal/config"
	"jiso/internal/utils"
)

// newSendCmd builds the PAR-301 one-shot headless send:
// connect → send → describe → disconnect, no TTY, no readline, no prompts.
//
// --wait semantics follow the REPL: the interactive `send` command blocks
// on App.Send (Service → connection.Manager.Send) until the response
// arrives or the response timeout fires, then renders the response. So
// waiting is the DEFAULT here. --wait=false is the headless-only
// fire-and-forget escape hatch (the same Service.BackgroundSend primitive
// the workers use): the message is composed, validated, packed, and
// written, and only the request composition is reported.
func newSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <tx-name>",
		Short: "One-shot connect, send, describe, and disconnect",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, _ := cmd.Flags().GetBool("wait")

			return executeSend(cmd, args[0], wait)
		},
	}

	cmd.Flags().Bool("wait", true,
		"Wait for the response and describe it (REPL parity); --wait=false sends fire-and-forget and describes the request")

	return cmd
}

// sendPlan is the machine-readable --dry-run plan of a one-shot send.
type sendPlan struct {
	DryRun bool   `json:"dry_run"`
	Tx     string `json:"tx"`
	Target string `json:"target"`
	Wait   bool   `json:"wait"`
}

func executeSend(cmd *cobra.Command, txName string, wait bool) error {
	out := output.New(cmd)
	c := cfg.GetConfig()

	// Data command: spec+tx are required; PersistentPreRunE already
	// load-validated the resolved files, this names missing paths (exit 2).
	if _, err := requireSpecAndTx(); err != nil {
		return err
	}

	host, port := strings.TrimSpace(c.GetHost()), strings.TrimSpace(c.GetPort())
	if host == "" || port == "" {
		_, _ = fmt.Fprintf(out.Err(), "Error: %s\n", missingTargetMessage("send", host, port))

		return &ExitCodeError{Code: ExitUsage}
	}

	a, err := app.New(c)
	if err != nil {
		// Defensive translation: PersistentPreRunE already exit-3s on
		// unloadable spec/tx, but never downgrade app's config class to
		// a generic failure.
		var appCfgErr *app.ConfigError
		if errors.As(err, &appCfgErr) {
			return &ExitConfigError{Path: appCfgErr.Path, Err: err}
		}

		return err
	}
	defer func() { _ = a.Close() }()

	// Headless contract: no interactive debug progress (hex dumps,
	// stabilize notices) on the one-shot path.
	a.Service().SetDebugMode(false)

	if out.DryRun() {
		plan := &sendPlan{DryRun: true, Tx: txName, Target: host + ":" + port, Wait: wait}

		return out.Data(plan, func() {
			out.Noticef("dry-run: would connect to %s:%s and send %s (wait=%t); nothing will be sent", host, port, txName, wait)
		})
	}

	if err := a.Connect(); err != nil {
		// Runtime network failure: exit 1 naming the target, not exit 3.
		return fmt.Errorf("failed to connect to %s:%s: %w", host, port, err)
	}
	defer func() {
		if err := a.Disconnect(); err != nil {
			_, _ = fmt.Fprintf(out.Err(), "Warning: disconnect: %v\n", err)
		}
	}()

	if !wait {
		return sendFireAndForget(out, a, txName, host, port)
	}

	result, err := a.Send(txName)
	if result != nil {
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintln(out.Err(), warning)
		}
	}
	if err != nil {
		return err
	}

	return out.Data(result, func() { printSendHuman(out, txName, host, port, result) })
}

// sendFireAndForget composes, validates, and writes txName without waiting
// for a response, then reports the request composition as a SendResult.
func sendFireAndForget(out *output.Renderer, a *app.App, txName, host, port string) error {
	tc := a.Transactions()

	msg, err := tc.Compose(txName)
	if err != nil {
		return err
	}

	if err := app.ValidateMessage(msg); err != nil {
		return fmt.Errorf("message validation failed: %w", err)
	}

	rawMsg, err := msg.Pack()
	if err != nil {
		return err
	}

	startTime := time.Now()

	_, err = a.Service().BackgroundSend(msg)
	elapsed := time.Since(startTime)

	success := err == nil
	tc.LogTransaction(txName, success)
	app.LogTransactionToDB(cfg.GetConfig().GetSessionID(), txName, msg, nil, int(elapsed.Milliseconds()), success)

	if err != nil {
		return fmt.Errorf("failed to send to %s:%s: %w", host, port, err)
	}

	result := &app.SendResult{
		Description: renderRequestOnly(msg, elapsed),
		Hex:         utils.HexDump(rawMsg),
		Elapsed:     elapsed,
	}

	return out.Data(result, func() { printSendFireHuman(out, txName, host, port, msg, result) })
}

// renderRequestOnly mirrors app's request/response renderer for the
// request-only (fire-and-forget) view.
func renderRequestOnly(request *iso8583.Message, elapsed time.Duration) string {
	var buf bytes.Buffer

	_, _ = fmt.Fprintln(&buf, "--- REQUEST ---")
	_ = utils.Describe(request, &buf, iso8583.DoNotFilterFields()...)

	_, _ = fmt.Fprintf(&buf, "\nSent at: %s\n", elapsed.Round(time.Millisecond))

	return buf.String()
}

// fieldValue returns the rendered value of response field id, "" when absent.
func fieldValue(result *app.SendResult, id string) string {
	for _, f := range result.Fields {
		if f.ID == id {
			return f.Value
		}
	}

	return ""
}

func printSendHuman(out *output.Renderer, txName, host, port string, result *app.SendResult) {
	w := out.Out()

	_, _ = fmt.Fprintf(w, "Transaction: %s\n", txName)
	_, _ = fmt.Fprintf(w, "Target:      %s:%s\n", host, port)
	_, _ = fmt.Fprintf(w, "Elapsed:     %s\n", result.Elapsed.Round(time.Millisecond))

	if mti := fieldValue(result, "0"); mti != "" {
		line := fmt.Sprintf("Response:    MTI %s", mti)
		if rc := fieldValue(result, "39"); rc != "" {
			line += fmt.Sprintf("  Response Code: %s", rc)
		}

		_, _ = fmt.Fprintln(w, line)
	}

	if cfg.GetConfig().GetHex() {
		if result.Hex != "" {
			_, _ = fmt.Fprintf(w, "Request HEX:\n%s", result.Hex)
		}
		if result.ResponseHex != "" {
			_, _ = fmt.Fprintf(w, "Response HEX:\n%s", result.ResponseHex)
		}
	}

	if result.Description != "" {
		_, _ = fmt.Fprint(w, result.Description)
	}
}

func printSendFireHuman(out *output.Renderer, txName, host, port string, msg *iso8583.Message, result *app.SendResult) {
	w := out.Out()

	mti, _ := msg.GetMTI()

	_, _ = fmt.Fprintf(w, "Transaction: %s (fire-and-forget, no response awaited)\n", txName)
	_, _ = fmt.Fprintf(w, "Target:      %s:%s\n", host, port)
	_, _ = fmt.Fprintf(w, "Request:     MTI %s\n", mti)
	_, _ = fmt.Fprintf(w, "Elapsed:     %s\n", result.Elapsed.Round(time.Millisecond))

	if cfg.GetConfig().GetHex() && result.Hex != "" {
		_, _ = fmt.Fprintf(w, "Request HEX:\n%s", result.Hex)
	}

	if result.Description != "" {
		_, _ = fmt.Fprint(w, result.Description)
	}
}

// missingTargetMessage names the missing pieces of a target for commands
// that address host:port (send, connect check) with the usual flag>env>
// config resolution hints.
func missingTargetMessage(action, host, port string) string {
	switch {
	case host == "" && port == "":
		return "cannot " + action + ": host and port are not configured " +
			"(use --host/-H and --port/-p, JISO_HOST/JISO_PORT, or the user config file)"
	case host == "":
		return "cannot " + action + ": host is not configured " +
			"(use --host/-H, JISO_HOST, or the user config file)"
	default:
		return "cannot " + action + ": port is not configured " +
			"(use --port/-p, JISO_PORT, or the user config file)"
	}
}
