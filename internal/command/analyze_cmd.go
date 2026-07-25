package command

import (
	json "github.com/goccy/go-json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
)

// AnalyzeCommand provides interactive or direct reverse-engineering of PCAP / stream captures
type AnalyzeCommand struct {
	spec *iso8583.MessageSpec
	tc   transactions.Repository
	args []string
}

// NewAnalyzeCommand creates a new AnalyzeCommand instance
func NewAnalyzeCommand(spec *iso8583.MessageSpec, tc transactions.Repository) *AnalyzeCommand {
	return &AnalyzeCommand{
		spec: spec,
		tc:   tc,
	}
}

func (ac *AnalyzeCommand) Name() string { return "analyze" }

func (ac *AnalyzeCommand) Synopsis() string {
	return "Analyze stream/PCAP capture files to extract transaction templates & datasets"
}

func (ac *AnalyzeCommand) SetArgs(args []string) {
	ac.args = args
}

func (ac *AnalyzeCommand) Execute() error {
	if len(ac.args) == 0 {
		return ac.promptAnalyze()
	}

	// Non-interactive command format: analyze <streamFile> [specFile] [headerType] [outputTxFile]
	streamFile := ac.args[0]
	specPath := config.GetConfig().GetSpec()
	headerType := "binary2"
	outputTxFile := config.GetConfig().GetFile()

	if len(ac.args) > 1 && ac.args[1] != "" {
		specPath = ac.args[1]
	}
	if len(ac.args) > 2 && ac.args[2] != "" {
		headerType = ac.args[2]
	}
	if len(ac.args) > 3 && ac.args[3] != "" {
		outputTxFile = ac.args[3]
	}

	var spec *iso8583.MessageSpec
	var err error
	if specPath != "" {
		spec, err = utils.CreateSpecFromFile(specPath)
		if err != nil {
			return fmt.Errorf("failed to load spec from '%s': %w", specPath, err)
		}
	} else if ac.spec != nil {
		spec = ac.spec
	} else {
		spec = utils.GetDefaultSpec()
	}

	return ac.runAnalysis(streamFile, spec, headerType, outputTxFile, analyzer.TrafficDirection{Mode: "all"})
}

func (ac *AnalyzeCommand) promptAnalyze() error {
	fmt.Println("================================================================================")
	fmt.Println("               ISO8583 STREAM & PCAP TRAFFIC ANALYZER")
	fmt.Println("================================================================================")

	// 1. Select Analysis Goal
	var analysisGoal string
	goalPrompt := &survey.Select{
		Message: "1. Select Analysis Goal:",
		Options: []string{
			"1. Generate Transactions & Datasets (Client scenarios / sending)",
			"2. Generate Mock Server Routes (Mock server response routing)",
		},
		Default: "1. Generate Transactions & Datasets (Client scenarios / sending)",
	}
	if err := survey.AskOne(goalPrompt, &analysisGoal); err != nil {
		return err
	}
	isMockRouteGoal := strings.HasPrefix(analysisGoal, "2.")

	// 2. Select ISO8583 Specification File
	specFiles := utils.FindAvailableSpecFiles()
	var selectedSpec string
	specPrompt := &survey.Select{
		Message: "2. Select ISO8583 Specification File:",
		Options: specFiles,
		Default: specFiles[0],
	}
	if err := survey.AskOne(specPrompt, &selectedSpec); err != nil {
		return err
	}

	if selectedSpec == "Custom Path..." {
		inputPrompt := &survey.Input{
			Message: "   Enter path to specification JSON file:",
		}
		if err := survey.AskOne(inputPrompt, &selectedSpec); err != nil {
			return err
		}
	}

	spec := ac.spec
	if selectedSpec != "" && !strings.HasPrefix(selectedSpec, "[Default") {
		if loadedSpec, err := utils.CreateSpecFromFile(selectedSpec); err == nil {
			spec = loadedSpec
			fmt.Printf("   ✓ Loaded specification: %s\n", selectedSpec)
		} else {
			fmt.Printf("   ⚠️ Failed to load spec '%s' (%v), fallback to default\n", selectedSpec, err)
		}
	}
	if spec == nil {
		spec = utils.GetDefaultSpec()
	}

	// 3. Select TCP Header Length Type
	var headerType string
	headerPrompt := &survey.Select{
		Message: "3. Select TCP Header Length Type:",
		Options: []string{"ascii4", "binary2", "bcd2", "NAPS", "visa"},
		Default: "binary2",
	}
	if err := survey.AskOne(headerPrompt, &headerType); err != nil {
		return err
	}

	// 4. Select PCAP/Stream Capture File from Browsing List
	pcapFiles := utils.FindAvailablePCAPFiles()
	var selectedPCAP string
	pcapPrompt := &survey.Select{
		Message: "4. Select Input PCAP/Stream Capture File:",
		Options: pcapFiles,
		Default: pcapFiles[0],
	}
	if err := survey.AskOne(pcapPrompt, &selectedPCAP); err != nil {
		return err
	}

	streamFile := selectedPCAP
	if selectedPCAP == "[Browse Custom Path...]" {
		inputPrompt := &survey.Input{
			Message: "   Enter custom path to stream/capture file:",
		}
		if err := survey.AskOne(inputPrompt, &streamFile); err != nil {
			return err
		}
	}
	streamFile = strings.TrimSpace(streamFile)
	if streamFile == "" {
		return fmt.Errorf("stream capture file path cannot be empty")
	}

	// 5. Inspect PCAP for directional traffic options if applicable
	selectedDir := analyzer.TrafficDirection{Mode: "all"}
	dirs, dirErr := analyzer.InspectPCAPDirections(streamFile)
	if dirErr == nil && len(dirs) > 1 {
		options := make([]string, len(dirs))
		dirMap := make(map[string]analyzer.TrafficDirection)
		defaultIdx := 0

		for i, d := range dirs {
			options[i] = d.Label
			dirMap[d.Label] = d
			// For Mock Routes, prefer Outgoing Responses (Src Port mode)
			if isMockRouteGoal && d.Mode == "src" {
				defaultIdx = i
			} else if !isMockRouteGoal && d.Mode == "dst" {
				defaultIdx = i
			}
		}

		var chosenLabel string
		dirPrompt := &survey.Select{
			Message: "5. Select Traffic Direction / Target Port Filter:",
			Options: options,
			Default: options[defaultIdx],
		}
		if err := survey.AskOne(dirPrompt, &chosenLabel); err == nil {
			if d, ok := dirMap[chosenLabel]; ok {
				selectedDir = d
				fmt.Printf("   ✓ Selected direction filter: %s\n", selectedDir.Label)
			}
		}
	}

	// 6. Select Target Transaction / Mock Route JSON file
	defaultTxFile := config.GetConfig().GetFile()
	if defaultTxFile == "" {
		if isMockRouteGoal {
			defaultTxFile = "./transactions/mock_routes.json"
		} else {
			defaultTxFile = "./transactions/transaction.json"
		}
	}
	var outputTxFile string
	outputPrompt := &survey.Input{
		Message: "6. Target Transaction/Route JSON output file:",
		Default: defaultTxFile,
	}
	if err := survey.AskOne(outputPrompt, &outputTxFile); err != nil {
		return err
	}
	outputTxFile = strings.TrimSpace(outputTxFile)

	return ac.runAnalysis(streamFile, spec, headerType, outputTxFile, selectedDir, isMockRouteGoal)
}

func (ac *AnalyzeCommand) runAnalysis(
	streamFile string,
	spec *iso8583.MessageSpec,
	headerType string,
	outputTxFile string,
	dir analyzer.TrafficDirection,
	isMockRouteGoal ...bool,
) error {
	mockRouteGoal := len(isMockRouteGoal) > 0 && isMockRouteGoal[0]

	fmt.Printf("\n🔍 Extracting ISO8583 messages from '%s' (Header: %s)...\n", streamFile, headerType)

	streamAnalyzer := analyzer.NewStreamAnalyzer(spec)
	extractedMessages, err := streamAnalyzer.ExtractMessagesFromFileWithDirection(streamFile, headerType, dir)
	if err != nil {
		return fmt.Errorf("extraction error: %w", err)
	}

	if len(extractedMessages) == 0 {
		return fmt.Errorf("no valid ISO8583 messages could be extracted from '%s' with header '%s'", streamFile, headerType)
	}

	fmt.Printf("✓ Successfully extracted %d ISO8583 messages!\n", len(extractedMessages))

	// Group messages into flows
	flows := streamAnalyzer.AggregateFlows(extractedMessages)
	fmt.Printf("✓ Aggregated traffic into %d distinct transaction flow(s):\n", len(flows))

	for key, flow := range flows {
		fmt.Printf("  • Flow [%s]: MTI=%s, DE3=%s (%d messages)\n", key, flow.MTI, flow.DE3, flow.Count)
	}

	// Perform variance / mock route analysis
	varianceEngine := analyzer.NewVarianceEngine(spec)
	var newItems []config.ConfigItem

	txCount := 0
	dsCount := 0

	for _, flow := range flows {
		var results []*analyzer.VarianceResult
		var err error

		if mockRouteGoal {
			results, err = varianceEngine.AnalyzeFlowToMockRoutes(flow)
		} else {
			results, err = varianceEngine.AnalyzeFlow(flow)
		}

		if err != nil {
			fmt.Printf("⚠️ Warning: Failed variance analysis on flow MTI %s DE3 %s: %v\n", flow.MTI, flow.DE3, err)
			continue
		}

		for _, res := range results {
			newItems = append(newItems, res.Transaction)
			txCount++

			if res.Dataset.Name != "" && len(res.Dataset.Data) > 0 {
				newItems = append(newItems, res.Dataset)
				dsCount++
			}
		}
	}

	if len(newItems) == 0 {
		return fmt.Errorf("no transaction templates, mock routes, or datasets could be generated")
	}

	// Save to target transaction file
	if outputTxFile == "" {
		if mockRouteGoal {
			outputTxFile = "./transactions/mock_routes.json"
		} else {
			outputTxFile = "./transactions/transaction.json"
		}
	}
	if err := saveConfigItemsToFile(outputTxFile, newItems); err != nil {
		return fmt.Errorf("failed to save generated items to '%s': %w", outputTxFile, err)
	}

	fmt.Printf("\n================================================================================")
	fmt.Printf("\n 🎉 ANALYSIS COMPLETE & SAVED TO: %s 🟢\n", outputTxFile)
	if mockRouteGoal {
		fmt.Printf(" Added %d new Mock Route ConfigItem(s).\n", txCount)
	} else {
		fmt.Printf(" Added %d new ConfigItem(s) (%d transactions, %d datasets).\n",
			len(newItems), txCount, dsCount)
	}
	fmt.Println("================================================================================")

	return nil
}

func saveConfigItemsToFile(filename string, newItems []config.ConfigItem) error {
	// Create directory if needed
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	var existingItems []config.ConfigItem
	if data, err := os.ReadFile(filename); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &existingItems)
	}

	// Filter out duplicate names
	itemMap := make(map[string]config.ConfigItem)
	for _, item := range existingItems {
		itemMap[item.Name] = item
	}
	for _, item := range newItems {
		itemMap[item.Name] = item
	}

	mergedItems := make([]config.ConfigItem, 0, len(itemMap))
	for _, item := range itemMap {
		mergedItems = append(mergedItems, item)
	}

	outputBytes, err := json.MarshalIndent(mergedItems, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged config items: %w", err)
	}

	return os.WriteFile(filename, outputBytes, 0644)
}
