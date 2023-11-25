package cli

import (
	"fmt"
	"strings"

	"github.com/chzyer/readline"
)

func (cli *CLI) runWithHistory() error {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "(? for Help)\033[31m»\033[0m ",
		HistoryFile:     "/tmp/readline.tmp",
		AutoComplete:    nil,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return fmt.Errorf("failed to create readline instance: %w", err)
	}
	defer rl.Close()

	for {
		command, err := rl.Readline()
		if err != nil { // io.EOF, readline.ErrInterrupt
			break
		}

		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}

		err = cli.handleCommand(command)
		if err != nil {
			fmt.Println(err)
		}

	}

	return nil
}

func (cli *CLI) handleCommand(command string) error {
	var err error

	switch command {
	case "quit", "exit":
		cli.stopAllWorkers()
		cli.svc.Close()
		fmt.Println("Exiting CLI tool")
		return nil
	case "help", "h", "?":
		cli.printHelp()
	case "version", "v":
		cli.printVersion()
	case "clear", "cls":
		cli.ClearTerminal()
	case "stats", "status":
		cli.printWorkerStats()
	case "stop-all":
		cli.printWorkerStats()
		cli.stopAllWorkers()
	case "reload":
		if cli.svc.IsConnected() {
			fmt.Println("Connection is open. Disconnect first.")
			break
		}
		cli.stopAllWorkers()
		cli.svc.Close()
		err := cli.InitService()
		if err != nil {
			fmt.Printf("Error reloading service: %s\n", err)
		}
	case "stop":
		if len(cli.workers) == 0 {
			fmt.Println("No background workers running")
			break
		}
		err := cli.stopWorker()
		if err != nil {
			fmt.Printf("Error stopping worker: %s\n", err)
		}
	default:
		parts := strings.Fields(command)
		cmdName := parts[0]

		cmd, ok := cli.commands[cmdName]
		if !ok {
			return fmt.Errorf("unknown command: %s", command)
		}

		err = cmd.Execute()
		if err != nil {
			fmt.Printf("Error executing command: %s\n", err)
		}
	}
	return err
}
