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
	var cleanArgs []string
	unsecure := false
	for _, arg := range ac.args {
		if arg == "--unsecure" || arg == "-u" || arg == "unsecure" {
			unsecure = true
		} else {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	if len(cleanArgs) == 0 {
		return ac.promptAnalyze()
	}

	// Non-interactive command format: analyze [--unsecure] <streamFile> [specFile] [headerType] [outputTxFile]
	streamFile := cleanArgs[0]
	specPath := config.GetConfig().GetSpec()
	headerType := "binary2"
	outputTxFile := config.GetConfig().GetFile()

	if len(cleanArgs) > 1 && cleanArgs[1] != "" {
		specPath = cleanArgs[1]
	}
	if len(cleanArgs) > 2 && cleanArgs[2] != "" {
		headerType = cleanArgs[2]
	}
	if len(cleanArgs) > 3 && cleanArgs[3] != "" {
		outputTxFile = cleanArgs[3]
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

	return ac.runAnalysis(streamFile, spec, headerType, outputTxFile, analyzer.TrafficDirection{Mode: "all"}, unsecure)
}
