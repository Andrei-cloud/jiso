package command

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"jiso/internal/config"
	"jiso/internal/db"
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
	statsMu      sync.Mutex
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
	if err := VerifySpec(c.Svc); err != nil {
		return err
	}
	if err := VerifyTx(c.Tc); err != nil {
		return err
	}
	if err := VerifyConnection(c.Svc); err != nil {
		return err
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

	// Only print hex dump if debug mode is not enabled (connection manager handles it)
	if config.GetConfig().GetHex() && !c.Svc.GetDebugMode() {
		fmt.Printf("Request HEX:\n%s", hexDump(rawMsg))
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

	startTime := time.Now()
	response, err := c.retrySend(msg, 3) // Retry up to 3 times

	// Log transaction regardless of success/failure
	success := err == nil
	c.Tc.LogTransaction(trxnName, success)

	elapsed := time.Since(startTime)

	// Store transaction in database if configured
	if config.GetConfig().GetDbPath() != "" {
		requestJSON, _ := db.MessageToJSON(msg)
		var responseJSON *string
		var processingTimeMs int

		if success && response != nil {
			respJSON, _ := db.MessageToJSON(response)
			responseJSON = &respJSON
			processingTimeMs = int(elapsed.Milliseconds())
		} else {
			processingTimeMs = 0 // Timeout or error
		}

		db.LogTransaction(
			config.GetConfig().GetSessionId(),
			trxnName,
			requestJSON,
			responseJSON,
			processingTimeMs,
			success,
		)
	}

	if err != nil {
		return err
	}

	// Verify STAN correlation for asynchronous processing
	requestStanField := msg.GetField(11)
	if requestStanField == nil {
		return fmt.Errorf("request STAN field missing")
	}
	requestStan, err := requestStanField.String()
	if err != nil {
		return fmt.Errorf("failed to get request STAN: %w", err)
	}

	responseStanField := response.GetField(11)
	if responseStanField == nil {
		return fmt.Errorf("response STAN field missing")
	}
	responseStan, err := responseStanField.String()
	if err != nil {
		return fmt.Errorf("failed to get response STAN: %w", err)
	}

	if requestStan != responseStan {
		// Log STAN mismatch as a protocol error
		fmt.Printf(
			"STAN mismatch detected: request=%s, response=%s for transaction %s\n",
			requestStan,
			responseStan,
			trxnName,
		)
		return fmt.Errorf("STAN mismatch: request=%s, response=%s", requestStan, responseStan)
	}

	if config.GetConfig().GetHex() && !c.Svc.GetDebugMode() {
		responsePacked, packErr := response.Pack()
		if packErr == nil {
			fmt.Printf("Response HEX:\n%s", hexDump(responsePacked))
		}
	}

	// Print response and timing using the renderer
	c.renderer.RenderRequestResponse(rebuiltMsg, response, elapsed)

	return nil
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
	c.statsMu.Lock()
	if c.stats == nil {
		c.stats = metrics.NewTransactionStats()
	}
	c.statsMu.Unlock()
	c.stats.StartClock()
}

func (c *SendCommand) ExecuteBackground(trxnName string, skipValidation bool, sessionID string) (string, time.Duration, error) {
	// Check connection health before attempting to send
	if c.Svc == nil || !c.Svc.IsConnected() {
		// Log the issue but don't fail the transaction - allow worker to continue
		fmt.Printf("Warning: Connection is offline, skipping transaction %s\n", trxnName)
		return "OFFLINE", 0, nil // Return nil to not count as failure
	}

	// Initialize stats if not already done
	c.statsMu.Lock()
	if c.stats == nil {
		c.stats = metrics.NewTransactionStats()
	}
	c.statsMu.Unlock()

	// Handle transaction with hash suffix
	if strings.Contains(trxnName, "#") {
		parts := strings.Split(trxnName, "#")
		trxnName = parts[0]
	}

	msg, err := c.Tc.Compose(trxnName)
	if err != nil {
		// Log failed transaction
		c.Tc.LogTransaction(trxnName, false)
		return "COMPOSE_ERR", 0, err
	}

	// Validate message before sending (unless skipped)
	if !skipValidation {
		if err := validateMessage(msg); err != nil {
			// Log failed transaction
			c.Tc.LogTransaction(trxnName, false)
			return "VALIDATION_ERR", 0, fmt.Errorf("message validation failed: %w", err)
		}
	}

	logSessionID := sessionID
	if logSessionID == "" {
		logSessionID = config.GetConfig().GetSessionId()
	}

	executionStart := time.Now()
	responseChan, err := c.Svc.SendAsync(msg, trxnName)
	if err != nil {
		// Log failed transaction
		c.Tc.LogTransaction(trxnName, false)

		// Store failed transaction in database
		if config.GetConfig().GetDbPath() != "" {
			requestJSON, _ := db.MessageToJSON(msg)
			db.LogTransaction(
				logSessionID,
				trxnName,
				requestJSON,
				nil, // No response
				0,   // No processing time
				false,
			)
		}

		// Record error
		if c.networkStats != nil {
			c.networkStats.RecordError(isRetriableError(err))
		}
		return "SEND_ERR", time.Since(executionStart), err
	}

	// Wait for response or timeout
	resp := <-responseChan
	execTime := time.Since(executionStart)

	if resp == nil {
		// Timeout occurred
		c.Tc.LogTransaction(trxnName, false)

		// Store timeout transaction in database
		if config.GetConfig().GetDbPath() != "" {
			requestJSON, _ := db.MessageToJSON(msg)
			db.LogTransaction(
				logSessionID,
				trxnName,
				requestJSON,
				nil, // No response
				int(execTime.Milliseconds()),
				false,
			)
		}

		return "TIMEOUT", execTime, fmt.Errorf("response timeout for transaction %s", trxnName)
	}

	// Verify STAN correlation for asynchronous processing
	requestStanField := msg.GetField(11)
	if requestStanField == nil {
		return "STAN_ERR", execTime, fmt.Errorf("request STAN field missing")
	}
	requestStan, err := requestStanField.String()
	if err != nil {
		return "STAN_ERR", execTime, fmt.Errorf("failed to get request STAN: %w", err)
	}

	responseStanField := resp.GetField(11)
	if responseStanField == nil {
		return "STAN_ERR", execTime, fmt.Errorf("response STAN field missing")
	}
	responseStan, err := responseStanField.String()
	if err != nil {
		return "STAN_ERR", execTime, fmt.Errorf("failed to get response STAN: %w", err)
	}

	if requestStan != responseStan {
		// Log STAN mismatch as a protocol error
		fmt.Printf(
			"STAN mismatch detected: request=%s, response=%s for transaction %s\n",
			requestStan,
			responseStan,
			trxnName,
		)
		c.Tc.LogTransaction(trxnName, false)

		// Store mismatch in database as failure
		if config.GetConfig().GetDbPath() != "" {
			requestJSON, _ := db.MessageToJSON(msg)
			responseJSON, _ := db.MessageToJSON(resp)
			mappedResponseJSON := &responseJSON
			db.LogTransaction(
				logSessionID,
				trxnName,
				requestJSON,
				mappedResponseJSON,
				int(execTime.Milliseconds()),
				false, // Mark as failure due to mismatch
			)
		}

		return "STAN_MISMATCH", execTime, fmt.Errorf("STAN mismatch: request=%s, response=%s", requestStan, responseStan)
	}

	rc := resp.GetField(39)
	if rc == nil {
		return "MISSING_RC", execTime, fmt.Errorf("response code field 39 missing")
	}
	rcStr, err := rc.String()
	if err != nil {
		// Log transaction with partial success
		c.Tc.LogTransaction(trxnName, false)

		// Store transaction with error in database
		if config.GetConfig().GetDbPath() != "" {
			requestJSON, _ := db.MessageToJSON(msg)
			db.LogTransaction(
				logSessionID,
				trxnName,
				requestJSON,
				nil, // No valid response
				0,   // No processing time
				false,
			)
		}

		return "RC_PARSE_ERR", execTime, err
	}

	// Log successful transaction
	c.Tc.LogTransaction(trxnName, true)

	// Store successful transaction in database
	if config.GetConfig().GetDbPath() != "" {
		requestJSON, _ := db.MessageToJSON(msg)
		responseJSON, _ := db.MessageToJSON(resp)
		mappedResponseJSON := &responseJSON
		db.LogTransaction(
			logSessionID,
			trxnName,
			requestJSON,
			mappedResponseJSON,
			int(execTime.Milliseconds()),
			true,
		)
	}

	// Record metrics - Removed to prevent race condition on shared state
	// Worker controller tracks success/failure counts independently
	// c.stats.RecordExecution(execTime, rcStr)

	return rcStr, execTime, nil
}

func (c *SendCommand) Stats() int {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	if c.stats == nil {
		return 0
	}
	return c.stats.ExecutionCount()
}

func (c *SendCommand) Duration() time.Duration {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	if c.stats == nil {
		return 0
	}
	return c.stats.Duration()
}

func (c *SendCommand) MeanExecutionTime() time.Duration {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	if c.stats == nil {
		return 0
	}
	return c.stats.MeanExecutionTime()
}

func (c *SendCommand) StandardDeviation() time.Duration {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	if c.stats == nil {
		return 0
	}
	return c.stats.StandardDeviation()
}

func (c *SendCommand) ResponseCodes() map[string]uint64 {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	if c.stats == nil {
		return make(map[string]uint64)
	}
	return c.stats.ResponseCodes()
}

