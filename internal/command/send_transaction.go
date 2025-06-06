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
	Tc       transactions.Repository
	Svc      *service.Service
	stats    *metrics.TransactionStats
	renderer *view.ISOMessageRenderer
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
	response, err := c.Svc.Send(msg)

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

func (c *SendCommand) StartClock() {
	if c.stats == nil {
		c.stats = metrics.NewTransactionStats()
	}
	c.stats.StartClock()
}

func (c *SendCommand) ExecuteBackground(trxnName string) error {
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

	executionStart := time.Now()
	resp, err := c.Svc.BackgroundSend(msg)
	if err != nil {
		// Log failed transaction
		c.Tc.LogTransaction(trxnName, false)
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
