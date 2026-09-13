package cmd

import (
	"errors"
	"fmt"

	"github.com/moov-io/iso8583"
	"github.com/spf13/cobra"

	"jiso/internal/cli/output"
	cfg "jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// skipConfigValidationAnnotation marks commands that must never fail on an
// unloadable --spec/--file (CLI-105): the REPL picks specs per session,
// stubs consume nothing yet, and version must always run even with a broken
// config.
const skipConfigValidationAnnotation = "jiso/skip-config-validation"

// configFileValidationSkipped reports whether cmd or an ancestor is exempt
// from spec/tx load-validation: annotated commands (repl, stubs, version)
// plus cobra's built-in completion and help commands. --help/--version
// already win before any hook runs.
func configFileValidationSkipped(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[skipConfigValidationAnnotation] == annotationSet {
			return true
		}
		if name := c.Name(); name == "completion" || name == "help" {
			return true
		}
	}

	return false
}

// loadValidateConfigFiles load-validates the resolved spec and transaction
// files with the same loaders the commands use, so a malformed file fails
// early with the exit-3 config error naming the file instead of surfacing
// later — or being silently ignored by inspect/server/analyze fallbacks.
// utils.CreateSpecFromFile caches the parsed spec by path, so commands reuse
// this parse instead of re-reading the file.
func loadValidateConfigFiles(c *cfg.Config) error {
	specPath := c.GetSpec()
	if specPath == "" {
		return nil
	}

	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return &ExitConfigError{Path: specPath, Err: fmt.Errorf("failed to load spec: %w", err)}
	}

	txPath := c.GetFile()
	if txPath == "" {
		return nil
	}

	if _, err := transactions.NewTransactionCollection(txPath, spec); err != nil {
		return &ExitConfigError{Path: txPath, Err: err}
	}

	return nil
}

// configuredSpecAndTx returns the resolved spec and transaction collection
// for the command about to run. PersistentPreRunE has already load-validated
// both files and utils.CreateSpecFromFile caches the parsed spec by path, so
// this reuses the cached parse instead of re-loading, and surfaces — never
// swallows — any load error (M1 review #24). Unset paths yield nil values and
// no error; commands decide whether nil is acceptable for their mode.
func configuredSpecAndTx() (*iso8583.MessageSpec, *transactions.TransactionCollection, error) {
	c := cfg.GetConfig()
	specPath, txPath := c.GetSpec(), c.GetFile()

	var spec *iso8583.MessageSpec
	if specPath != "" {
		s, err := utils.CreateSpecFromFile(specPath)
		if err != nil {
			return nil, nil, &ExitConfigError{Path: specPath, Err: fmt.Errorf("failed to load spec: %w", err)}
		}
		spec = s
	}

	if txPath == "" || spec == nil {
		return spec, nil, nil
	}

	tc, err := transactions.NewTransactionCollection(txPath, spec)
	if err != nil {
		return nil, nil, &ExitConfigError{Path: txPath, Err: err}
	}

	return spec, tc, nil
}

// dryRunPlan is the machine-readable --dry-run plan of a mutating file
// command: {"dry_run": true, "action": ..., "path": ...}.
type dryRunPlan struct {
	DryRun bool   `json:"dry_run"`
	Action string `json:"action"`
	Path   string `json:"path"`
}

// previewFileWrite is the shared --dry-run handler for commands whose only
// side effect is writing a generated file (spec/tx init, analyze): it prints
// the plan — pure JSON under --json — and writes nothing (M1 review #3).
func previewFileWrite(out *output.Renderer, action, path string) error {
	plan := &dryRunPlan{DryRun: true, Action: action, Path: path}

	return out.Data(plan, func() {
		out.Noticef("dry-run: would write %s to %s", action, path)
	})
}

// requireSpecAndTx is configuredSpecAndTx for data commands that cannot run
// without both files: a missing path is the existing usage-class error.
func requireSpecAndTx() (*transactions.TransactionCollection, error) {
	if cfg.GetConfig().GetSpec() == "" {
		return nil, errors.New("spec file is required (use -s or --spec)")
	}
	if cfg.GetConfig().GetFile() == "" {
		return nil, errors.New("transaction file is required (use -f or --file)")
	}

	_, tc, err := configuredSpecAndTx()

	return tc, err
}
