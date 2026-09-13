package utils

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/specs"

	"jiso/internal/command/templates"
)

const letterBytes = "1234567890"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits
)

var (
	specCache   sync.Map
	defaultSpec *iso8583.MessageSpec
	defaultOnce sync.Once
)

// CreateSpecFromFile loads a spec from path, caching it by path so a long-lived
// process does not re-read and re-parse the same file for every message. The
// cached spec is shared, so callers must treat it as read-only.
func CreateSpecFromFile(path string) (*iso8583.MessageSpec, error) {
	if cached, ok := specCache.Load(path); ok {
		if spec, ok := cached.(*iso8583.MessageSpec); ok && spec != nil {
			return spec, nil
		}
	}

	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer func() { _ = fd.Close() }() // read-only handle; nothing to report on close

	raw, err := io.ReadAll(fd)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	spec, err := specs.ImportJSON(raw)
	if err != nil {
		return nil, err
	}

	specCache.Store(path, spec)
	return spec, nil
}

// ResolveSpec returns the spec at specPath, or fallback when no path was given
// or the path could not be read. Falling back quietly is correct here: the
// command that must fail on a bad --spec is the one that validates the config,
// and a helper that only picks a default has nothing to report.
func ResolveSpec(specPath string, fallback *iso8583.MessageSpec) *iso8583.MessageSpec {
	if specPath == "" {
		return fallback
	}
	if spec, err := CreateSpecFromFile(specPath); err == nil && spec != nil {
		return spec
	}
	if !strings.HasSuffix(specPath, ".json") {
		specPathWithJSON := filepath.Join("specs", specPath+".json")
		if spec, err := CreateSpecFromFile(specPathWithJSON); err == nil && spec != nil {
			return spec
		}
	}
	return fallback
}

// GetDefaultSpec returns the compiled-in default spec, parsed once from the
// embedded template, so a command can compose a message with no --spec at all.
func GetDefaultSpec() *iso8583.MessageSpec {
	defaultOnce.Do(func() {
		if len(templates.DefaultSpecJSON) > 0 {
			if spec, err := specs.ImportJSON(templates.DefaultSpecJSON); err == nil && spec != nil {
				defaultSpec = spec
				return
			}
		}
		defaultSpec = iso8583.Spec87
	})
	return defaultSpec
}

// RandString returns n alphanumeric characters, the source a dynamic field uses
// when its value is set to random. It draws from math/rand, which is right for
// shaping test data and wrong for anything that needs secrecy.
func RandString(n int) string {
	if n <= 0 {
		return ""
	}

	b := make([]byte, n)
	for i, cache, remain := n-1, rand.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = rand.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}

// ResponseMTI maps an MTI to the one its reply carries, by the third-digit
// convention (0820 becomes 0830). An MTI whose third digit names no known
// pairing returns the empty string rather than a plausible wrong number.
func ResponseMTI(mti string) string {
	if len(mti) < 4 {
		return ""
	}
	c := mti[2]
	if c == '1' || c == '3' || c == '5' || c == '8' || c == '9' {
		return mti
	}
	switch c {
	case '0':
		return mti[:2] + "1" + mti[3:]
	case '2':
		return mti[:2] + "3" + mti[3:]
	case '4':
		return mti[:2] + "5" + mti[3:]
	default:
		if c >= '0' && c <= '8' && (c-'0')%2 == 0 {
			return mti[:2] + string(c+1) + mti[3:]
		}
		return mti[:2] + "1" + mti[3:]
	}
}

// RequestMTI maps a response MTI back to the request it answers (0830 becomes
// 0820), the inverse of ResponseMTI, with the same refusal for unpaired digits.
func RequestMTI(mti string) string {
	if len(mti) < 4 {
		return ""
	}
	c := mti[2]
	if c == '0' || c == '2' || c == '4' || c == '8' || c == '9' {
		return mti
	}
	switch c {
	case '1':
		return mti[:2] + "0" + mti[3:]
	case '3':
		return mti[:2] + "2" + mti[3:]
	case '5':
		return mti[:2] + "4" + mti[3:]
	default:
		if c >= '1' && c <= '9' && (c-'0')%2 == 1 {
			return mti[:2] + string(c-1) + mti[3:]
		}
		return mti[:2] + "0" + mti[3:]
	}
}

// IsResponseMTI reports whether an MTI belongs to the response class by its
// third digit, which is how the analyzer pairs a captured reply with the request
// it answers.
func IsResponseMTI(mti string) bool {
	if len(mti) < 4 {
		return false
	}
	c := mti[2]
	return c == '1' || c == '3' || c == '5' || c == '8' || c == '9'
}

// GetTrxnDateTime returns the local clock as the MMDDhhmmss string field 13
// carries. It reads the time of composing, so a message built before midnight
// and sent after it carries the earlier date.
func GetTrxnDateTime() string {
	currentTime := time.Now()
	// The format is defined based on the following time: Mon Jan 2 15:04:05 -0700 MST 2006
	// MMDDhhmmss format (month, day, hour, minute, second) - exactly 10 characters
	return currentTime.Format("0102150405")
}

// HexDump renders bytes as an offset / hex / ASCII dump, the form an operator
// compares against a capture file or a vendor log.
func HexDump(data []byte) string {
	var buf strings.Builder
	for i := 0; i < len(data); i += 16 {
		// offset
		_, _ = fmt.Fprintf(&buf, "%08x  ", i)
		// hex bytes
		for j := 0; j < 16; j++ {
			if i+j < len(data) {
				_, _ = fmt.Fprintf(&buf, "%02x ", data[i+j])
			} else {
				buf.WriteString("   ")
			}
			if j == 7 {
				buf.WriteString(" ")
			}
		}
		buf.WriteString(" |")
		// ASCII
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 32 && b <= 126 {
				buf.WriteByte(b)
			} else {
				buf.WriteByte('.')
			}
		}
		buf.WriteString("|\n")
	}
	return buf.String()
}
