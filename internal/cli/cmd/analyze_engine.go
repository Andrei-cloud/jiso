package cmd

import (
	"errors"

	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
	"jiso/internal/app"
	cfg "jiso/internal/config"
)

// headlessAnalyzeResult carries the engine output, the items to persist, and
// the file they persist to.
type headlessAnalyzeResult struct {
	output     *app.AnalyzeOutput
	items      []cfg.Item
	outputFile string
}

// runHeadlessAnalyze delegates to the shared engine orchestration in
// internal/app (app.RunAnalyzeEngine, SCR-510 extraction — the §J TUI
// wizard drives the same entry points) and maps the typed config-class
// errors into this command's exit taxonomy.
func runHeadlessAnalyze(
	mode string,
	pcapPath string,
	header string,
	spec *iso8583.MessageSpec,
	unsecure bool,
	dir analyzer.TrafficDirection,
) (*headlessAnalyzeResult, error) {
	outputFile := headlessTxOutput(mode)

	output, items, err := app.RunAnalyzeEngine(app.AnalyzeEngineOptions{
		Mode:         mode,
		PcapPath:     pcapPath,
		HeaderType:   header,
		Spec:         spec,
		Unsecure:     unsecure,
		Direction:    dir,
		OutputFile:   outputFile,
		ScenarioName: headlessScenarioName,
	})
	if err != nil {
		return nil, mapAppConfigError(err)
	}

	return &headlessAnalyzeResult{output: output, items: items, outputFile: outputFile}, nil
}

// mapAppConfigError folds an internal/app config-class error into the
// exit-taxonomy type (identical message formatting: both render
// "err: path" and deduplicate the path when the message already names
// it).
func mapAppConfigError(err error) error {
	var cfgErr *app.ConfigError
	if errors.As(err, &cfgErr) {
		return &ExitConfigError{Path: cfgErr.Path, Err: cfgErr.Err}
	}

	return err
}
