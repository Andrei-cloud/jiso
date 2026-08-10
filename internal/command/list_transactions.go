package command

import (
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"

	"jiso/internal/transactions"
)

type ListCommand struct {
	Tc transactions.Repository
}

func (c *ListCommand) Name() string {
	return "list"
}

func (c *ListCommand) Synopsis() string {
	return "List available transactions."
}

func (c *ListCommand) Execute() error {
	if err := VerifyTx(c.Tc); err != nil {
		return err
	}
	names := c.Tc.ListNames()
	if len(names) == 0 {
		fmt.Println("No transactions available")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	for _, name := range names {
		_ = table.Append([]string{name})
	}

	fmt.Println("Available transactions:")
	_ = table.Render()

	return nil
}
