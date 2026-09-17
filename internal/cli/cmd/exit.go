package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

// v2 process exit code taxonomy.
const (
	// ExitOK is the process exit code for success.
	ExitOK = 0
	// ExitError is the generic failure exit code.
	ExitError = 1
	// ExitUsage is the process exit code for usage/flag errors (v2 taxonomy).
	ExitUsage = 2
	// ExitConfig is the process exit code for config/spec/file-load errors.
	ExitConfig = 3
	// ExitTestFailure is the process exit code for failed scenario runs.
	ExitTestFailure = 4
	// ExitSIGINT is the process exit code for SIGINT (128 + 2).
	ExitSIGINT = 130
)

// ExitCodeError signals that a command finished with an explicit process exit
// code. The command is expected to have already written its message to
// cmd.ErrOrStderr; main maps this error to os.Exit without reprinting.
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("command exited with code %d", e.Code)
}

// ExitConfigError signals a config-class failure: a spec/tx/TLS file that is
// missing, unreadable, unparseable, or a config value that fails validation.
// main maps it to ExitConfig; Error names the offending file when known.
type ExitConfigError struct {
	Path string
	Err  error
}

func (e *ExitConfigError) Error() string {
	if e.Err == nil {
		if e.Path == "" {
			return "config error"
		}

		return fmt.Sprintf("config error: %s", e.Path)
	}

	if e.Path == "" || strings.Contains(e.Err.Error(), e.Path) {
		return e.Err.Error()
	}

	return fmt.Sprintf("%s: %s", e.Err, e.Path)
}

func (e *ExitConfigError) Unwrap() error {
	return e.Err
}

// ExitCodeForError maps an error returned by ExecuteContext to a process exit
// code following the v2 taxonomy.
func ExitCodeForError(err error) int {
	if err == nil {
		return ExitOK
	}

	var exitErr *ExitCodeError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	var cfgErr *ExitConfigError
	if errors.As(err, &cfgErr) {
		return ExitConfig
	}

	if errors.Is(err, context.Canceled) {
		return ExitSIGINT
	}

	if isUsageError(err) {
		return ExitUsage
	}

	return ExitError
}

func isUsageError(err error) bool {
	var notExist *pflag.NotExistError
	var valueRequired *pflag.ValueRequiredError
	var invalidValue *pflag.InvalidValueError
	if errors.As(err, &notExist) || errors.As(err, &valueRequired) || errors.As(err, &invalidValue) {
		return true
	}

	msg := err.Error()
	for _, pattern := range []string{
		"unknown flag",
		"unknown shorthand flag",
		"flag needs an argument",
		"invalid argument",
		"unknown command",
		"required flag(s)",
		"arg(s)",
		"file is required",
	} {
		if strings.Contains(msg, pattern) {
			return true
		}
	}

	return false
}
