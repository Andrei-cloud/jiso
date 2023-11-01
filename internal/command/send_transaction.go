package command

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
)

var ErrConnectionOffline = fmt.Errorf("connection is offline")

type SendCommand struct {
	Tc            *transactions.TransactionCollection
	Svc           *service.Service
	start         time.Time
	counts        int
	executionTime time.Duration
	variance      time.Duration
	respCodes     map[string]uint64
	respCodesLock sync.Mutex
}

func (c *SendCommand) Name() string {
	return "send"
}

func (c *SendCommand) Synopsis() string {
	return "Send selected transaction. (reqires connection to server)"
}

func (c *SendCommand) Execute() error {
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

	rebuioldMsg := iso8583.NewMessage(msg.GetSpec())
	err = rebuioldMsg.Unpack(rawMsg)
	if err != nil {
		return err
	}

	// Print ISO8583 message
	iso8583.Describe(rebuioldMsg, os.Stdout, iso8583.DoNotFilterFields()...)

	c.start = time.Now()
	response, err := c.Svc.Send(msg)
	if err != nil {
		return err
	}
	elapsed := time.Since(c.start)

	// Print response
	iso8583.Describe(response, os.Stdout, iso8583.DoNotFilterFields()...)

	// Print elapsed time
	fmt.Printf("\nElapsed time: %s\n", elapsed.Round(time.Millisecond))
	return nil
}

func (c *SendCommand) StartClock() {
	c.start = time.Now()
}

func (c *SendCommand) ExecuteBackground(trxnName string) error {
	if c.respCodes == nil {
		c.respCodes = make(map[string]uint64)
	}

	if strings.Contains(trxnName, "#") {
		parts := strings.Split(trxnName, "#")
		trxnName = parts[0]
	}

	msg, err := c.Tc.Compose(trxnName)
	if err != nil {
		return err
	}

	executionStart := time.Now()
	resp, err := c.Svc.BackgroundSend(msg)
	if err != nil {
		return err
	}
	t := time.Since(executionStart)
	c.executionTime += t
	c.counts++

	rc := resp.GetField(39)
	rc_str, err := rc.String()
	if err != nil {
		return err
	}

	c.respCodesLock.Lock()
	c.respCodes[rc_str]++
	c.respCodesLock.Unlock()

	diff := t - c.MeanExecutionTime()
	c.variance += diff * diff

	return nil
}

func (c *SendCommand) Stats() int {
	return c.counts
}

func (c *SendCommand) Duration() time.Duration {
	return time.Since(c.start)
}

func (c *SendCommand) MeanExecutionTime() time.Duration {
	return c.executionTime / time.Duration(c.counts)
}

func (c *SendCommand) StandardDeviation() time.Duration {
	locVariance := c.variance
	locVariance /= time.Duration(c.counts)
	return time.Duration(math.Sqrt(float64(locVariance)))
}

func (c *SendCommand) ResponseCodes() map[string]uint64 {
	return c.respCodes
}
