package command

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	json "github.com/goccy/go-json"

	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
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
	if err := VerifyTx(c.Tc); err != nil {
		return err
	}
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

	fmt.Printf("Name: %s\n", name)
	fmt.Printf("MTI: %s\n", mti)
	fmt.Printf("Processing Code: %s\n", procCode)
	fmt.Println("Message:")
	fmt.Print(formattedMessage)

	fmt.Println("\nSample Message Build")
	fmt.Println("--------------------")
	fmt.Println("Composing a sample message with dataset interpolation if dataset_name is configured...")

	sampleMsg, composeErr := c.Tc.Compose(trxnName)
	if composeErr != nil {
		fmt.Printf("Compose error: %v\n", composeErr)
		return nil
	}

	packed, packErr := sampleMsg.Pack()
	if packErr != nil {
		fmt.Printf("Pack error: %v\n", packErr)
	} else {
		fmt.Printf("Packed bytes: %d\n", len(packed))
		fmt.Println("\nPacked HEX dump:")
		fmt.Print(hexDump(packed))
	}

	fmt.Println("\nParsed field view:")
	fmt.Print(renderParsedMessage(sampleMsg))

	return nil
}

func renderParsedMessage(msg *iso8583.Message) string {
	var buf bytes.Buffer
	utils.Describe(msg, &buf, iso8583.DoNotFilterFields()...)
	return buf.String()
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
