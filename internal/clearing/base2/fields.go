// fields.go reads a stored transaction back into the values a CTF needs: the ISO
// field map, the merchant location, the EMV data, the transaction id, the trace
// criterion. It knows how the analyzer wrote a message as JSON; generator.go knows
// what to do with the result.
package base2

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"jiso/internal/db"
)

type isoFields struct {
	PAN              string
	PANExtension     string
	ProcessingCode   string
	Amount           int64
	SettlementAmount int64
	DateLocal        string
	TimeLocal        string
	MCC              string
	POSEntryMode     string
	CardAcceptorID   string
	TerminalID       string
	MerchantLocation string
	CurrencyCode     string
	AuthCode         string
	RRN              string
	ARN              string
	TransactionID    string
	EMVData          string
}

func extractISOFields(rec *db.EnrichedTransactionRecord) *isoFields {
	fields := &isoFields{}

	reqMap := parseFieldsMap(rec.RequestJSON)
	respMap := parseFieldsMap(derefString(rec.ResponseJSON))

	// PAN (Field 2)
	rawPAN := getStringField(reqMap, "2")
	if len(rawPAN) > 16 {
		fields.PAN = rawPAN[:16]
		fields.PANExtension = rawPAN[16:]
	} else {
		fields.PAN = rawPAN
		fields.PANExtension = "000"
	}

	// Processing Code (Field 3)
	fields.ProcessingCode = getStringField(reqMap, "3")

	// Amounts (Field 4, Field 5)
	fields.Amount = getInt64Field(reqMap, "4")
	fields.SettlementAmount = getInt64Field(reqMap, "5")

	// Dates (Field 12, Field 13)
	fields.TimeLocal = getStringField(reqMap, "12")
	fields.DateLocal = getStringField(reqMap, "13")

	// MCC (Field 18)
	fields.MCC = getStringField(reqMap, "18")

	// POS Entry Mode (Field 22)
	fields.POSEntryMode = getStringField(reqMap, "22")
	if len(fields.POSEntryMode) > 2 {
		fields.POSEntryMode = fields.POSEntryMode[:2]
	}

	// Terminal ID & Card Acceptor ID (Field 41, Field 42)
	fields.TerminalID = getStringField(reqMap, "41")
	fields.CardAcceptorID = getStringField(reqMap, "42")

	// Merchant Name / Location (Field 43 or Field 34 composite)
	fields.MerchantLocation = extractMerchantLocation(reqMap)

	// Currency (Field 49)
	fields.CurrencyCode = getStringField(reqMap, "49")

	// Auth Code (Field 38)
	authCode := getStringField(respMap, "38")
	if authCode == "" {
		authCode = getStringField(reqMap, "38")
	}
	fields.AuthCode = authCode

	// RRN (Field 37) & ARN (Field 31)
	fields.RRN = getStringField(reqMap, "37")
	fields.ARN = getStringField(reqMap, "31")

	// Transaction Identifier (Field 62.2, 62.02 or Field 62 TID)
	fields.TransactionID = extractTransactionID(reqMap, respMap)

	// EMV Chip Data (Field 55, 104, 123)
	fields.EMVData = extractEMVData(reqMap)

	return fields
}

func extractMerchantLocation(reqMap map[string]any) string {
	if loc := getStringField(reqMap, "43"); loc != "" {
		return loc
	}

	if f34, ok := reqMap["34"].(map[string]any); ok {
		if loc, ok := merchantLocationFromF34(f34); ok {
			return loc
		}
	}

	return ""
}

// merchantLocationFromF34 builds a merchant location string from a composite
// field 34, reading its C1/C3/C6 sub-fields from sub-node "02" or "2".
func merchantLocationFromF34(f34 map[string]any) (string, bool) {
	subMap, ok := f34["02"].(map[string]any)
	if !ok {
		subMap, _ = f34["2"].(map[string]any)
	}
	if subMap == nil {
		return "", false
	}

	name := getStringField(subMap, "C1")
	city := getStringField(subMap, "C3")
	country := getStringField(subMap, "C6")
	if name == "" && city == "" {
		return "", false
	}

	return fmt.Sprintf("%-25s%-13s%2s", name, city, country), true
}

func extractEMVData(reqMap map[string]any) string {
	if f55 := getStringField(reqMap, "55"); f55 != "" {
		return f55
	}
	if f104, ok := reqMap["104"].(map[string]any); ok {
		if s, ok := f104["5F"].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func parseFieldsMap(jsonStr string) map[string]any {
	if strings.TrimSpace(jsonStr) == "" {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil
	}

	if fields, ok := data["fields"].(map[string]any); ok {
		return fields
	}
	return data
}

func getStringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	val, exists := m[key]
	if !exists || val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getInt64Field(m map[string]any, key string) int64 {
	str := getStringField(m, key)
	if str == "" {
		return 0
	}
	val, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

func extractTransactionID(reqMap, respMap map[string]any) string {
	// Try Field 62 from response or request
	for _, m := range []map[string]any{respMap, reqMap} {
		if m == nil {
			continue
		}

		f62, ok := m["62"]
		if !ok || f62 == nil {
			continue
		}

		switch v := f62.(type) {
		case map[string]any:
			if id, ok := transactionIDFromMap(v); ok {
				return id
			}
		case string:
			if id, ok := transactionIDFromString(v); ok {
				return id
			}
		}
	}

	return "000000000000000"
}

// transactionIDFromMap pulls a transaction id from a composite field 62,
// preferring sub-fields 2 and 02, then a non-zero sub-field 1.
func transactionIDFromMap(v map[string]any) (string, bool) {
	for _, key := range []string{"2", "02"} {
		if sub, ok := v[key]; ok && sub != nil {
			return SanitizeNumeric(fmt.Sprintf("%v", sub), 15), true
		}
	}

	if sub1, ok := v["1"]; ok && sub1 != nil {
		if str := SanitizeNumeric(fmt.Sprintf("%v", sub1), 15); str != strings.Repeat("0", 15) {
			return str, true
		}
	}

	return "", false
}

// transactionIDFromString pulls a transaction id from a raw-string field 62:
// either the last 15 BCD digits after an 8-byte bitmap with bit 2 set, or the
// sanitized string itself when it is not all zeros.
func transactionIDFromString(v string) (string, bool) {
	// Check for 8-byte bitmap with Bit 2 (0x40) set
	if raw := []byte(v); len(raw) >= 16 && (raw[0]&0x40) != 0 {
		if bcdHex := hex.EncodeToString(raw[8:16]); len(bcdHex) >= 15 {
			tid := bcdHex[len(bcdHex)-15:]

			return SanitizeNumeric(tid, 15), true
		}
	}

	if cleaned := SanitizeNumeric(v, 15); cleaned != strings.Repeat("0", 15) {
		return cleaned, true
	}

	return "", false
}

func determineTC(procCode string) string {
	if len(procCode) >= 2 {
		prefix := procCode[:2]
		switch prefix {
		case "00":
			return TCSalesDraft // 05
		case "01", "17":
			return TCCashDisbursement // 07
		case "20":
			return TCCreditVoucher // 06
		}
	}
	return TCSalesDraft
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
