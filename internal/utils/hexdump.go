package utils

import (
	"encoding/hex"
	"strings"
)

// StandardHexDump renders data in the classic hexdump layout: 8-hex-digit
// offset, 16 bytes as hex pairs (two groups of 8), and the printable-ASCII
// gutter (dots for non-printables) — the same format hex.Dump produces.
func StandardHexDump(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(hex.Dump(data), "\n"), "\n")
}
