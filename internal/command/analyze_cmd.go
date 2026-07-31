package command

import (
	"fmt"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"

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

