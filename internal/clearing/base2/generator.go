package base2

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"jiso/internal/db"
)

// GeneratorOptions contains configuration options for Base II CTF generation.
type GeneratorOptions struct {
	BINFilter      string
	CIB            string
	AcquirerBIN    string
	BatchNumber    int
	CenterBatchID  string
	CenterFileID   string
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
func GenerateCTF(_ *db.SessionRecord, txs []*db.EnrichedTransactionRecord, opts GeneratorOptions) (*CTFResult, error) {
	normalizeCTFOptions(&opts)

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

		set, counted := buildTransactionRecords(tx, int64(i+1), opts, cleanBINFilter)
		if !counted {
			skippedCount++

			continue
		}

		records = append(records, set.records...)
		monetaryCount++
		destAmountSum += set.destAmt
		sourceAmountSum += set.sourceAmt
	}

	if monetaryCount == 0 {
		return &CTFResult{
			SkippedCount: skippedCount,
		}, fmt.Errorf("no approved transactions found matching criteria")
	}

	return assembleCTF(records, opts, monetaryCount, destAmountSum, sourceAmountSum, skippedCount), nil
}

// assembleCTF appends the batch (TC 91) and file (TC 92) trailers, renders the
// raw file bytes, and returns the result summary.
func assembleCTF(records []*Record, opts GeneratorOptions, monetaryCount int, destSum, sourceSum int64, skippedCount int) *CTFResult {
	// 5. TC 91: Batch Trailer
	yyddd := fmt.Sprintf("%02d%03d", opts.GenerationTime.Year()%100, opts.GenerationTime.YearDay())
	totalTCRs := len(records) + 1 // +1 for TC 91

	tc91 := &TC91BatchTrailer{
		TC:                   TCBatchTrailer,
		TCQ:                  "0",
		TCRSequence:          TCR0,
		CIB:                  opts.CIB,
		ProcessingDate:       yyddd,
		DestinationAmountSum: destSum,
		MonetaryTxCount:      monetaryCount,
		BatchNumber:          opts.BatchNumber,
		TotalTCRCount:        totalTCRs,
		CenterBatchID:        opts.CenterBatchID,
		NumberOfTransactions: monetaryCount + 1,
		SourceAmountSum:      sourceSum,
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
		DestinationAmountSum: destSum,
		SourceAmountSum:      sourceSum,
		BatchNumber:          opts.BatchNumber,
		CIB:                  opts.CIB,
		ProcessingDate:       yyddd,
		SkippedCount:         skippedCount,
	}
}

// normalizeCTFOptions fills the option fields the generator derives from other
// options or defaults (generation time, batch number, CIB, center ids).
func normalizeCTFOptions(opts *GeneratorOptions) {
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
}

// tcrSet is the record set built for one approved transaction, plus the amounts
// that feed the batch totals.
type tcrSet struct {
	records   []*Record
	destAmt   int64
	sourceAmt int64
}

// buildTransactionRecords builds the TCR 0/1/5 (and TCR 7 when EMV data is
// present) records for a single transaction. It reports (false) when the BIN
// filter excludes the card, so the caller counts it as skipped.
func buildTransactionRecords(tx *db.EnrichedTransactionRecord, seq int64, opts GeneratorOptions, binFilter string) (tcrSet, bool) {
	parsedFields := extractISOFields(tx)

	pan := parsedFields.PAN
	if binFilter != "" && !strings.HasPrefix(pan, binFilter) {
		return tcrSet{}, false
	}

	txTime := tx.Timestamp
	if txTime.IsZero() {
		txTime = opts.GenerationTime
	}

	c := tcrContext{
		pf:        parsedFields,
		opts:      opts,
		txCode:    determineTC(parsedFields.ProcessingCode),
		destAmt:   parsedFields.Amount,
		sourceAmt: parsedFields.Amount,
		respCode:  tx.ResponseCode,
	}
	if c.destAmt == 0 && parsedFields.SettlementAmount > 0 {
		c.destAmt = parsedFields.SettlementAmount
	}

	// Date fields
	c.purchaseDate = fmt.Sprintf("%02d%02d", txTime.Month(), txTime.Day())
	if len(parsedFields.DateLocal) == 4 {
		c.purchaseDate = parsedFields.DateLocal
	}
	c.centralProcDate = fmt.Sprintf("%d%03d", txTime.Year()%10, txTime.YearDay())

	// Sequence & ARN
	c.arn = parsedFields.ARN
	if c.arn == "" || len(c.arn) < 23 {
		c.arn = GenerateARN(opts.AcquirerBIN, txTime, seq)
	}

	// Merchant Info parsing (Field 43: 25 Name, 13 City, 2 Country/State / 3-alpha Country)
	c.mLoc = ParseMerchantLocation(parsedFields.MerchantLocation, parsedFields.CurrencyCode)

	records := c.tcr0Records()
	records = append(records, c.tcr1Records()...)
	records = append(records, c.tcr5Records()...)
	records = append(records, c.tcr7Records()...)

	return tcrSet{records: records, destAmt: c.destAmt, sourceAmt: c.sourceAmt}, true
}

// tcrContext holds the per-transaction values shared by the TCR builders.
type tcrContext struct {
	pf              *isoFields
	opts            GeneratorOptions
	txCode          string
	arn             string
	purchaseDate    string
	centralProcDate string
	destAmt         int64
	sourceAmt       int64
	respCode        string
	mLoc            MerchantLocationDetails
}

// tcr0Records builds the TCR 0 (Draft Data) record.
func (c tcrContext) tcr0Records() []*Record {
	tcr0 := &TCR0DraftData{
		TC:                    c.txCode,
		TCQ:                   "0",
		TCRSequence:           TCR0,
		AccountNumber:         c.pf.PAN,
		AccountExtension:      c.pf.PANExtension,
		FloorLimitIndicator:   " ",
		CRBIndicator:          " ",
		ARN:                   c.arn,
		AcquirerBusinessID:    c.opts.AcquirerBIN,
		PurchaseDate:          c.purchaseDate,
		DestinationAmount:     c.destAmt,
		DestinationCurrency:   defaultString(c.pf.CurrencyCode, "840"),
		SourceAmount:          c.sourceAmt,
		SourceCurrency:        defaultString(c.pf.CurrencyCode, "840"),
		MerchantName:          c.mLoc.MerchantName,
		MerchantCity:          c.mLoc.MerchantCity,
		MerchantCountry:       c.mLoc.CountryNumeric,
		MCC:                   defaultString(c.pf.MCC, "5999"),
		MerchantZIP:           "00000",
		MerchantState:         c.mLoc.StateProvince,
		RequestedPaymentSvc:   "1",
		NumberOfPaymentForms:  "0",
		UsageCode:             "0",
		ReasonCode:            "00",
		SettlementFlag:        "2",
		ACI:                   "1",
		AuthCode:              defaultString(c.pf.AuthCode, "000000"),
		POSTerminalCap:        " ",
		CardholderIDMethod:    " ",
		CollectionOnlyFlag:    " ",
		POSEntryMode:          defaultString(c.pf.POSEntryMode, "  "),
		CentralProcessingDate: c.centralProcDate,
		ReimbursementAttr:     "0",
	}

	return []*Record{tcr0.Format()}
}

// tcr1Records builds the TCR 1 (Additional Data) record.
func (c tcrContext) tcr1Records() []*Record {
	tcr1 := &TCR1AdditionalData{
		TC:                 c.txCode,
		TCQ:                "0",
		TCRSequence:        TCR1,
		BusinessFormatCode: " ",
		MemberMessageText:  "MESSAGE TEXT-------------------------------------",
		CardAcceptorID:     defaultString(c.pf.CardAcceptorID, "123456789012345"),
		TerminalID:         defaultString(c.pf.TerminalID, "88888880"),
		NationalReimbFee:   0,
		AcceptanceTermInd:  "0",
		PurchaseIdentifier: defaultString(c.pf.RRN, "1234567890123456789012345"),
		CashbackAmount:     0,
	}

	return []*Record{tcr1.Format()}
}

// tcr5Records builds the TCR 5 (Payment Service Data) record.
func (c tcrContext) tcr5Records() []*Record {
	tcr5 := &TCR5PaymentServiceData{
		TC:                   c.txCode,
		TCQ:                  "0",
		TCRSequence:          TCR5,
		TransactionID:        defaultString(c.pf.TransactionID, "000000000000000"),
		AuthorizedAmount:     c.destAmt,
		AuthCurrencyCode:     defaultString(c.pf.CurrencyCode, "840"),
		AuthResponseCode:     defaultString(c.respCode, "00"),
		ValidationCode:       "    ",
		MultipleClearingSeq:  "00",
		MultipleClearingCnt:  "00",
		TotalAuthorizedAmt:   c.destAmt,
		InformationIndicator: "N",
		InterchangeFeeAmount: 0,
		InterchangeFeeSign:   "D",
		SourceToBaseExchRate: "00000000",
		BaseToDestExchRate:   "00000000",
		OptionalIssuerISA:    0,
		PurchaseIdentifier:   c.pf.RRN,
	}

	return []*Record{tcr5.Format()}
}

// tcr7Records builds the TCR 7 (Chip Card Data) record when EMV data is present.
func (c tcrContext) tcr7Records() []*Record {
	if c.pf.EMVData == "" {
		return nil
	}

	tcr7 := &TCR7ChipCardData{
		TC:          c.txCode,
		TCQ:         "0",
		TCRSequence: TCR7,
		EMVRawHex:   c.pf.EMVData,
	}

	return []*Record{tcr7.Format()}
}
