package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
	"jiso/internal/transactions"
)

// newInspectCmd builds `jiso inspect <tx-name>`: the headless replacement for
// the REPL `info` command (PAR-302).
//
// Parity contract with REPL info — both render the SAME composition, built
// through the shared app.InfoView builders (internal/app/result_info.go):
//
//	Repository.Info (declared fields) → Repository.Compose (dataset
//	interpolation via {{data.*}} placeholder resolution happens inside
//	Compose, so both paths interpolate identically) → Pack → HEX dump →
//	utils.Describe parsed field view.
//
// The only intentional difference is presentation policy: info is
// interactive (survey picker, notices on stderr), inspect is headless
// (UAT-04: the name is ALWAYS required — a missing one is a usage error
// exit 2, never a survey prompt, under --json stdout stays empty; notices
// via output.Noticef so --quiet/--json keep stdout pure). Data is
// identical by construction: both sides build the view with
// app.NewInfoView / app.NewInfoViewFromComposedMessage.
func newInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect [tx-name]",
		Short: "Show composed message, packed hex, and parsed fields for a transaction",
		Args:  cobra.MaximumNArgs(1),
		RunE:  executeInspect,
	}
}

func executeInspect(cmd *cobra.Command, args []string) error {
	out := output.New(cmd)

	// Cached, load-validated spec/collection from PersistentPreRunE; load
	// errors surface instead of being swallowed (M1 review #24).
	_, tc, err := configuredSpecAndTx()
	if err != nil {
		return err
	}

	var tcRepo transactions.Repository
	if tc != nil {
		tcRepo = tc
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	if name == "" {
		// UAT-04: the v2 cobra path never prompts (E1 golden rule). The
		// survey picker stays in the REPL command layer (the wizard path
		// here emitted ANSI prompt bytes on stdout and died with a
		// runtime-1 "EOF" when piped); a missing name is a plain usage
		// error naming the missing argument, stdout clean in both human
		// and --json mode.
		_, _ = fmt.Fprintf(out.Err(), "Error: transaction name is required: jiso inspect <tx-name>\n")

		return &ExitCodeError{Code: ExitUsage}
	}

	if err := cmdpkg.VerifyTx(tcRepo); err != nil {
		return err
	}

	info, err := tcRepo.Info(name)
	if err != nil {
		return err
	}
	txName, description, fieldsJSON := info.Name, info.Description, info.FieldsJSON

	view := app.NewInfoView(txName, description, fieldsJSON)

	sampleMsg, composeErr := tcRepo.Compose(txName)
	if composeErr != nil {
		view.SetComposeError(composeErr)

		return out.Data(view, func() { printInspectHuman(out, view) })
	}

	view = app.NewInfoViewFromComposedMessage(txName, description, fieldsJSON, sampleMsg)

	return out.Data(view, func() { printInspectHuman(out, view) })
}

const inspectComposeNotice = "Composing a sample message with dataset interpolation if dataset_name is configured..."

func printInspectHuman(out *output.Renderer, v *app.InfoView) {
	w := out.Out()

	_, _ = fmt.Fprintf(w, "Name: %s\n", v.Name)
	if v.Description != "" {
		_, _ = fmt.Fprintf(w, "Description: %s\n", v.Description)
	}
	_, _ = fmt.Fprintf(w, "MTI: %s\n", v.MTI)
	_, _ = fmt.Fprintf(w, "Processing Code: %s\n", v.ProcessingCode)

	_, _ = fmt.Fprintln(w, "Message:")
	_, _ = fmt.Fprint(w, cmdpkg.FormatFieldsForInfo(v.Fields))

	out.Noticef("%s", inspectComposeNotice)

	if v.ComposeError != "" {
		_, _ = fmt.Fprintf(w, "Compose error: %s\n", v.ComposeError)

		return
	}

	_, _ = fmt.Fprintln(w, "\nSample Message Build")
	_, _ = fmt.Fprintln(w, "--------------------")

	if v.PackError != "" {
		_, _ = fmt.Fprintf(w, "Pack error: %s\n", v.PackError)
	} else {
		_, _ = fmt.Fprintf(w, "Packed bytes: %d\n", v.PackedBytes)
		_, _ = fmt.Fprintf(w, "\nPacked HEX dump:\n%s", v.PackedHEX)
	}

	if v.ParsedMessage != "" {
		_, _ = fmt.Fprintf(w, "\nParsed field view:\n%s", v.ParsedMessage)
	}
}
