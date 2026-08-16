package base2

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"jiso/internal/db"
)

// GeneratorOptions contains configuration options for Base II CTF generation.
type GeneratorOptions struct {
	BINFilter     string
	CIB           string
	AcquirerBIN   string
	BatchNumber   int
	CenterBatchID string
	CenterFileID  string
	GenerationTime time.Time
}

// CTFResult contains generated CTF records and metadata statistics.
type CTFResult struct {
	Records              []*Record
	RawContent           []byte
	MonetaryTxCount      int
	TotalTCRCount        int
	DestinationAmountSum int64
	SourceAmountSum      int64
	BatchNumber          int
	CIB                  string
	ProcessingDate       string
	SkippedCount         int
}

// GenerateCTF converts approved Visa transactions for a session into a Base II CTF file.
func GenerateCTF(session *db.SessionRecord, txs []*db.EnrichedTransactionRecord, opts GeneratorOptions) (*CTFResult, error) {
	if opts.GenerationTime.IsZero() {
		opts.GenerationTime = time.Now().UTC()
	}
	if opts.BatchNumber <= 0 {
		opts.BatchNumber = 1
	}
	if opts.CIB == "" {
		if opts.AcquirerBIN != "" && len(opts.AcquirerBIN) >= 6 {
			opts.CIB = opts.AcquirerBIN[:6]
		} else {
			opts.CIB = "400129"
		}
	}
	if opts.AcquirerBIN == "" {
		opts.AcquirerBIN = opts.CIB
	}
	if opts.CenterBatchID == "" {
		opts.CenterBatchID = fmt.Sprintf("%02d%02d%04d", opts.GenerationTime.Month(), opts.GenerationTime.Day(), opts.GenerationTime.Hour()*100+opts.GenerationTime.Minute())
	}
	if opts.CenterFileID == "" {
		opts.CenterFileID = opts.CenterBatchID
	}

	cleanBINFilter := strings.TrimSpace(opts.BINFilter)

	var records []*Record
	var monetaryCount int
	var destAmountSum int64
	var sourceAmountSum int64
	var skippedCount int

	for i, tx := range txs {
		if tx == nil {
			continue
		}

		// Only approved transactions (success = true and response_code == "00" or "000")
		if !tx.Success || (tx.ResponseCode != "00" && tx.ResponseCode != "000" && tx.ResponseCode != "") {
			skippedCount++
			continue
		}

		parsedFields := extractISOFields(tx)

		pan := parsedFields.PAN
		if cleanBINFilter != "" && !strings.HasPrefix(pan, cleanBINFilter) {
			skippedCount++
			continue
		}

		txCode := determineTC(parsedFields.ProcessingCode)
		txTime := tx.Timestamp
		if txTime.IsZero() {
			txTime = opts.GenerationTime
		}

		// Date fields
		purchaseDate := fmt.Sprintf("%02d%02d", txTime.Month(), txTime.Day())
		if len(parsedFields.DateLocal) == 4 {
			purchaseDate = parsedFields.DateLocal
		}
		centralProcDate := fmt.Sprintf("%d%03d", txTime.Year()%10, txTime.YearDay())

		// Sequence & ARN
		seq := int64(i + 1)
		arn := parsedFields.ARN
		if arn == "" || len(arn) < 23 {
			arn = GenerateARN(opts.AcquirerBIN, txTime, seq)
		}

		// Amounts
		destAmt := parsedFields.Amount
		sourceAmt := parsedFields.Amount
		if destAmt == 0 && parsedFields.SettlementAmount > 0 {
			destAmt = parsedFields.SettlementAmount
		}

		// Merchant Info parsing (Field 43: 25 Name, 13 City, 2 Country/State / 3-alpha Country)
		mLoc := ParseMerchantLocation(parsedFields.MerchantLocation, parsedFields.CurrencyCode)

		// 1. TCR 0: Draft Data
		tcr0 := &TCR0DraftData{
			TC:                    txCode,
			TCQ:                   "0",
			TCRSequence:           TCR0,
			AccountNumber:         parsedFields.PAN,
			AccountExtension:      parsedFields.PANExtension,
			FloorLimitIndicator:   " ",
			CRBIndicator:          " ",
			ARN:                   arn,
			AcquirerBusinessID:    opts.AcquirerBIN,
			PurchaseDate:          purchaseDate,
			DestinationAmount:     destAmt,
			DestinationCurrency:   defaultString(parsedFields.CurrencyCode, "840"),
			SourceAmount:          sourceAmt,
			SourceCurrency:        defaultString(parsedFields.CurrencyCode, "840"),
			MerchantName:          mLoc.MerchantName,
			MerchantCity:          mLoc.MerchantCity,
			MerchantCountry:       mLoc.CountryNumeric,
			MCC:                   defaultString(parsedFields.MCC, "5999"),
			MerchantZIP:           "00000",
			MerchantState:         mLoc.StateProvince,
			RequestedPaymentSvc:   "1",
			NumberOfPaymentForms:  "0",
			UsageCode:             "0",
			ReasonCode:            "00",
			SettlementFlag:        "2",
			ACI:                   "1",
			AuthCode:              defaultString(parsedFields.AuthCode, "000000"),
			POSTerminalCap:        " ",
			CardholderIDMethod:    " ",
			CollectionOnlyFlag:    " ",
			POSEntryMode:          defaultString(parsedFields.POSEntryMode, "  "),
			CentralProcessingDate: centralProcDate,
			ReimbursementAttr:     "0",
		}
		records = append(records, tcr0.Format())

		// 2. TCR 1: Additional Data
		tcr1 := &TCR1AdditionalData{
			TC:                 txCode,
			TCQ:                "0",
			TCRSequence:        TCR1,
			BusinessFormatCode: " ",
			MemberMessageText:  "MESSAGE TEXT-------------------------------------",
			CardAcceptorID:     defaultString(parsedFields.CardAcceptorID, "123456789012345"),
			TerminalID:         defaultString(parsedFields.TerminalID, "88888880"),
			NationalReimbFee:   0,
			AcceptanceTermInd:  "0",
			PurchaseIdentifier: defaultString(parsedFields.RRN, "1234567890123456789012345"),
			CashbackAmount:     0,
		}
		records = append(records, tcr1.Format())

		// 3. TCR 5: Payment Service Data
		tcr5 := &TCR5PaymentServiceData{
			TC:                   txCode,
			TCQ:                  "0",
			TCRSequence:          TCR5,
			TransactionID:        defaultString(parsedFields.TransactionID, "000000000000000"),
			AuthorizedAmount:     destAmt,
			AuthCurrencyCode:     defaultString(parsedFields.CurrencyCode, "840"),
			AuthResponseCode:     defaultString(tx.ResponseCode, "00"),
			ValidationCode:       "    ",
			MultipleClearingSeq:  "00",
			MultipleClearingCnt:  "00",
			TotalAuthorizedAmt:   destAmt,
			InformationIndicator: "N",
			InterchangeFeeAmount: 0,
			InterchangeFeeSign:   "D",
			SourceToBaseExchRate: "00000000",
			BaseToDestExchRate:   "00000000",
			OptionalIssuerISA:    0,
			PurchaseIdentifier:   parsedFields.RRN,
		}
		records = append(records, tcr5.Format())

		// 4. TCR 7: Chip Card Data (if Field 55 EMV data is present)
		if parsedFields.EMVData != "" {
			tcr7 := &TCR7ChipCardData{
				TC:          txCode,
				TCQ:         "0",
				TCRSequence: TCR7,
				EMVRawHex:   parsedFields.EMVData,
			}
			records = append(records, tcr7.Format())
		}

		monetaryCount++
		destAmountSum += destAmt
		sourceAmountSum += sourceAmt
	}

	if monetaryCount == 0 {
		return &CTFResult{
			SkippedCount: skippedCount,
		}, fmt.Errorf("no approved transactions found matching criteria")
	}

	// 5. TC 91: Batch Trailer
	yyddd := fmt.Sprintf("%02d%03d", opts.GenerationTime.Year()%100, opts.GenerationTime.YearDay())
	totalTCRs := len(records) + 1 // +1 for TC 91

	tc91 := &TC91BatchTrailer{
		TC:                   TCBatchTrailer,
		TCQ:                  "0",
		TCRSequence:          TCR0,
		CIB:                  opts.CIB,
		ProcessingDate:       yyddd,
		DestinationAmountSum: destAmountSum,
		MonetaryTxCount:      monetaryCount,
		BatchNumber:          opts.BatchNumber,
		TotalTCRCount:        totalTCRs,
		CenterBatchID:        opts.CenterBatchID,
		NumberOfTransactions: monetaryCount + 1,
		SourceAmountSum:      sourceAmountSum,
	}
	records = append(records, tc91.Format())

	// 6. TC 92: File Trailer
	tc92 := &TC92FileTrailer{
		TC91BatchTrailer: *tc91,
	}
	tc92.CenterBatchID = opts.CenterFileID
	records = append(records, tc92.Format())

	var buf bytes.Buffer
	for _, rec := range records {
		buf.WriteString(rec.String())
		buf.WriteString("\n")
	}

	return &CTFResult{
		Records:              records,
		RawContent:           buf.Bytes(),
		MonetaryTxCount:      monetaryCount,
		TotalTCRCount:        totalTCRs,
		DestinationAmountSum: destAmountSum,
		SourceAmountSum:      sourceAmountSum,
		BatchNumber:          opts.BatchNumber,
		CIB:                  opts.CIB,
		ProcessingDate:       yyddd,
		SkippedCount:         skippedCount,
	}, nil
}

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

func extractMerchantLocation(reqMap map[string]interface{}) string {
	if loc := getStringField(reqMap, "43"); loc != "" {
		return loc
	}
	if f34, ok := reqMap["34"].(map[string]interface{}); ok {
		var subMap map[string]interface{}
		if s, ok := f34["02"].(map[string]interface{}); ok {
			subMap = s
		} else if s, ok := f34["2"].(map[string]interface{}); ok {
			subMap = s
		}
		if subMap != nil {
			name := getStringField(subMap, "C1")
			city := getStringField(subMap, "C3")
			country := getStringField(subMap, "C6")
			if name != "" || city != "" {
				return fmt.Sprintf("%-25s%-13s%2s", name, city, country)
			}
		}
	}
	return ""
}

func extractEMVData(reqMap map[string]interface{}) string {
	if f55 := getStringField(reqMap, "55"); f55 != "" {
		return f55
	}
	if f104, ok := reqMap["104"].(map[string]interface{}); ok {
		if s, ok := f104["5F"].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func parseFieldsMap(jsonStr string) map[string]interface{} {
	if strings.TrimSpace(jsonStr) == "" {
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil
	}

	if fields, ok := data["fields"].(map[string]interface{}); ok {
		return fields
	}
	return data
}

func getStringField(m map[string]interface{}, key string) string {
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

func getInt64Field(m map[string]interface{}, key string) int64 {
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

func extractTransactionID(reqMap, respMap map[string]interface{}) string {
	// Try Field 62 from response or request
	for _, m := range []map[string]interface{}{respMap, reqMap} {
		if m == nil {
			continue
		}
		if f62, ok := m["62"]; ok && f62 != nil {
			switch v := f62.(type) {
			case map[string]interface{}:
				if sub2, ok := v["2"]; ok && sub2 != nil {
					return SanitizeNumeric(fmt.Sprintf("%v", sub2), 15)
				}
				if sub02, ok := v["02"]; ok && sub02 != nil {
					return SanitizeNumeric(fmt.Sprintf("%v", sub02), 15)
				}
				if sub1, ok := v["1"]; ok && sub1 != nil {
					str := SanitizeNumeric(fmt.Sprintf("%v", sub1), 15)
					if str != strings.Repeat("0", 15) {
						return str
					}
				}
			case string:
				raw := []byte(v)
				// Check for 8-byte bitmap with Bit 2 (0x40) set
				if len(raw) >= 16 && (raw[0]&0x40) != 0 {
					bcdHex := hex.EncodeToString(raw[8:16])
					if len(bcdHex) >= 15 {
						tid := bcdHex[len(bcdHex)-15:]
						return SanitizeNumeric(tid, 15)
					}
				}
				cleaned := SanitizeNumeric(v, 15)
				if cleaned != strings.Repeat("0", 15) {
					return cleaned
				}
			}
		}
	}
	return "000000000000000"
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
