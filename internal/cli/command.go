package cli

import (
	"errors"
)

// ErrExitProgram is a special error type to signal a clean exit from the CLI
var ErrExitProgram = errors.New("exit program")
