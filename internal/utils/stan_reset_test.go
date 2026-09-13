package utils

import (
	"sync"
	"time"
)

// resetCounterForTest tears the singleton down so a test can simulate a
// process restart: the old worker is signalled (its flush lands on disk,
// which is what the restart test asserts against), the quit/done channels
// are replaced so a fresh GetCounter starts a clean worker, and the
// persistence dir is preserved.
func resetCounterForTest() {
	if quitChan != nil {
		select {
		case <-quitChan: // already closed
		default:
			close(quitChan)
		}

		if persistDone != nil {
			select {
			case <-persistDone:
			case <-time.After(2 * time.Second):
			}
		}
	}

	once = sync.Once{}
	counterInstance = nil
	persistChan = nil
	quitChan = nil
	persistDone = nil
}
