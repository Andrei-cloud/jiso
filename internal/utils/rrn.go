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
	rrn := atomic.AddUint32(&r.value, 1) % 10000000 // ensure 7 digits
	if rrn == 0 {                                   // if overflow, restart from 1
		atomic.StoreUint32(&r.value, 1)
		rrn = 1
	}
	// generate RRN: ydddnnnnnnnn
	return fmt.Sprintf(
		"%02d%03d%07d",
		y%100,
		d,
		rrn,
	) // %02d to keep last two digits of the year, %03d to ensure 3 digits for the day of the year, %08d to ensure 8 digits for the rrn
}
