package command

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// FileSelector provides interactive file selection
type FileSelector struct {
	currentDir string
	fileType   string // "spec" or "transaction"
}

func NewFileSelector(fileType string) *FileSelector {
	var startDir string
	switch fileType {
	case "spec":
		startDir = "./specs"
	case "transaction":
		startDir = "./transactions"
	default:
		startDir = "."
	}

	return &FileSelector{
		currentDir: startDir,
		fileType:   fileType,
	}
}

func (fs *FileSelector) SelectFile() (string, error) {
	for {
		entries, err := os.ReadDir(fs.currentDir)
		if err != nil {
			return "", fmt.Errorf("failed to read directory: %w", err)
		}

		// Sort entries: directories first, then files
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() && !entries[j].IsDir() {
				return true
			}
			if !entries[i].IsDir() && entries[j].IsDir() {
				return false
			}
			return entries[i].Name() < entries[j].Name()
		})

		var options []string
		options = append(options, ".. (go up)")

		for _, entry := range entries {
			if entry.IsDir() {
				options = append(options, entry.Name()+"/")
			} else {
				// Filter files based on type
				if fs.shouldIncludeFile(entry.Name()) {
					options = append(options, entry.Name())
				}
			}
		}

		if len(options) == 1 { // only ".." option
			return "", fmt.Errorf("no suitable files found")
		}

		var selected string
		prompt := &survey.Select{
			Message: fmt.Sprintf("Select %s file (current: %s):", fs.fileType, fs.currentDir),
			Options: options,
		}

		err = survey.AskOne(prompt, &selected)
		if err != nil {
			return "", err
		}

		if selected == ".. (go up)" {
			parent := filepath.Dir(fs.currentDir)
			if parent == fs.currentDir {
				// Already at root
				continue
			}
			fs.currentDir = parent
		} else if strings.HasSuffix(selected, "/") {
			// Directory selected
			dirName := strings.TrimSuffix(selected, "/")
			fs.currentDir = filepath.Join(fs.currentDir, dirName)
		} else {
			// File selected
			return filepath.Join(fs.currentDir, selected), nil
		}
	}
}

func (fs *FileSelector) shouldIncludeFile(filename string) bool {
	switch fs.fileType {
	case "spec":
		return strings.HasSuffix(filename, ".json")
	case "transaction":
		return strings.HasSuffix(filename, ".json")
	default:
		return true
	}
}
