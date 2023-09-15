package command

import (
	"fmt"

	cfg "jiso/internal/config"

	"github.com/AlecAivazis/survey/v2"
)

type CollectArgsCommand struct {
}

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
				Default: "./specs/spec.json",
				Message: "Enter path to customized specification file in JSON format:",
			},
			Validate: survey.Required,
		})
	}

	if cfg.GetConfig().GetFile() == "" {
		questions = append(questions, &survey.Question{
			Name: "file",
			Prompt: &survey.Input{
				Default: "./transactions/transaction.json",
				Message: "Enter path to transaction file in JSON format:",
			},
			Validate: survey.Required,
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

	cfg.GetConfig().SetHost(answers.Host)
	cfg.GetConfig().SetPort(answers.Port)
	cfg.GetConfig().SetSpec(answers.SpecFile)
	cfg.GetConfig().SetFile(answers.File)
	fmt.Println("Arguments collected successfully")

	return nil
}
