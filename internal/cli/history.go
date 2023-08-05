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
		panic(err)
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
		case "status":
			cli.printWorkerStatus()
		case "stop-all":
			cli.stopAllWorkers()
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
			// cmdParts := strings.Fields(cmd)
			// cmdName := cmdParts[0]
			// args := cmdParts[1:]

			command, ok := cli.commands[command]
			if !ok {
				fmt.Printf("Invalid command: %s\n", command)
				continue
			}

			// err := command.Parse(args)
			// if err != nil {
			//     fmt.Printf("Error parsing command arguments: %s\n", err)
			//     continue
			// }

			fmt.Printf("%s: %s\n", command.Name(), command.Synopsis())
			err := command.Execute()
			if err != nil {
				fmt.Printf("Error executing command: %s\n", err)
			}
		}

	}

	return nil
}
