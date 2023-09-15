package command

import "time"

type Command interface {
	Name() string
	Synopsis() string
	Execute() error
}

type BgCommand interface {
	Name() string
	Synopsis() string
	ExecuteBackground(string) error
	StartClock()
	Duration() time.Duration
	Stats() int
	MeanExecutionTime() time.Duration
	StandardDeviation() time.Duration
	ResponseCodes() map[string]uint64
}

type WorkerController interface {
	StartWorker(name string, command BgCommand, interval time.Duration)
}
