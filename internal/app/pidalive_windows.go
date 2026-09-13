//go:build windows

package app

import (
	"math"

	"golang.org/x/sys/windows"
)

// stillActive is the sentinel GetExitCodeProcess returns for a process that has
// not yet terminated (Windows STILL_ACTIVE, 259). A reaped PID reports its real
// exit code instead, so the comparison distinguishes live from gone.
const stillActive = 259

// pidAlive reports whether pid currently belongs to a live process. Windows has
// no signal-0 probe, so we open a query-only handle: OpenProcess fails for a
// PID that does not exist, and a live process still reports STILL_ACTIVE.
func pidAlive(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return false
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}

	return code == stillActive
}
