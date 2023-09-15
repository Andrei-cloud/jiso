package command

import (
	"fmt"
	"jiso/internal/transactions"
)

type ListCommand struct {
	Tc *transactions.TransactionCollection
}

func (c *ListCommand) Name() string {
	return "list"
}

func (c *ListCommand) Synopsis() string {
	return "Command prints all transactions in the collection."
}

func (c *ListCommand) Execute() error {
	list := c.Tc.ListFormatted()
	// Print list of transactions by line
	for _, line := range list {
		fmt.Printf("\t%s\n", line)
	}

	return nil
}
