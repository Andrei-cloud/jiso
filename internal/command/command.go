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
}

type WorkerController interface {
	StartWorker(name string, command BgCommand, interval time.Duration)
}
