package command

import (
	"fmt"
	"os"

	"jiso/internal/transactions"

	"github.com/olekukonko/tablewriter"
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
	names := c.Tc.ListNames()
	if len(names) == 0 {
		fmt.Println("No transactions available")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Transaction Name"})

	for _, name := range names {
		table.Append([]string{name})
	}

	fmt.Println("Available transactions:")
	table.Render()
	return nil
}
