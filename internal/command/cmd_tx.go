package command

import (
	"fmt"

	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"

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

	var spec *iso8583.MessageSpec
	if c.Svc != nil {
		spec = c.Svc.GetSpec()
	}
	if spec == nil {
		specPath := cfg.GetConfig().GetSpec()
		if specPath != "" {
			if s, err := utils.CreateSpecFromFile(specPath); err == nil {
				spec = s
			}
		}
	}

	tc, err := transactions.NewTransactionCollection(txPath, spec)
	if err != nil {
		return fmt.Errorf("failed to load transaction file from '%s': %w", txPath, err)
	}

	cfg.GetConfig().SetFile(txPath)

	if c.Ctrl != nil {
		if err := c.Ctrl.ReloadTransactions(txPath); err != nil {
			return fmt.Errorf("failed to reload transaction repository in CLI: %w", err)
		}
	}

	fmt.Printf("Transaction file updated successfully to: %s (Count: %d)\n", txPath, len(tc.ListNames()))
	return nil
}
