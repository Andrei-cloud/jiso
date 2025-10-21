package command

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
	"github.com/olekukonko/tablewriter"
)

type InfoCommand struct {
	Tc transactions.Repository
}

func (c *InfoCommand) Name() string {
	return "info"
}

func (c *InfoCommand) Synopsis() string {
	return "Show information about selected transaction."
}

func (c *InfoCommand) Execute() error {
	names := c.Tc.ListNames()
	if len(names) == 0 {
		fmt.Println("No transactions available")
		return nil
	}

	qs := []*survey.Question{
		{
			Name: "transaction",
			Prompt: &survey.Select{
				Message: "Select transaction:",
				Options: names,
			},
		},
	}

	var trxnName string
	err := survey.Ask(qs, &trxnName)
	if err != nil {
		return err
	}

	// Get transaction details
	name, _, fieldsJSON, err := c.Tc.Info(trxnName)
	if err != nil {
		return err
	}

	// Parse the fields JSON to extract MTI and Processing Code
	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
		return fmt.Errorf("failed to parse transaction fields: %w", err)
	}

	// Extract MTI (field 0) and Processing Code (field 3)
	mti := ""
	if mtiVal, ok := fields["0"]; ok {
		mti = fmt.Sprintf("%v", mtiVal)
	}

	// Extract Processing Code - may be string or nested object
	procCode := ""
	if pcVal, ok := fields["3"]; ok {
		switch v := pcVal.(type) {
		case string:
			procCode = v
		case map[string]interface{}:
			// For composite processing codes, try to combine the values
			parts := []string{}
			for _, val := range v {
				parts = append(parts, fmt.Sprintf("%v", val))
			}
			if len(parts) > 0 {
				procCode = fmt.Sprintf("%v", pcVal) // Show the structure
			}
		default:
			procCode = fmt.Sprintf("%v", pcVal)
		}
	}

	// Format message field with better readability
	formattedMessage := formatFieldsJSON(fields)

	// Show details in nice table - using Auto-merge to handle multi-line cells properly
	table := tablewriter.NewWriter(os.Stdout)
	// table.SetHeader([]string{"Field", "Value"})
	// table.SetAutoMergeCells(false)
	// table.SetRowLine(true)
	table.Append([]string{"Name", name})
	table.Append([]string{"MTI", mti})
	table.Append([]string{"Processing Code", procCode})

	// Split the formatted message into lines and add each one to preserve formatting
	messageLines := strings.Split(formattedMessage, "\n")
	if len(messageLines) > 0 {
		// Add first line with "Message" label
		table.Append([]string{"Message", messageLines[0]})

		// Add remaining lines with empty label for proper alignment
		for i := 1; i < len(messageLines); i++ {
			if messageLines[i] != "" {
				table.Append([]string{"", messageLines[i]})
			}
		}
	}

	table.Render()
	return nil
}

// formatFieldsJSON formats the fields JSON in a clean, readable format
func formatFieldsJSON(fields map[string]interface{}) string {
	// Get keys and sort them numerically
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}

	// Custom sort for field numbers
	sort.Slice(keys, func(i, j int) bool {
		// Convert to integers for numeric comparison, but treat errors as string comparison
		numI, errI := parseFieldNumber(keys[i])
		numJ, errJ := parseFieldNumber(keys[j])

		if errI == nil && errJ == nil {
			return numI < numJ
		}
		return keys[i] < keys[j]
	})

	// Build the formatted string
	var sb strings.Builder
	for _, k := range keys {
		value := fields[k]
		sb.WriteString(fmt.Sprintf("\"%s\": %v", k, formatValue(value)))
		sb.WriteString("\n")
	}

	return sb.String()
}

// parseFieldNumber attempts to convert a field key to an integer
func parseFieldNumber(key string) (int, error) {
	var num int
	_, err := fmt.Sscanf(key, "%d", &num)
	return num, err
}

// formatValue formats a value properly for display
func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return fmt.Sprintf("\"%s\"", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
