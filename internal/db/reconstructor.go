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

type ReconstructedMessage struct {
	Message       *iso8583.Message
	HEX           string
	DescribeText  string
	IsRawFallback bool
	ParseError    string
}

// Reconstruct reconstructs an ISO 8583 message from stored JSON or raw HEX fallback,
// packing it using the provided specification path (or default spec) to generate
// raw HEX output and parsed Describe tree text.
func Reconstruct(jsonStr, rawHexStr, specPath string) (*ReconstructedMessage, error) {
	spec := utils.ResolveSpec(specPath, utils.GetDefaultSpec())

	// 1. Try reconstructing from JSON if available
	if strings.TrimSpace(jsonStr) != "" {
		res, err := reconstructFromJSON(jsonStr, spec)
		if err == nil {
			return res, nil
		}
	}

	// 2. Try reconstruction from raw HEX fallback if available
	if strings.TrimSpace(rawHexStr) != "" {
		return reconstructFromHEX(rawHexStr, spec)
	}

	return &ReconstructedMessage{
		DescribeText: "(No message data available)",
	}, nil
}

func reconstructFromJSON(jsonStr string, spec *iso8583.MessageSpec) (*ReconstructedMessage, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	mti, _ := data["mti"].(string)
	fieldsMap, _ := data["fields"].(map[string]interface{})

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
		case map[string]interface{}:
			_ = utils.SetCompositeFieldValue(msg, spec, fieldID, val)
		default:
			valStr := fmt.Sprintf("%v", v)
			_ = msg.Field(fieldID, valStr)
		}
	}


	packedBytes, err := msg.Pack()
	hexStr := ""
	if err == nil {
		hexStr = utils.HexDump(packedBytes)
	} else {
		hexStr = fmt.Sprintf("(Failed to pack message bytes: %v)", err)
	}

	var buf bytes.Buffer
	if err := utils.Describe(msg, &buf, iso8583.DoNotFilterFields()...); err != nil {
		buf.WriteString(fmt.Sprintf("\n(Describe error: %v)", err))
	}

	return &ReconstructedMessage{
		Message:       msg,
		HEX:           hexStr,
		DescribeText:  buf.String(),
		IsRawFallback: false,
	}, nil
}

func reconstructFromHEX(rawHexStr string, spec *iso8583.MessageSpec) (*ReconstructedMessage, error) {
	cleanHex := strings.ReplaceAll(rawHexStr, " ", "")
	cleanHex = strings.ReplaceAll(cleanHex, "\n", "")
	cleanHex = strings.ReplaceAll(cleanHex, "\r", "")

	rawBytes, err := hex.DecodeString(cleanHex)
	if err != nil {
		// If it's formatted bytes string
		rawBytes = []byte(rawHexStr)
	}

	hexDumpStr := utils.HexDump(rawBytes)

	msg := iso8583.NewMessage(spec)
	err = msg.Unpack(rawBytes)
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
