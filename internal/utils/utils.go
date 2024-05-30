package utils

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/specs"
)

const letterBytes = "1234567890"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits
)

var src = rand.NewSource(time.Now().UnixNano())

func CreateSpecFromFile(path string) (*iso8583.MessageSpec, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer fd.Close()

	raw, err := io.ReadAll(fd)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	return specs.Builder.ImportJSON(raw)
}

func RandString(n int) string {
	if n < 0 {
		return ""
	}

	sb := strings.Builder{}
	sb.Grow(n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			sb.WriteByte(letterBytes[idx])
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return sb.String()
}

func ResponseMTI(mti string) string {
	if len(mti) < 4 {
		return ""
	}
	return mti[:2] + "1" + mti[3:]
}

func GetTrxnDateTime() string {
	currentTime := time.Now()
	// The format is defined based on the following time: Mon Jan 2 15:04:05 -0700 MST 2006
	return currentTime.Format("0102150405")
}
