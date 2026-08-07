package command

import (
	"fmt"

	"github.com/moov-io/iso8583"
)

func validateMessage(msg *iso8583.Message) error {
	// Basic validation: check if MTI is set
	field := msg.GetField(0)
	if field == nil {
		return fmt.Errorf("MTI field missing")
	}
	mti, err := field.String()
	if err != nil {
		return fmt.Errorf("MTI field error: %w", err)
	}
	if mti == "" {
		return fmt.Errorf("MTI field is empty")
	}

	// Enhanced validation based on message type
	switch mti[:2] {
	case "02": // Financial messages
		return validateFinancialMessage(msg)
	case "08": // Network management messages
		return validateNetworkMessage(msg)
	default:
		// For other message types, basic MTI validation is sufficient
		return nil
	}
}

func validateFinancialMessage(msg *iso8583.Message) error {
	// Required fields for financial transactions
	requiredFields := map[int]string{
		2:  "PAN (Primary Account Number)",
		3:  "Processing Code",
		4:  "Transaction Amount",
		7:  "Transmission Date/Time",
		11: "STAN (System Trace Audit Number)",
		37: "RRN (Retrieval Reference Number)",
		41: "Terminal ID",
		43: "Card Acceptor Name/Location",
	}

	for fieldNum, fieldName := range requiredFields {
		field := msg.GetField(fieldNum)
		if field == nil {
			return fmt.Errorf("required field %d (%s) is missing", fieldNum, fieldName)
		}
		fieldStr, err := field.String()
		if err != nil {
			return fmt.Errorf("field %d (%s) error: %w", fieldNum, fieldName, err)
		}
		if fieldStr == "" {
			return fmt.Errorf("required field %d (%s) is empty", fieldNum, fieldName)
		}
	}

	// Additional validation for amount (field 4) - should be numeric
	amountField := msg.GetField(4)
	if amountField != nil {
		amountStr, _ := amountField.String()
		if amountStr != "" && !isNumeric(amountStr) {
			return fmt.Errorf("field 4 (Transaction Amount) must be numeric, got: %s", amountStr)
		}
	}

	// Additional validation for PAN (field 2) - should be numeric and reasonable length
	panField := msg.GetField(2)
	if panField != nil {
		panStr, _ := panField.String()
		if panStr != "" {
			if !isNumeric(panStr) {
				return fmt.Errorf("field 2 (PAN) must be numeric, got: %s", panStr)
			}
			if len(panStr) < 13 || len(panStr) > 19 {
				return fmt.Errorf("field 2 (PAN) length must be 13-19 digits, got: %d", len(panStr))
			}
		}
	}

	return nil
}

func validateNetworkMessage(msg *iso8583.Message) error {
	// Required fields for network management transactions (0800)
	requiredFields := map[int]string{
		7:  "Transmission Date/Time",
		11: "STAN (System Trace Audit Number)",
		70: "Network Management Information Code",
	}

	for fieldNum, fieldName := range requiredFields {
		field := msg.GetField(fieldNum)
		if field == nil {
			return fmt.Errorf("required field %d (%s) is missing", fieldNum, fieldName)
		}
		fieldStr, err := field.String()
		if err != nil {
			return fmt.Errorf("field %d (%s) error: %w", fieldNum, fieldName, err)
		}
		if fieldStr == "" {
			return fmt.Errorf("required field %d (%s) is empty", fieldNum, fieldName)
		}
	}

	return nil
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
