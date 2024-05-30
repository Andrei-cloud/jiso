package utils

import (
	"fmt"
	"sync/atomic"
	"time"
)

type RRN struct {
	value uint32
}

var rrnInstance *RRN

func GetRRNInstance() *RRN {
	if rrnInstance == nil {
		rrnInstance = &RRN{value: 0}
	}
	return rrnInstance
}

func (r *RRN) GetRRN() string {
	t := time.Now()
	y, d := t.Year(), t.YearDay()
	var rrn uint32
	for {
		rrn = atomic.AddUint32(&r.value, 1) % 10000000 // ensure 7 digits
		if rrn != 0 {
			break
		}
		// If rrn is 0, we decrement the counter to -1, so that the next increment will set it to 0 again.
		atomic.AddUint32(&r.value, ^uint32(0))
	}
	// generate RRN: ydddnnnnnnnn
	return fmt.Sprintf(
		"%02d%03d%07d",
		y%100,
		d,
		rrn,
	) // %02d to keep last two digits of the year, %03d to ensure 3 digits for the day of the year, %07d to ensure 7 digits for the rrn
}
