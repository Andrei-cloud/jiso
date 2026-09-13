package app

import (
	"errors"
	"fmt"
	"strings"
)

// Minimal error types mirroring the v2 exit taxonomy
// (internal/cli/cmd/exit.go). internal/app must not import
// internal/cli/cmd, so the config-class type is duplicated here; frontends
// translate these into process exit codes.

// ErrClosed is returned by App methods after Close.
var ErrClosed = errors.New("application is closed")

// ErrWorkerNotFound is the sentinel behind WorkerStop/StressStop/
// StressSummaryByID for an unknown worker ID. The concrete error keeps the
// legacy "worker '<id>' not found" text verbatim (errors.Is matches the
// sentinel, the text stays shim-compatible).
var ErrWorkerNotFound = errors.New("worker not found")

// WorkerNotFoundError reports an unknown worker ID. Its Error text is the
// legacy CLI string; Unwrap links it to ErrWorkerNotFound.
type WorkerNotFoundError struct {
	ID string
}

func (e *WorkerNotFoundError) Error() string {
	return fmt.Sprintf("worker '%s' not found", e.ID)
}

func (e *WorkerNotFoundError) Unwrap() error {
	return ErrWorkerNotFound
}

// ErrTxNotFound is the §I façade sentinel behind ReviewTx when the requested
// transaction row is absent from the session database (mirrors
// db.ErrTransactionNotFound at the façade boundary).
var ErrTxNotFound = errors.New("transaction not found")

// ConfigError signals a config-class failure: a spec/tx/TLS file that is
// missing, unreadable, unparseable, or a config value that fails
// validation. Error names the offending file when known.
type ConfigError struct {
	Path string
	Err  error
}

func (e *ConfigError) Error() string {
	if e.Path == "" || strings.Contains(e.Err.Error(), e.Path) {
		return e.Err.Error()
	}

	return fmt.Sprintf("%s: %s", e.Err, e.Path)
}

func (e *ConfigError) Unwrap() error {
	return e.Err
}

// NotConnectedError signals that a send was attempted while no connection
// to Address is online.
type NotConnectedError struct {
	Address string
}

func (e *NotConnectedError) Error() string {
	return fmt.Sprintf("not connected to target host %s. Please connect first using 'connect'", e.Address)
}
