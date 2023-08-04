package command

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
)

type CollectArgsCommand struct {
	Host         *string
	Port         *string
	SpecFileName *string
	File         *string
}

func (c *CollectArgsCommand) Name() string {
	return "collect-args"
}

func (c *CollectArgsCommand) Synopsis() string {
	return "Collect missing arguments interactively"
}

func (c *CollectArgsCommand) Execute() error {
	questions := []*survey.Question{}

	if *c.Host == "" {
		questions = append(questions, &survey.Question{
			Name: "host",
			Prompt: &survey.Input{
				Default: "localhost",
				Message: "Enter hostname to connect to:",
			},
			Validate: survey.Required,
		})
	}

	if *c.Port == "" {
		questions = append(questions, &survey.Question{
			Name: "port",
			Prompt: &survey.Input{
				Default: "9999",
				Message: "Enter port to connect to:",
			},
			Validate: survey.Required,
		})
	}

	if *c.SpecFileName == "" {
		questions = append(questions, &survey.Question{
			Name: "specfile",
			Prompt: &survey.Input{
				Default: "./specs/spec.json",
				Message: "Enter path to customized specification file in JSON format:",
			},
			Validate: survey.Required,
		})
	}

	if *c.File == "" {
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

	if *c.Host == "" {
		*c.Host = answers.Host
	}

	if *c.Port == "" {
		*c.Port = answers.Port
	}

	if *c.SpecFileName == "" {
		*c.SpecFileName = answers.SpecFile
	}

	if *c.File == "" {
		*c.File = answers.File
	}

	fmt.Println("Arguments collected successfully")

	return nil
}
