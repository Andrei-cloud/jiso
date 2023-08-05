package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	cmd "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
)

type CLI struct {
	commands map[string]cmd.Command
	svc      *service.Service
	tc       *transactions.TransactionCollection

	// Background worker state
	workers map[string]*workerState
	mu      sync.Mutex
}

type workerState struct {
	command  cmd.BgCommand
	interval time.Duration
	ticker   *time.Ticker
	done     chan struct{}
}

func NewCLI() *CLI {
	return &CLI{
		commands: make(map[string]cmd.Command),
		workers:  make(map[string]*workerState),
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
	cli.AddCommand(&cmd.SendCommand{Tc: cli.tc, Svc: cli.svc})
	cli.AddCommand(&cmd.ConnectCommand{Svc: cli.svc})
	cli.AddCommand(&cmd.DisconnectCommand{Svc: cli.svc})
	cli.AddCommand(&cmd.BackgroundCommand{Tc: cli.tc, Svc: cli.svc, Wrk: cli})

	return cli.runWithHistory()
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

	fmt.Print(`Workers controll commands:
Type 'status' to see the status of background workers
Type 'stop-all' to stop all background workers
Type 'stop' to stop a specific background worker

Other commands:
Type 'clear' or 'cls' to clear the terminal
Type 'help' to see this list again
Type 'quit' to exit the CLI tool`)
}
