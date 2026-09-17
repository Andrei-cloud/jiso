// analyzeflows.go resolves the §J run's flow selections to concrete traffic
// directions. The transactions/datasets and mock-routes goals analyse exactly
// the selected (port, direction) units; the scenario goal correlates a whole
// port (requests and responses), so a pick at either half pulls the port in
// once, with independent direction selection.
package app

import (
	"jiso/internal/analyzer"
)

// FlowSelection is one flow the §J run should analyze: a server port and a
// direction (analyzer.DirectionDst or DirectionSrc). The
// operator picks directions independently for the transactions/datasets and
// mock-routes goals; the scenario goal folds a port's two directions into a
// single correlated unit.
type FlowSelection struct {
	Port int    `json:"port"`
	Dir  string `json:"dir"`
}

// selectAnalyzeFlows resolves the operator's flow selections to concrete
// traffic directions. The transactions/datasets and mock-routes goals
// analyze EXACTLY the selected (port, direction) units — both halves of a
// port when both are picked. The scenario goal correlates a whole port
// (it reads both directions at the port), so one direction picked at a port
// pulls the port in once. An empty selection keeps the auto-pick.
func selectAnalyzeFlows(flows []analyzer.TrafficDirection, sels []FlowSelection, mode, pcapPath string) ([]analyzer.TrafficDirection, error) {
	if len(sels) == 0 {
		dir, err := PickAnalyzeFlow(flows, 0, pcapPath)
		if err != nil {
			return nil, err
		}

		return []analyzer.TrafficDirection{dir}, nil
	}
	if mode == AnalyzeModeScenario {
		return selectScenarioPorts(flows, sels, pcapPath)
	}

	return selectFlowDirs(flows, sels, pcapPath)
}

// selectScenarioPorts folds per-direction picks into the ports a scenario
// run correlates: a port is analyzed once, when either half is selected.
func selectScenarioPorts(flows []analyzer.TrafficDirection, sels []FlowSelection, pcapPath string) ([]analyzer.TrafficDirection, error) {
	seen := make(map[int]bool, len(sels))
	selected := make([]analyzer.TrafficDirection, 0, len(sels))
	for _, s := range sels {
		if seen[s.Port] {
			continue
		}
		seen[s.Port] = true
		dir, err := PickAnalyzeFlow(flows, s.Port, pcapPath)
		if err != nil {
			return nil, err
		}
		selected = append(selected, dir)
	}

	return selected, nil
}

// selectFlowDirs resolves each selected (port, direction) to its exact
// traffic direction, so a port's requests and responses are analyzed only
// when each is selected, with independent direction selection.
func selectFlowDirs(flows []analyzer.TrafficDirection, sels []FlowSelection, pcapPath string) ([]analyzer.TrafficDirection, error) {
	seen := make(map[FlowSelection]bool, len(sels))
	selected := make([]analyzer.TrafficDirection, 0, len(sels))
	for _, s := range sels {
		if seen[s] {
			continue
		}
		seen[s] = true
		dir, err := pickAnalyzeFlowDir(flows, s.Port, s.Dir, pcapPath)
		if err != nil {
			return nil, err
		}
		selected = append(selected, dir)
	}

	return selected, nil
}
