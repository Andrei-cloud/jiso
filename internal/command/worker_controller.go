package command

import (
	"time"
)

// WorkerController defines the interface for managing background workers
type WorkerController interface {
	// StartWorker starts a new worker with the given parameters
	StartWorker(name string, count int, interval time.Duration) (string, error)

	// StopWorker stops a worker by its ID
	StopWorker(id string) error

	// StopAllWorkers stops all running workers
	StopAllWorkers() error

	// GetWorkerStats returns statistics for all workers
	GetWorkerStats() map[string]interface{}
}
