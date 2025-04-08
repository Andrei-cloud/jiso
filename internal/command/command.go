package command

import (
	"time"
)

// Command interface defines methods required for all CLI commands
type Command interface {
	// Name returns the command name used to run it
	Name() string

	// Synopsis returns a short description of the command
	Synopsis() string

	// Execute runs the command with given arguments
	Execute() error
}

// BgCommand interface defines methods required for background commands
type BgCommand interface {
	Command

	// ExecuteBackground runs the command in the background
	ExecuteBackground(name string) error

	// StartClock begins timing for statistics collection
	StartClock()

	// Stats returns the number of executions completed
	Stats() int

	// Duration returns the total elapsed time
	Duration() time.Duration

	// MeanExecutionTime returns average execution time
	MeanExecutionTime() time.Duration

	// StandardDeviation returns standard deviation of execution times
	StandardDeviation() time.Duration

	// ResponseCodes returns a map of response codes and their frequencies
	ResponseCodes() map[string]uint64
}
