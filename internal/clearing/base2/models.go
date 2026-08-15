package base2

import (
	"fmt"
	"strings"
)

const (
	// RecordLength is the exact length in bytes for all Base II CTF records.
	RecordLength = 168

	// Transaction Codes (TC)
	TCSalesDraft       = "05"
	TCCreditVoucher    = "06"
	TCCashDisbursement = "07"
	TCBatchTrailer     = "91"
	TCFileTrailer      = "92"
	TCHeader           = "90"

	// Transaction Component Sequence Numbers (TCR)
	TCR0 = "0"
	TCR1 = "1"
	TCR5 = "5"
	TCR7 = "7"
)

// Record represents a single 168-character fixed-width CTF record.
type Record [RecordLength]byte

// NewRecord creates an empty Record filled with spaces.
func NewRecord() *Record {
	r := &Record{}
	for i := range r {
		r[i] = ' '
	}
	return r
}

// Set places a string value into positions [start, end] (1-indexed, inclusive).
// If padLeft is true, val is right-aligned and padded with padChar on the left.
// If padLeft is false, val is left-aligned and padded with padChar on the right.
func (r *Record) Set(start, end int, val string, padLeft bool, padChar byte) {
	if start < 1 || end > RecordLength || start > end {
		return
	}

	length := end - start + 1
	valLen := len(val)

	var formatted string
	if valLen > length {
		if padLeft {
			formatted = val[valLen-length:]
		} else {
			formatted = val[:length]
		}
	} else if valLen < length {
		pad := strings.Repeat(string(padChar), length-valLen)
		if padLeft {
			formatted = pad + val
		} else {
			formatted = val + pad
		}
	} else {
		formatted = val
	}

	buf := []byte(formatted)
	for i, b := range buf {
		if b < 32 || b > 126 {
			if padChar == '0' {
				buf[i] = '0'
			} else {
				buf[i] = ' '
			}
		}
	}

	copy(r[start-1:end], buf)
}

// SanitizeNumeric extracts only digits '0'-'9' and ensures exact length padded with '0'.
func SanitizeNumeric(s string, length int) string {
	var digits strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	res := digits.String()
	if len(res) == 0 {
		return strings.Repeat("0", length)
	}
	if len(res) > length {
		return res[len(res)-length:]
	}
	if len(res) < length {
		return strings.Repeat("0", length-len(res)) + res
	}
	return res
}

// String returns the 168-character string representation of the record.
func (r *Record) String() string {
	return string(r[:])
}

// Bytes returns the record as a byte slice.
func (r *Record) Bytes() []byte {
	res := make([]byte, RecordLength)
	copy(res, r[:])
	return res
}

// TCR0DraftData holds draft data fields for TCR 0.
type TCR0DraftData struct {
	TC                    string
	TCQ                   string
	TCRSequence           string
	AccountNumber         string
	AccountExtension      string
	FloorLimitIndicator   string
	CRBIndicator          string
	ARN                   string
	AcquirerBusinessID    string
	PurchaseDate          string // MMDD
	DestinationAmount     int64
	DestinationCurrency   string
	SourceAmount          int64
	SourceCurrency        string
	MerchantName          string
	MerchantCity          string
	MerchantCountry       string
	MCC                   string
	MerchantZIP           string
	MerchantState         string
	RequestedPaymentSvc   string
	NumberOfPaymentForms  string
	UsageCode             string
	ReasonCode            string
	SettlementFlag        string
	ACI                   string
	AuthCode              string
	POSTerminalCap        string
	CardholderIDMethod    string
	CollectionOnlyFlag    string
	POSEntryMode          string
	CentralProcessingDate string // YDDD
	ReimbursementAttr     string
}

// Format formats TCR0 into a 168-character Record.
func (d *TCR0DraftData) Format() *Record {
	r := NewRecord()
	r.Set(1, 2, defaultString(d.TC, TCSalesDraft), true, '0')
	r.Set(3, 3, defaultString(d.TCQ, "0"), false, '0')
	r.Set(4, 4, defaultString(d.TCRSequence, TCR0), false, '0')
	r.Set(5, 20, d.AccountNumber, false, ' ')
	r.Set(21, 23, defaultString(d.AccountExtension, "000"), false, '0')
	r.Set(24, 24, defaultString(d.FloorLimitIndicator, " "), false, ' ')
	r.Set(25, 25, defaultString(d.CRBIndicator, " "), false, ' ')
	r.Set(26, 26, " ", false, ' ')
	r.Set(27, 49, d.ARN, true, '0')
	r.Set(50, 57, d.AcquirerBusinessID, true, '0')
	r.Set(58, 61, d.PurchaseDate, true, '0')
	r.Set(62, 73, fmt.Sprintf("%012d", d.DestinationAmount), true, '0')
	r.Set(74, 76, defaultString(d.DestinationCurrency, "840"), false, ' ')
	r.Set(77, 88, fmt.Sprintf("%012d", d.SourceAmount), true, '0')
	r.Set(89, 91, defaultString(d.SourceCurrency, "840"), false, ' ')
	r.Set(92, 116, defaultString(d.MerchantName, "MER NAME TEST"), false, ' ')
	r.Set(117, 129, defaultString(d.MerchantCity, "MCITY TEST"), false, ' ')
	r.Set(130, 132, defaultString(d.MerchantCountry, "840"), false, ' ')
	r.Set(133, 136, defaultString(d.MCC, "5999"), true, '0')
	r.Set(137, 141, defaultString(d.MerchantZIP, "00000"), true, '0')
	r.Set(142, 144, defaultString(d.MerchantState, "   "), false, ' ')
	r.Set(145, 145, defaultString(d.RequestedPaymentSvc, "1"), false, ' ')
	r.Set(146, 146, defaultString(d.NumberOfPaymentForms, "0"), false, '0')
	r.Set(147, 147, defaultString(d.UsageCode, "0"), false, '0')
	r.Set(148, 149, defaultString(d.ReasonCode, "00"), true, '0')
	r.Set(150, 150, defaultString(d.SettlementFlag, "2"), false, '0')
	r.Set(151, 151, defaultString(d.ACI, "1"), false, ' ')
	r.Set(152, 157, defaultString(d.AuthCode, "000000"), false, ' ')
	r.Set(158, 158, defaultString(d.POSTerminalCap, " "), false, ' ')
	r.Set(159, 159, " ", false, ' ')
	r.Set(160, 160, defaultString(d.CardholderIDMethod, " "), false, ' ')
	r.Set(161, 161, defaultString(d.CollectionOnlyFlag, " "), false, ' ')
	r.Set(162, 163, defaultString(d.POSEntryMode, "  "), false, ' ')
	r.Set(164, 167, d.CentralProcessingDate, true, '0')
	r.Set(168, 168, defaultString(d.ReimbursementAttr, "0"), false, '0')
	return r
}

// TCR1AdditionalData holds additional data fields for TCR 1.
type TCR1AdditionalData struct {
	TC                 string
	TCQ                string
	TCRSequence        string
	BusinessFormatCode string
	MemberMessageText  string
	CardAcceptorID     string
	TerminalID         string
	NationalReimbFee   int64
	AcceptanceTermInd  string
	PurchaseIdentifier string
	CashbackAmount     int64
}

// Format formats TCR1 into a 168-character Record.
func (d *TCR1AdditionalData) Format() *Record {
	r := NewRecord()
	r.Set(1, 2, defaultString(d.TC, TCSalesDraft), true, '0')
	r.Set(3, 3, defaultString(d.TCQ, "0"), false, '0')
	r.Set(4, 4, defaultString(d.TCRSequence, TCR1), false, '0')
	r.Set(5, 5, defaultString(d.BusinessFormatCode, " "), false, ' ')
	r.Set(6, 7, "  ", false, ' ')
	r.Set(8, 12, "     ", false, ' ')
	r.Set(13, 14, "  ", false, ' ')
	r.Set(15, 16, "  ", false, ' ')
	r.Set(17, 22, "939456", false, ' ')
	r.Set(23, 23, " ", false, ' ')
	r.Set(24, 73, defaultString(d.MemberMessageText, "MESSAGE TEXT-------------------------------------"), false, ' ')
	r.Set(74, 75, "  ", false, ' ')
	r.Set(76, 78, "   ", false, ' ')
	r.Set(79, 79, " ", false, ' ')
	r.Set(80, 80, " ", false, ' ')
	r.Set(81, 95, defaultString(d.CardAcceptorID, "123456789012345"), false, ' ')
	r.Set(96, 103, defaultString(d.TerminalID, "88888880"), false, ' ')
	r.Set(104, 115, fmt.Sprintf("%012d", d.NationalReimbFee), true, '0')
	r.Set(116, 116, " ", false, ' ')
	r.Set(117, 117, " ", false, ' ')
	r.Set(118, 121, "    ", false, ' ')
	r.Set(122, 122, " ", false, ' ')
	r.Set(123, 123, " ", false, ' ')
	r.Set(124, 124, defaultString(d.AcceptanceTermInd, "0"), false, '0')
	r.Set(125, 125, " ", false, ' ')
	r.Set(126, 126, " ", false, ' ')
	r.Set(127, 127, " ", false, ' ')
	r.Set(128, 128, " ", false, ' ')
	r.Set(129, 129, " ", false, ' ')
	r.Set(130, 130, " ", false, ' ')
	r.Set(131, 132, "  ", false, ' ')
	r.Set(133, 157, defaultString(d.PurchaseIdentifier, "1234567890123456789012345"), false, ' ')
	r.Set(158, 166, fmt.Sprintf("%09d", d.CashbackAmount), true, '0')
	r.Set(167, 167, " ", false, ' ')
	r.Set(168, 168, " ", false, ' ')
	return r
}

// TCR5PaymentServiceData holds payment service data fields for TCR 5.
type TCR5PaymentServiceData struct {
	TC                   string
	TCQ                  string
	TCRSequence          string
	TransactionID        string
	AuthorizedAmount     int64
	AuthCurrencyCode     string
	AuthResponseCode     string
	ValidationCode       string
	MultipleClearingSeq  string
	MultipleClearingCnt  string
	TotalAuthorizedAmt   int64
	InformationIndicator string
	InterchangeFeeAmount int64
	InterchangeFeeSign   string
	SourceToBaseExchRate string
	BaseToDestExchRate   string
	OptionalIssuerISA    int64
	PurchaseIdentifier   string
}

// Format formats TCR5 into a 168-character Record.
func (d *TCR5PaymentServiceData) Format() *Record {
	r := NewRecord()
	r.Set(1, 2, defaultString(d.TC, TCSalesDraft), true, '0')
	r.Set(3, 3, defaultString(d.TCQ, "0"), false, '0')
	r.Set(4, 4, defaultString(d.TCRSequence, TCR5), false, '0')
	r.Set(5, 19, SanitizeNumeric(d.TransactionID, 15), true, '0')
	r.Set(20, 31, fmt.Sprintf("%012d", d.AuthorizedAmount), true, '0')
	r.Set(32, 34, defaultString(d.AuthCurrencyCode, "840"), false, ' ')
	r.Set(35, 36, defaultString(d.AuthResponseCode, "  "), false, ' ')
	r.Set(37, 40, defaultString(d.ValidationCode, "    "), false, ' ')
	r.Set(41, 41, " ", false, ' ')
	r.Set(42, 42, " ", false, ' ')
	r.Set(43, 44, "  ", false, ' ')
	r.Set(45, 46, defaultString(d.MultipleClearingSeq, "00"), true, '0')
	r.Set(47, 48, defaultString(d.MultipleClearingCnt, "00"), true, '0')
	r.Set(49, 49, " ", false, ' ')
	r.Set(50, 61, fmt.Sprintf("%012d", d.TotalAuthorizedAmt), true, '0')
	r.Set(62, 62, defaultString(d.InformationIndicator, "N"), false, ' ')
	r.Set(63, 76, "              ", false, ' ')
	r.Set(77, 77, " ", false, ' ')
	r.Set(78, 79, "  ", false, ' ')
	r.Set(80, 81, "  ", false, ' ')
	r.Set(82, 91, "          ", false, ' ')
	r.Set(92, 106, fmt.Sprintf("%015d", d.InterchangeFeeAmount), true, '0')
	r.Set(107, 107, defaultString(d.InterchangeFeeSign, "D"), false, ' ')
	r.Set(108, 115, defaultString(d.SourceToBaseExchRate, "00000000"), true, '0')
	r.Set(116, 123, defaultString(d.BaseToDestExchRate, "00000000"), true, '0')
	r.Set(124, 135, fmt.Sprintf("%012d", d.OptionalIssuerISA), true, '0')
	r.Set(136, 137, "  ", false, ' ')
	r.Set(138, 143, "      ", false, ' ')
	r.Set(144, 144, " ", false, ' ')
	r.Set(145, 148, "    ", false, ' ')
	r.Set(149, 149, " ", false, ' ')
	if d.PurchaseIdentifier != "" {
		r.Set(150, 164, d.PurchaseIdentifier, false, ' ')
	} else {
		r.Set(150, 165, "                ", false, ' ')
	}
	r.Set(166, 166, " ", false, ' ')
	r.Set(167, 167, " ", false, ' ')
	r.Set(168, 168, " ", false, ' ')
	return r
}

// TCR7ChipCardData holds EMV chip transaction fields for TCR 7.
type TCR7ChipCardData struct {
	TC          string
	TCQ         string
	TCRSequence string
	EMVRawHex   string
}

// Format formats TCR7 into a 168-character Record.
func (d *TCR7ChipCardData) Format() *Record {
	r := NewRecord()
	r.Set(1, 2, defaultString(d.TC, TCSalesDraft), true, '0')
	r.Set(3, 3, defaultString(d.TCQ, "0"), false, '0')
	r.Set(4, 4, defaultString(d.TCRSequence, TCR7), false, '0')
	if d.EMVRawHex != "" {
		r.Set(5, 168, d.EMVRawHex, false, ' ')
	}
	return r
}

// TC91BatchTrailer holds batch trailer control totals.
type TC91BatchTrailer struct {
	TC                   string
	TCQ                  string
	TCRSequence          string
	CIB                  string
	ProcessingDate       string // YYDDD
	DestinationAmountSum int64
	MonetaryTxCount      int
	BatchNumber          int
	TotalTCRCount        int
	CenterBatchID        string
	NumberOfTransactions int
	SourceAmountSum      int64
}

// Format formats TC91 into a 168-character Record.
func (d *TC91BatchTrailer) Format() *Record {
	r := NewRecord()
	r.Set(1, 2, defaultString(d.TC, TCBatchTrailer), true, '0')
	r.Set(3, 3, defaultString(d.TCQ, "0"), false, '0')
	r.Set(4, 4, defaultString(d.TCRSequence, TCR0), false, '0')
	r.Set(5, 10, defaultString(d.CIB, "400129"), true, '0')
	r.Set(11, 15, d.ProcessingDate, true, '0')
	r.Set(16, 30, fmt.Sprintf("%015d", d.DestinationAmountSum), true, '0')
	r.Set(31, 42, fmt.Sprintf("%012d", d.MonetaryTxCount), true, '0')
	r.Set(43, 48, fmt.Sprintf("%06d", d.BatchNumber), true, '0')
	r.Set(49, 60, fmt.Sprintf("%012d", d.TotalTCRCount), true, '0')
	r.Set(61, 66, "000000", true, '0')
	r.Set(67, 74, defaultString(d.CenterBatchID, "08101423"), false, ' ')
	r.Set(75, 83, fmt.Sprintf("%09d", d.NumberOfTransactions), true, '0')
	r.Set(84, 101, "000000000000000000", true, '0')
	r.Set(102, 116, fmt.Sprintf("%015d", d.SourceAmountSum), true, '0')
	r.Set(117, 131, "000000000000000", true, '0')
	r.Set(132, 146, "000000000000000", true, '0')
	r.Set(147, 161, "000000000000000", true, '0')
	r.Set(162, 168, "       ", false, ' ')
	return r
}

// TC92FileTrailer holds file trailer control totals.
type TC92FileTrailer struct {
	TC91BatchTrailer
}

// Format formats TC92 into a 168-character Record.
func (d *TC92FileTrailer) Format() *Record {
	d.TC = TCFileTrailer
	return d.TC91BatchTrailer.Format()
}

func defaultString(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}
