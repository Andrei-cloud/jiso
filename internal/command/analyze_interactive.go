package command

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	json "github.com/goccy/go-json"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/utils"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
)

// AnalyzeCommand provides interactive or direct reverse-engineering of PCAP / stream captures

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
		Options: []string{"ascii4", "binary2", "binary4", "bcd2", "NAPS", "visa"},
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

	// Sort flow keys deterministically
	flowKeys := make([]string, 0, len(flows))
	flowMap := make(map[string]string) // option label -> flow key
	var multiOptions []string

	for key, flow := range flows {
		label := fmt.Sprintf("Flow [%s]: MTI=%s, DE3=%s, DE22=%s (%d messages)", key, flow.MTI, flow.DE3, flow.DE22, flow.Count)
		fmt.Printf("  • %s\n", label)
		flowKeys = append(flowKeys, key)
		flowMap[label] = key
		multiOptions = append(multiOptions, label)
	}

	selectedFlowKeys := flowKeys
	if len(multiOptions) > 0 {
		var selectedOptions []string
		exportPrompt := &survey.MultiSelect{
			Message: "Select flow(s) to export:",
			Options: multiOptions,
			Default: multiOptions,
		}
		if err := survey.AskOne(exportPrompt, &selectedOptions); err == nil && len(selectedOptions) > 0 {
			selectedFlowKeys = make([]string, 0, len(selectedOptions))
			for _, opt := range selectedOptions {
				if k, ok := flowMap[opt]; ok {
					selectedFlowKeys = append(selectedFlowKeys, k)
				}
			}
		}
	}

	// Perform variance / mock route analysis
	varianceEngine := analyzer.NewVarianceEngine(spec)
	var newItems []config.ConfigItem

	txCount := 0
	dsCount := 0

	for _, key := range selectedFlowKeys {
		flow, ok := flows[key]
		if !ok {
			continue
		}
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

	// Create a custom serializable struct to preserve sorted map order during JSON marshaling
	type serializableConfigItem struct {
		Type           config.ConfigDiscriminator `json:"type,omitempty"`
		Name           string                     `json:"name"`
		Description    string                     `json:"description,omitempty"`
		Fields         interface{}                `json:"fields,omitempty"`
		Dataset        interface{}                `json:"dataset,omitempty"`
		Data           interface{}                `json:"data,omitempty"`
		DatasetName    string                     `json:"dataset_name,omitempty"`
		Steps          json.RawMessage            `json:"steps,omitempty"`
		MatchFields    interface{}                `json:"match_fields,omitempty"`
		RequiredFields []string                   `json:"required_fields,omitempty"`
		EchoFields     []int                      `json:"echo_fields,omitempty"`
		ResponseMTI    string                     `json:"response_mti,omitempty"`
		ResponseFields interface{}                `json:"response_fields,omitempty"`
		DelayMs        int                        `json:"delay_ms,omitempty"`
		LatencyMs      int                        `json:"latency_ms,omitempty"`
		JitterMs       int                        `json:"jitter_ms,omitempty"`
		DropConnection bool                       `json:"drop_connection,omitempty"`
	}

	mergedItems := make([]serializableConfigItem, 0, len(itemMap))
	for _, item := range itemMap {
		sItem := serializableConfigItem{
			Type:           item.Type,
			Name:           item.Name,
			Description:    item.Description,
			DatasetName:    item.DatasetName,
			Steps:          item.Steps,
			RequiredFields: item.RequiredFields,
			EchoFields:     item.EchoFields,
			ResponseMTI:    item.ResponseMTI,
			DelayMs:        item.DelayMs,
			LatencyMs:      item.LatencyMs,
			JitterMs:       item.JitterMs,
			DropConnection: item.DropConnection,
		}

		if len(item.Fields) > 0 {
			var parsed interface{}
			if err := json.Unmarshal(item.Fields, &parsed); err == nil {
				sorted := config.SortMapKeysRecursively(parsed)
				if sortedBytes, sErr := json.Marshal(sorted); sErr == nil {
					sItem.Fields = json.RawMessage(sortedBytes)
				} else {
					sItem.Fields = item.Fields
				}
			} else {
				sItem.Fields = item.Fields
			}
		}
		if item.Dataset != nil {
			sItem.Dataset = item.Dataset
		}
		if item.Data != nil {
			sItem.Data = item.Data
		}
		if item.MatchFields != nil {
			sorted := config.SortInterfaceMapKeys(item.MatchFields)
			if sortedBytes, sErr := json.Marshal(sorted); sErr == nil {
				sItem.MatchFields = json.RawMessage(sortedBytes)
			} else {
				sItem.MatchFields = item.MatchFields
			}
		}
		if item.ResponseFields != nil {
			sorted := config.SortStringMapKeys(item.ResponseFields)
			if sortedBytes, sErr := json.Marshal(sorted); sErr == nil {
				sItem.ResponseFields = json.RawMessage(sortedBytes)
			} else {
				sItem.ResponseFields = item.ResponseFields
			}
		}

		mergedItems = append(mergedItems, sItem)
	}

	// Sort items by Name for deterministic order
	sort.Slice(mergedItems, func(i, j int) bool {
		return mergedItems[i].Name < mergedItems[j].Name
	})

	outputBytes, err := json.MarshalIndent(mergedItems, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged config items: %w", err)
	}

	return os.WriteFile(filename, outputBytes, 0644)
}
