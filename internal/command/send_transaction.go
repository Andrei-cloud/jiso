package command

import (
	"fmt"
	"strings"
	"time"

	"jiso/internal/metrics"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/view"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
)

var ErrConnectionOffline = fmt.Errorf("connection is offline")

type SendCommand struct {
	Tc           transactions.Repository
	Svc          *service.Service
	stats        *metrics.TransactionStats
	networkStats *metrics.NetworkingStats
	renderer     *view.ISOMessageRenderer
}

func (c *SendCommand) Name() string {
	return "send"
}

func (c *SendCommand) Synopsis() string {
	return "Send selected transaction. (reqires connection to server)"
}

func (c *SendCommand) Execute() error {
	// Perform thorough connection checks
	if c.Svc == nil {
		return fmt.Errorf("service not initialized")
	}

	if !c.Svc.IsConnected() {
		return ErrConnectionOffline
	}

	if c.Svc.Connection == nil {
		return ErrConnectionOffline
	}

	if c.Svc.Connection.Status() != connection.StatusOnline {
		return ErrConnectionOffline
	}

	qs := []*survey.Question{
		{
			Name: "send",
			Prompt: &survey.Select{
				Message: "Select transaction:",
				Options: c.Tc.ListNames(),
			},
		},
	}

	var trxnName string
	err := survey.Ask(qs, &trxnName)
	if err != nil {
		return err
	}

	msg, err := c.Tc.Compose(trxnName)
	if err != nil {
		return err
	}

	// Validate message before sending
	if err := validateMessage(msg); err != nil {
		return fmt.Errorf("message validation failed: %w", err)
	}

	rawMsg, err := msg.Pack()
	if err != nil {
		return err
	}

	rebuiltMsg := iso8583.NewMessage(msg.GetSpec())
	err = rebuiltMsg.Unpack(rawMsg)
	if err != nil {
		return err
	}

	// Ensure renderer is initialized
	if c.renderer == nil {
		c.renderer = view.NewISOMessageRenderer(nil) // Use default stdout
	}

	// Remove the first print of the message to avoid duplication
	// c.renderer.RenderMessage(rebuiltMsg) - removed

	startTime := time.Now()
	response, err := c.retrySend(msg, 3) // Retry up to 3 times

	// Log transaction regardless of success/failure
	success := err == nil
	c.Tc.LogTransaction(trxnName, success)

	if err != nil {
		return err
	}
	elapsed := time.Since(startTime)

	// Print response and timing using the renderer
	c.renderer.RenderRequestResponse(rebuiltMsg, response, elapsed)

	return nil
}

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
	// Required fields for network management transactions
	requiredFields := map[int]string{
		7:  "Transmission Date/Time",
		11: "STAN (System Trace Audit Number)",
		37: "RRN (Retrieval Reference Number)",
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

func (c *SendCommand) retrySend(msg *iso8583.Message, maxRetries int) (*iso8583.Message, error) {
	var lastErr error
	baseDelay := 500 * time.Millisecond
	maxDelay := 5 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}
			time.Sleep(delay)
		}

		resp, err := c.Svc.Send(msg)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Record error classification
		if c.networkStats != nil {
			c.networkStats.RecordError(isRetriableError(err))
		}

		// If error is not retriable, don't retry
		if !isRetriableError(err) {
			break
		}
	}

	return nil, lastErr
}

func (c *SendCommand) StartClock() {
	if c.stats == nil {
		c.stats = metrics.NewTransactionStats()
	}
	c.stats.StartClock()
}

func (c *SendCommand) ExecuteBackground(trxnName string) error {
	// Check connection health before attempting to send
	if !c.Svc.IsConnected() {
		// Log the issue but don't fail the transaction - allow worker to continue
		fmt.Printf("Warning: Connection is offline, skipping transaction %s\n", trxnName)
		return nil // Return nil to not count as failure
	}

	// Initialize stats if not already done
	if c.stats == nil {
		c.stats = metrics.NewTransactionStats()
	}

	// Handle transaction with hash suffix
	if strings.Contains(trxnName, "#") {
		parts := strings.Split(trxnName, "#")
		trxnName = parts[0]
	}

	msg, err := c.Tc.Compose(trxnName)
	if err != nil {
		// Log failed transaction
		c.Tc.LogTransaction(trxnName, false)
		return err
	}

	// Validate message before sending
	if err := validateMessage(msg); err != nil {
		// Log failed transaction
		c.Tc.LogTransaction(trxnName, false)
		return fmt.Errorf("message validation failed: %w", err)
	}

	executionStart := time.Now()
	resp, err := c.Svc.BackgroundSend(msg)
	if err != nil {
		// Log failed transaction
		c.Tc.LogTransaction(trxnName, false)
		// Record error
		if c.networkStats != nil {
			c.networkStats.RecordError(isRetriableError(err))
		}
		return err
	}
	execTime := time.Since(executionStart)

	rc := resp.GetField(39)
	rcStr, err := rc.String()
	if err != nil {
		// Log transaction with partial success
		c.Tc.LogTransaction(trxnName, false)
		return err
	}

	// Log successful transaction
	c.Tc.LogTransaction(trxnName, true)

	// Record metrics
	c.stats.RecordExecution(execTime, rcStr)

	return nil
}

func (c *SendCommand) Stats() int {
	if c.stats == nil {
		return 0
	}
	return c.stats.ExecutionCount()
}

func (c *SendCommand) Duration() time.Duration {
	if c.stats == nil {
		return 0
	}
	return c.stats.Duration()
}

func (c *SendCommand) MeanExecutionTime() time.Duration {
	if c.stats == nil {
		return 0
	}
	return c.stats.MeanExecutionTime()
}

func (c *SendCommand) StandardDeviation() time.Duration {
	if c.stats == nil {
		return 0
	}
	return c.stats.StandardDeviation()
}

func (c *SendCommand) ResponseCodes() map[string]uint64 {
	if c.stats == nil {
		return make(map[string]uint64)
	}
	return c.stats.ResponseCodes()
}

func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Permanent errors - don't retry
	permanentErrors := []string{
		"message validation failed",
		"MTI field",
		"required field",
		"field error",
		"invalid",
		"authentication failed",
		"authorization failed",
		"unauthorized",
		"forbidden",
	}

	for _, permErr := range permanentErrors {
		if strings.Contains(strings.ToLower(errStr), permErr) {
			return false
		}
	}

	// Connection closed is permanent
	if err == connection.ErrConnectionClosed {
		return false
	}

	// Network-related errors are retriable
	retriableErrors := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"network is unreachable",
		"no such host",
		"temporary failure",
		"server unavailable",
		"service unavailable",
		"internal server error", // Sometimes temporary
		"bad gateway",           // Network issue
		"gateway timeout",
	}

	for _, retErr := range retriableErrors {
		if strings.Contains(strings.ToLower(errStr), retErr) {
			return true
		}
	}

	// Default: assume retriable for unknown errors (safer to retry)
	return true
}
