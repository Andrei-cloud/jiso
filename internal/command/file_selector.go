package command

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AlecAivazis/survey/v2"
)

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
			return "", fmt.Errorf("no suitable files found in %s", fs.currentDir)
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
