package db

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

// ReconstructedMessage is one message recovered from a capture: the decoded
// message when the spec could read it, its hex, and the describe text the review
// pane prints. IsRawFallback and ParseError carry the case where the decode did
// not work, so the operator sees the bytes and the reason instead of an empty
// message that looks like there was no traffic.
type ReconstructedMessage struct {
	Message       *iso8583.Message
	HEX           string
	DescribeText  string
	IsRawFallback bool
	ParseError    string
}

// Reconstruct rebuilds the review view of a stored transaction. The recorded
// JSON is the source of truth for the tree: its values were extracted from
// the message itself, while specPath names the session config — possibly a
// different dialect than the record spoke (a stamped Visa exchange recorded
// while the session was dialled on flex). The HEX pane shows the recorded
// wire bytes when present, and only for legacy rows that carry none falls
// back to repacking under the resolved spec.
func Reconstruct(jsonStr, rawHexStr, specPath string) (*ReconstructedMessage, error) {
	spec := utils.ResolveSpec(specPath, utils.GetDefaultSpec())

	// 1. Recorded JSON renders the tree as stored.
	if strings.TrimSpace(jsonStr) != "" {
		res, err := reconstructFromJSON(jsonStr, rawHexStr, spec)
		if err == nil {
			return res, nil
		}
	}

	// 2. Raw HEX (recorded or legacy formatted dump) is the only payload.
	if strings.TrimSpace(rawHexStr) != "" {
		return reconstructFromHEX(rawHexStr, spec)
	}

	return &ReconstructedMessage{
		DescribeText: "(No message data available)",
	}, nil
}

func reconstructFromJSON(jsonStr, rawHexStr string, spec *iso8583.MessageSpec) (*ReconstructedMessage, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	mti, _ := data["mti"].(string)
	fieldsMap, _ := data["fields"].(map[string]any)

	var buf bytes.Buffer
	utils.DescribeStoredJSON(&buf, mti, fieldsMap, spec)
	res := &ReconstructedMessage{DescribeText: buf.String()}

	// Recorded wire bytes are the honest HEX pane.
	if raw, ok := parseStoredHex(rawHexStr); ok {
		res.HEX = utils.HexDump(raw)
		return res, nil
	}

	// Legacy rows stored no wire hex: repack the values to render the pane.
	// The resolved spec may not be the dialect the message spoke; say so
	// rather than dropping the (complete) tree with it.
	packed, err := repackRecord(mti, fieldsMap, spec)
	if err != nil {
		res.HEX = fmt.Sprintf("(Repack with spec %s failed: %v)", specDisplayName(spec), err)
		res.ParseError = err.Error()
		return res, nil
	}
	res.HEX = utils.HexDump(packed)

	return res, nil
}

// repackRecord builds a message from recorded JSON values and packs it,
// the pre-JSON-tree behaviour, kept for rows that carry no wire hex.
func repackRecord(mti string, fieldsMap map[string]any, spec *iso8583.MessageSpec) ([]byte, error) {
	msg := iso8583.NewMessage(spec)
	if mti != "" {
		msg.MTI(mti)
	}
	for k, v := range fieldsMap {
		fieldID, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		switch val := v.(type) {
		case map[string]any:
			if err := utils.SetCompositeFieldValue(msg, spec, fieldID, val); err != nil {
				continue
			}
		default:
			if err := msg.Field(fieldID, fmt.Sprintf("%v", v)); err != nil {
				continue
			}
		}
	}

	return msg.Pack()
}

// parseStoredHex decodes a stored hex column: plain hex (what LogTransactionToDB
// writes since wire bytes became always-recorded) or a legacy formatted
// HexDump blob (offset / hex / ASCII columns, decoded from the hex column only).
func parseStoredHex(s string) ([]byte, bool) {
	if strings.TrimSpace(s) == "" {
		return nil, false
	}
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, s)
	if b, err := hex.DecodeString(clean); err == nil && len(b) > 0 {
		return b, true
	}

	var out []byte
	for _, line := range strings.Split(s, "\n") {
		if i := strings.IndexByte(line, '|'); i >= 0 {
			line = line[:i] // drop the ASCII gutter of a formatted dump
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue // blank or offset-only line
		}
		hexPart := strings.Join(parts[1:], "") // drop the offset column
		if len(hexPart)%2 != 0 {
			return nil, false
		}
		b, err := hex.DecodeString(hexPart)
		if err != nil {
			return nil, false
		}
		out = append(out, b...)
	}
	if len(out) == 0 {
		return nil, false
	}

	return out, true
}

// specDisplayName names a spec for error text, mirroring Describe's header rule.
func specDisplayName(spec *iso8583.MessageSpec) string {
	if spec != nil && spec.Name != "" {
		return spec.Name
	}

	return "ISO 8583"
}

func reconstructFromHEX(rawHexStr string, spec *iso8583.MessageSpec) (*ReconstructedMessage, error) {
	rawBytes, decoded := parseStoredHex(rawHexStr)
	if !decoded {
		// Not hex at all: keep the old behaviour of reviewing the bytes as text.
		rawBytes = []byte(rawHexStr)
	}

	hexDumpStr := utils.HexDump(rawBytes)

	msg := iso8583.NewMessage(spec)
	err := msg.Unpack(rawBytes)
	if err == nil {
		var buf bytes.Buffer
		_ = utils.Describe(msg, &buf, iso8583.DoNotFilterFields()...)
		return &ReconstructedMessage{
			Message:       msg,
			HEX:           hexDumpStr,
			DescribeText:  buf.String(),
			IsRawFallback: true,
		}, nil
	}

	return &ReconstructedMessage{
		HEX:           hexDumpStr,
		DescribeText:  fmt.Sprintf("(RAW HEX fallback message - could not unpack with spec: %v)", err),
		IsRawFallback: true,
		ParseError:    err.Error(),
	}, nil
}
