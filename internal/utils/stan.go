package utils

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type counter struct {
	value uint32
}

var (
	counterInstance *counter
	once            sync.Once
)

func GetCounter() *counter {
	once.Do(func() {
		counterInstance = &counter{value: 0}
	})
	return counterInstance
}

func (c *counter) GetStan() string {
	var val uint32
	for {
		val = atomic.AddUint32(&c.value, 1) % 1000000
		if val != 0 {
			break
		}
		// If val is 0, we decrement the counter to -1, so that the next increment will set it to 0 again.
		atomic.AddUint32(&c.value, ^uint32(0))
	}
	return fmt.Sprintf("%06d", val)
}
