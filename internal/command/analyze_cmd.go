package command

import (
	"fmt"
	"strings"

	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// AnalyzeCommand provides interactive or direct reverse-engineering of PCAP / stream captures.
type AnalyzeCommand struct {
	spec *iso8583.MessageSpec
	tc   transactions.Repository
	args []string
}

// NewAnalyzeCommand creates a new AnalyzeCommand instance.
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
	var spec *iso8583.MessageSpec
	unsecure := false
	isScenario := false
	headerType := config.GetConfig().GetHeader()
	if headerType == "" {
		headerType = "binary2"
	}

	for i := 0; i < len(ac.args); i++ {
		arg := ac.args[i]
		switch {
		case arg == "--unsecure" || arg == "-u" || arg == "unsecure":
			unsecure = true
		case arg == "--scenario" || arg == "-s" || arg == "scenario" || arg == "-S":
			isScenario = true
		case arg == "--header" || arg == "-H" || arg == "-header":
			if i+1 < len(ac.args) {
				i++
				headerType = ac.args[i]
			}
		case strings.HasPrefix(arg, "--header="):
			headerType = strings.TrimPrefix(arg, "--header=")
		case strings.HasPrefix(arg, "-header="):
			headerType = strings.TrimPrefix(arg, "-header=")
		case strings.HasPrefix(arg, "-H="):
			headerType = strings.TrimPrefix(arg, "-H=")
		default:
			cleanArgs = append(cleanArgs, arg)
		}
	}

	if len(cleanArgs) == 0 {
		return ac.promptAnalyze()
	}

	// Non-interactive command format: analyze [--unsecure] [--scenario] <streamFile> [specFile] [headerType] [outputTxFile]
	streamFile := cleanArgs[0]
	specPath := config.GetConfig().GetSpec()
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

	switch {
	case specPath != "":
		var err error
		spec, err = utils.CreateSpecFromFile(specPath)
		if err != nil {
			return fmt.Errorf("failed to load spec from '%s': %w", specPath, err)
		}
	case ac.spec != nil:
		spec = ac.spec
	default:
		spec = utils.GetDefaultSpec()
	}

	if isScenario {
		return ac.runScenarioAnalysis(streamFile, spec, headerType, outputTxFile, unsecure)
	}

	return ac.runAnalysis(streamFile, spec, headerType, outputTxFile, analyzer.TrafficDirection{Mode: "all"}, unsecure)
}
