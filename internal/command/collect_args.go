package command

import (
	"fmt"

	cfg "jiso/internal/config"

	"github.com/AlecAivazis/survey/v2"
)

type CollectArgsCommand struct{}

func (c *CollectArgsCommand) Name() string {
	return "collect-args"
}

func (c *CollectArgsCommand) Synopsis() string {
	return "Collect missing arguments interactively"
}

func (c *CollectArgsCommand) Execute() error {
	questions := []*survey.Question{}

	if cfg.GetConfig().GetHost() == "" {
		questions = append(questions, &survey.Question{
			Name: "host",
			Prompt: &survey.Input{
				Default: "localhost",
				Message: "Enter hostname to connect to:",
			},
			Validate: survey.Required,
		})
	}

	if cfg.GetConfig().GetPort() == "" {
		questions = append(questions, &survey.Question{
			Name: "port",
			Prompt: &survey.Input{
				Default: "9999",
				Message: "Enter port to connect to:",
			},
			Validate: survey.Required,
		})
	}

	if cfg.GetConfig().GetSpec() == "" {
		questions = append(questions, &survey.Question{
			Name: "specfile",
			Prompt: &survey.Input{
				Default: "./specs/spec_bcp.json",
				Message: "Enter path to specification file in JSON format (press Enter to browse):",
			},
		})
	}

	if cfg.GetConfig().GetFile() == "" {
		questions = append(questions, &survey.Question{
			Name: "file",
			Prompt: &survey.Input{
				Default: "./transactions/transaction.json",
				Message: "Enter path to transaction file in JSON format (press Enter to browse):",
			},
		})
	}

	if len(questions) == 0 {
		fmt.Println("No missing arguments")
		return nil
	}

	answers := struct {
		Host     string
		Port     string
		SpecFile string
		File     string
	}{}

	err := survey.Ask(questions, &answers)
	if err != nil {
		return err
	}

	// Handle file selection for empty inputs or default values
	if answers.SpecFile == "" || answers.SpecFile == "./specs/spec_bcp.json" {
		fmt.Println("Browsing for specification file...")
		selector := NewFileSelector("spec")
		selectedFile, err := selector.SelectFile()
		if err != nil {
			return fmt.Errorf("file selection failed: %w", err)
		}
		answers.SpecFile = selectedFile
	}

	if answers.File == "" || answers.File == "./transactions/transaction.json" {
		fmt.Println("Browsing for transaction file...")
		selector := NewFileSelector("transaction")
		selectedFile, err := selector.SelectFile()
		if err != nil {
			return fmt.Errorf("file selection failed: %w", err)
		}
		answers.File = selectedFile
	}

	cfg.GetConfig().SetHost(answers.Host)
	cfg.GetConfig().SetPort(answers.Port)
	cfg.GetConfig().SetSpec(answers.SpecFile)
	cfg.GetConfig().SetFile(answers.File)
	fmt.Println("Arguments collected successfully")

	return nil
}


