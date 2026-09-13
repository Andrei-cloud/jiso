//go:build unix

package app

import (
	"errors"
	"syscall"
)

// pidAlive reports whether pid currently belongs to a live process using a
// signal-0 probe: it performs no signalling but still returns nil (we may
// signal it) or EPERM (it exists but belongs to another user). Any other
// error — most often ESRCH — means no such process.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}
