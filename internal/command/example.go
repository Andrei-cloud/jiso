package command

import "fmt"

type ExampleCommand struct{}

func (c *ExampleCommand) Name() string {
	return "example"
}

func (c *ExampleCommand) Synopsis() string {
	return "An example command"
}

func (c *ExampleCommand) Execute() error {
	fmt.Println("Executing example command")
	return nil
}
