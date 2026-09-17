// scenario_runner_dataset.go is the scenario step's dataset seam: which
// dataset a step reads and how {{data.x}} / {{card.x}} / {{context.x}}
// variables resolve against the step's randomly picked row. It lives
// apart from scenario_runner_step.go (the execution leg) so each file
// keeps one concern (line-budget split).
package transactions

import (
	"math/rand"
	"strings"
	"time"
)

// resolveStepDataset picks the dataset for this step (the referenced transaction's
// dataset, the scenario dataset, or a default) and selects a random row for it.
func (sr *ScenarioRunner) resolveStepDataset(step ScenarioStep, scenarioDatasetName string) string {
	// Re-initialize selectedDatasets map on every step run to ensure random selection per step
	sr.selectedDatasets = make(map[string]map[string]string)

	var datasetName string
	if step.UseTransactionID != "" {
		if t, err := sr.tc.findTransaction(step.UseTransactionID); err == nil {
			datasetName = t.DatasetName
		}
	}
	if datasetName == "" {
		datasetName = scenarioDatasetName
	}
	if datasetName == "" && len(sr.tc.datasets) > 0 {
		if _, ok := sr.tc.datasets["card_pool"]; ok {
			datasetName = "card_pool"
		} else {
			for name := range sr.tc.datasets {
				datasetName = name
				break
			}
		}
	}

	if datasetName != "" {
		dataset, err := sr.tc.GetDataset(datasetName)
		if err == nil && len(dataset.Data) > 0 {
			randomIndex := rand.Intn(len(dataset.Data))
			sr.selectedDatasets[datasetName] = dataset.Data[randomIndex]
		}
	}

	return datasetName
}

func (sr *ScenarioRunner) injectVariables(val, datasetName string) string {
	replaceDatasetVar := func(m string, match []string) string {
		return sr.resolveDatasetVar(m, match, datasetName)
	}

	val = dataRegex.ReplaceAllStringFunc(val, func(m string) string {
		return replaceDatasetVar(m, dataRegex.FindStringSubmatch(m))
	})
	val = cardRegex.ReplaceAllStringFunc(val, func(m string) string {
		return replaceDatasetVar(m, cardRegex.FindStringSubmatch(m))
	})
	val = contextRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := contextRegex.FindStringSubmatch(m)
		if len(match) > 1 {
			key := match[1]
			if v, ok := sr.sessionState[key]; ok && strings.TrimSpace(v) != "" {
				return v
			}
			switch key {
			case "AuthId", "auth_code":
				return "000000"
			case "OrigMTI":
				return "0100"
			case "OrigSTAN":
				return "000001"
			case "OrigDateTime":
				return time.Now().Format("0102150405")
			case "OrigAcquirer", "OrigForwarder":
				return "000000"
			}
		}
		return m
	})
	return val
}

// resolveDatasetVar substitutes a dataset/card placeholder with the value from the
// step's selected dataset row, choosing a random row on first use.
func (sr *ScenarioRunner) resolveDatasetVar(m string, match []string, datasetName string) string {
	if len(match) <= 1 {
		return m
	}

	key := match[1]

	// Check if we already have selected an item for this dataset
	selectedRow, ok := sr.selectedDatasets[datasetName]
	if !ok && datasetName != "" {
		// Retrieve dataset and choose a random row
		dataset, err := sr.tc.GetDataset(datasetName)
		if err == nil && len(dataset.Data) > 0 {
			randomIndex := rand.Intn(len(dataset.Data))
			selectedRow = dataset.Data[randomIndex]
			sr.selectedDatasets[datasetName] = selectedRow
			ok = true
		}
	}

	if ok {
		if v, exist := selectedRow[key]; exist {
			return v
		}
	}

	return m
}
