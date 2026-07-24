package command

import (
	"fmt"

	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/moov-io/iso8583"
)

// TxCommand handles interactive transaction file selection and switching
type TxCommand struct {
	TxPath string
	Svc    *service.Service
	Ctrl   CLIController
}

func (c *TxCommand) Name() string     { return "tx" }
func (c *TxCommand) Synopsis() string { return "Select or load transaction file (tx [<path>])" }

func (c *TxCommand) SetArgs(args []string) {
	if len(args) > 0 {
		c.TxPath = args[0]
	}
}

func (c *TxCommand) Execute() error {
	txPath := c.TxPath
	if txPath == "" {
		selector := NewFileSelector("transaction")
		selected, err := selector.SelectFile()
		if err != nil {
			return fmt.Errorf("transaction file selection failed: %w", err)
		}
		txPath = selected
	}

	cfg.GetConfig().SetFile(txPath)

	var count int
	if c.Ctrl != nil {
		n, err := c.Ctrl.ReloadTransactions(txPath)
		if err != nil {
			return fmt.Errorf("failed to reload transaction repository in CLI: %w", err)
		}
		count = n
	} else {
		var spec *iso8583.MessageSpec
		if c.Svc != nil {
			spec = c.Svc.GetSpec()
		}
		tc, err := transactions.NewTransactionCollection(txPath, spec)
		if err != nil {
			return fmt.Errorf("failed to load transaction file from '%s': %w", txPath, err)
		}
		count = len(tc.ListNames())
	}

	fmt.Printf("Transaction file updated successfully to: %s (Count: %d)\n", txPath, count)
	return nil
}
