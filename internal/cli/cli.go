package cli

import (
	"fmt"

	cmd "jiso/internal/command"

	"github.com/AlecAivazis/survey/v2"
)

type CLI struct {
	commands map[string]cmd.Command
}

func NewCLI() *CLI {
	return &CLI{
		commands: make(map[string]cmd.Command),
	}
}

func (cli *CLI) AddCommand(command cmd.Command) {
	cli.commands[command.Name()] = command
}

func (cli *CLI) Run() error {
	if collectArgsCommand, ok := cli.commands["collect-args"]; ok {
		err := collectArgsCommand.Execute()
		if err != nil {
			return err
		}
	}

	for {
		var commandName string
		err := cli.prompt([]*survey.Question{
			{
				Name: "command",
				Prompt: &survey.Input{
					Message: "Enter command name:",
				},
			},
		}, &commandName)
		if err != nil {
			return err
		}

		if commandName == "quit" {
			fmt.Println("Exiting CLI tool")
			return nil
		}

		if commandName == "help" {
			cli.printHelp()
			continue
		}

		command, ok := cli.commands[commandName]
		if !ok {
			fmt.Printf("Invalid command: %s\n", commandName)
			continue
		}

		fmt.Printf("%s: %s\n", command.Name(), command.Synopsis())
		err = command.Execute()
		if err != nil {
			fmt.Printf("Error executing command: %s\n", err)
		}
	}
}

func (cli *CLI) prompt(questions []*survey.Question, response interface{}) error {
	return survey.Ask(questions, response)
}

func (cli *CLI) printHelp() {
	fmt.Println("Available commands:")
	maxNameLen := 0
	for _, cmd := range cli.commands {
		if len(cmd.Name()) > maxNameLen {
			maxNameLen = len(cmd.Name())
		}
	}
	for _, cmd := range cli.commands {
		fmt.Printf("%-*s  %s\n", maxNameLen, cmd.Name(), cmd.Synopsis())
	}
}
