package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	cmd "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
)

type CLI struct {
	commands map[string]cmd.Command
	svc      *service.Service
	tc       *transactions.TransactionCollection
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
	svc, err := service.NewService(
		cfg.GetConfig().GetHost(),
		cfg.GetConfig().GetPort(),
		cfg.GetConfig().GetSpec(),
	)
	if err != nil {
		return err
	}

	cli.setService(svc)

	// New transcation collection
	cli.tc, err = transactions.NewTransactionCollection(
		cfg.GetConfig().GetFile(),
		cli.getSpec(),
	)
	if err != nil {
		return err
	}

	cli.AddCommand(&cmd.ListCommand{Tc: cli.tc})
	cli.AddCommand(&cmd.InfoCommand{Tc: cli.tc})

	for {
		var commandName string
		err := cli.prompt([]*survey.Question{
			{
				Name: "command",
				Prompt: &survey.Input{
					Message: "Enter command:",
				},
			},
		}, &commandName)
		if err != nil {
			return err
		}

		if commandName == "quit" {
			cli.svc.Close()
			fmt.Println("Exiting CLI tool")
			return nil
		}

		if commandName == "help" {
			cli.printHelp()
			continue
		}

		if commandName == "clear" || commandName == "cls" {
			cli.ClearTerminal()
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

func (cli *CLI) ClearTerminal() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear terminal: %v\n", err)
	}
}

func (cli *CLI) Close() {
	if cli.svc != nil {
		cli.svc.Close()
	}
}

func (cli *CLI) prompt(questions []*survey.Question, response interface{}) error {
	return survey.Ask(questions, response)
}

func (cli *CLI) printHelp() {
	fmt.Print("Available commands:\n\n")
	maxNameLen := 0
	for _, cmd := range cli.commands {
		if len(cmd.Name()) > maxNameLen {
			maxNameLen = len(cmd.Name())
		}
	}
	for _, cmd := range cli.commands {
		if cmd.Name() == "collect-args" {
			continue
		}
		fmt.Printf("\t%-*s  %s\n", maxNameLen, cmd.Name(), cmd.Synopsis())
	}
	fmt.Println()

	fmt.Println("Type 'clear' or 'cls' to clear the terminal")
	fmt.Println("Type 'help' to see this list again")
	fmt.Println("Type 'quit' to exit the CLI tool")
}

func (cli *CLI) setService(svc *service.Service) {
	cli.svc = svc
}

func (cli *CLI) getSpec() *iso8583.MessageSpec {
	if cli.svc == nil {
		return nil
	}
	return cli.svc.GetSpec()
}
