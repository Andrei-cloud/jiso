package command

import (
	"fmt"

	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/utils"
)

// SpecCommand handles interactive specification selection and switching
type SpecCommand struct {
	SpecPath string
	Svc      *service.Service
}

func (c *SpecCommand) Name() string     { return "spec" }
func (c *SpecCommand) Synopsis() string { return "Select or load ISO8583 specification file (spec [<path>])" }

func (c *SpecCommand) SetArgs(args []string) {
	if len(args) > 0 {
		c.SpecPath = args[0]
	}
}

func (c *SpecCommand) Execute() error {
	specPath := c.SpecPath
	if specPath == "" {
		selector := NewFileSelector("spec")
		selected, err := selector.SelectFile()
		if err != nil {
			return fmt.Errorf("specification selection failed: %w", err)
		}
		specPath = selected
	}

	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return fmt.Errorf("failed to load specification from '%s': %w", specPath, err)
	}

	cfg.GetConfig().SetSpec(specPath)
	if c.Svc != nil {
		c.Svc.SetSpec(spec)
	}

	fmt.Printf("Specification updated successfully to: %s (Spec: %s)\n", specPath, spec.Name)
	return nil
}
