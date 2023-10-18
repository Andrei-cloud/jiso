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
	val := atomic.AddUint32(&c.value, 1) % 1000000
	if val == 0 {
		atomic.StoreUint32(&c.value, 1)
		val = 1
	}
	return fmt.Sprintf("%06d", val)
}
