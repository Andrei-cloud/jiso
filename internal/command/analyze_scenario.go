package command

import (
	"fmt"
	"strings"

	"jiso/internal/analyzer"
	"jiso/internal/config"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
)

func (ac *AnalyzeCommand) runScenarioAnalysis(
	streamFile string,
	spec *iso8583.MessageSpec,
	headerType string,
	outputTxFile string,
	unsecure bool,
) error {
	fmt.Printf("\n🔍 Extracting & Correlating ISO8583 request/response pairs from '%s' (Header: %s)...\n", streamFile, headerType)

	streamAnalyzer := analyzer.NewStreamAnalyzer(spec)
	annotatedMsgs, err := streamAnalyzer.ExtractAnnotatedMessagesFromFile(streamFile, headerType)
	if err != nil {
		return fmt.Errorf("extraction error: %w", err)
	}

	if len(annotatedMsgs) == 0 {
		return fmt.Errorf("no valid ISO8583 messages could be extracted from '%s'", streamFile)
	}

	correlator := analyzer.NewCorrelator(unsecure)
	pairs, err := correlator.Correlate(annotatedMsgs)
	if err != nil {
		return fmt.Errorf("correlation error: %w", err)
	}

	if len(pairs) == 0 {
		return fmt.Errorf("no request/response pairs could be correlated from '%s'", streamFile)
	}

	fmt.Printf("✓ Successfully correlated %d transaction pair(s) from traffic:\n", len(pairs))

	var options []string
	pairMap := make(map[string]int)

	for i, pair := range pairs {
		optLabel := fmt.Sprintf("[%d] %s", i+1, pair.Label)
		options = append(options, optLabel)
		pairMap[optLabel] = i
	}

	var selectedOptions []string
	pairPrompt := &survey.MultiSelect{
		Message: "Select transaction pair(s) to include in scenario:",
		Options: options,
		Default: options,
	}
	if err := survey.AskOne(pairPrompt, &selectedOptions); err != nil || len(selectedOptions) == 0 {
		return fmt.Errorf("no pairs selected for scenario scaffold")
	}

	var selectedPairs []*analyzer.CorrelatedPair
	includeReversals := make(map[int]bool)

	for newIdx, optLabel := range selectedOptions {
		origIdx := pairMap[optLabel]
		pair := pairs[origIdx]
		selectedPairs = append(selectedPairs, pair)

		promptMsg := fmt.Sprintf("Include Reversal step for pair #%d (%s)?", origIdx+1, pair.Label)
		defaultRev := pair.Reversal != nil
		var incRev bool
		revPrompt := &survey.Confirm{
			Message: promptMsg,
			Default: defaultRev,
		}
		_ = survey.AskOne(revPrompt, &incRev)
		includeReversals[newIdx] = incRev
	}

	var generateMockRoutes bool
	mockPrompt := &survey.Confirm{
		Message: "Generate corresponding Mock Server Routes for responses?",
		Default: true,
	}
	if err := survey.AskOne(mockPrompt, &generateMockRoutes); err != nil {
		generateMockRoutes = false
	}

	defaultScenName := "PCAP Captured Test Scenario"
	var scenarioName string
	namePrompt := &survey.Input{
		Message: "Enter Scenario Name:",
		Default: defaultScenName,
	}
	if err := survey.AskOne(namePrompt, &scenarioName); err != nil || strings.TrimSpace(scenarioName) == "" {
		scenarioName = defaultScenName
	}

	builder := analyzer.NewScenarioBuilder(spec, unsecure)
	opts := analyzer.ScenarioScaffoldOptions{
		ScenarioName:       scenarioName,
		IncludeReversals:   includeReversals,
		GenerateMockRoutes: generateMockRoutes,
		Unsecure:           unsecure,
	}

	scaffold, err := builder.Build(selectedPairs, opts)
	if err != nil {
		return fmt.Errorf("failed to build scenario scaffold: %w", err)
	}

	var newItems []config.ConfigItem
	newItems = append(newItems, scaffold.Transactions...)
	newItems = append(newItems, scaffold.Datasets...)
	newItems = append(newItems, scaffold.Scenario)
	if generateMockRoutes {
		newItems = append(newItems, scaffold.MockRoutes...)
	}

	if outputTxFile == "" {
		outputTxFile = "./transactions/transaction.json"
	}

	if err := saveConfigItemsToFile(outputTxFile, newItems); err != nil {
		return fmt.Errorf("failed to save generated items to '%s': %w", outputTxFile, err)
	}

	fmt.Printf("\n================================================================================")
	fmt.Printf("\n 🎉 SCENARIO SCAFFOLD COMPLETE & SAVED TO: %s 🟢\n", outputTxFile)
	fmt.Printf(" Created Scenario: '%s' (%d steps)\n", scenarioName, len(selectedPairs))
	fmt.Printf(" Added %d Transaction Templates, %d Mock Routes.\n", len(scaffold.Transactions), len(scaffold.MockRoutes))
	fmt.Println("================================================================================")

	return nil
}
