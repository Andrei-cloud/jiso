package command

import (
	"jiso/internal/client"
	"jiso/internal/config"
	"jiso/internal/metrics"
	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/moov-io/iso8583"
)

// Factory creates commands with properly injected dependencies
type Factory struct {
	service      *service.Service
	transactions transactions.Repository
	networkStats *metrics.NetworkingStats
	controller   WorkerController
	cliCtrl      CLIController
	clientCfg    *client.ClientConfig
}

// NewFactory creates a new command factory
func NewFactory(
	svc *service.Service,
	tx transactions.Repository,
	networkStats *metrics.NetworkingStats,
	controller WorkerController,
) *Factory {
	var cliCtrl CLIController
	if cc, ok := controller.(CLIController); ok {
		cliCtrl = cc
	}
	return &Factory{
		service:      svc,
		transactions: tx,
		networkStats: networkStats,
		controller:   controller,
		cliCtrl:      cliCtrl,
		clientCfg:    client.NewClientConfig(config.GetConfig().GetHost(), config.GetConfig().GetPort(), nil),
	}
}

// CreateConnectCommand creates a connect command
func (f *Factory) CreateConnectCommand() Command {
	return &ConnectCommand{
		Tc:   f.transactions,
		Svc:  f.service,
		Ctrl: f.cliCtrl,
	}
}

// CreateDisconnectCommand creates a disconnect command
func (f *Factory) CreateDisconnectCommand() Command {
	return &DisconnectCommand{
		Svc: f.service,
	}
}

// CreateSendCommand creates a send command
func (f *Factory) CreateSendCommand() Command {
	return &SendCommand{
		Tc:           f.transactions,
		Svc:          f.service,
		networkStats: f.networkStats,
	}
}

// CreateBackgroundCommand creates a background command
func (f *Factory) CreateBackgroundCommand() Command {
	return &BackgroundCommand{
		Tc:  f.transactions,
		Svc: f.service,
		Wrk: f.controller,
	}
}

// CreateStressTestCommand creates a stress test command
func (f *Factory) CreateStressTestCommand() Command {
	return &StressTestCommand{
		Tc:  f.transactions,
		Svc: f.service,
		Wrk: f.controller,
	}
}

// CreateListCommand creates a list command
func (f *Factory) CreateListCommand() Command {
	return &ListCommand{
		Tc: f.transactions,
	}
}

// CreateInfoCommand creates an info command
func (f *Factory) CreateInfoCommand() Command {
	return &InfoCommand{
		Tc: f.transactions,
	}
}

// CreateDbStatsCommand creates a database stats command
func (f *Factory) CreateDbStatsCommand() Command {
	return &DbStatsCommand{}
}

// CreateScenarioCommand creates a scenarios list command
func (f *Factory) CreateScenarioCommand() Command {
	return &ScenarioCommand{
		Tc: f.transactions,
	}
}

// CreateRunScenarioCommand creates a scenario runner command
func (f *Factory) CreateRunScenarioCommand() Command {
	return &RunScenarioCommand{
		Tc:  f.transactions,
		Svc: f.service,
	}
}

// CreateInitSpecCommand creates an init-spec command
func (f *Factory) CreateInitSpecCommand() Command {
	return &InitSpecCommand{}
}

// CreateInitTxCommand creates an init-tx command
func (f *Factory) CreateInitTxCommand() Command {
	return &InitTxCommand{}
}

// CreateHelpCommand creates a help command
func (f *Factory) CreateHelpCommand() Command {
	return &HelpCommand{Ctrl: f.cliCtrl}
}

// CreateVersionCommand creates a version command
func (f *Factory) CreateVersionCommand() Command {
	return &VersionCommand{Ctrl: f.cliCtrl}
}

// CreateClearCommand creates a clear command
func (f *Factory) CreateClearCommand() Command {
	return &ClearCommand{Ctrl: f.cliCtrl}
}

// CreateExitCommand creates an exit command
func (f *Factory) CreateExitCommand() Command {
	return &ExitCommand{}
}

// CreateStatsCommand creates a stats command
func (f *Factory) CreateStatsCommand() Command {
	return &StatsCommand{Ctrl: f.cliCtrl}
}

// CreateStopAllCommand creates a stop-all command
func (f *Factory) CreateStopAllCommand() Command {
	return &StopAllCommand{Ctrl: f.cliCtrl}
}

// CreateStopCommand creates a stop command
func (f *Factory) CreateStopCommand() Command {
	return &StopCommand{Ctrl: f.cliCtrl}
}

// CreateReloadCommand creates a reload command
func (f *Factory) CreateReloadCommand() Command {
	return &ReloadCommand{Ctrl: f.cliCtrl}
}

// CreateTargetCommand creates a target command
func (f *Factory) CreateTargetCommand() Command {
	return NewTargetCommand(f.clientCfg, f.service)
}

// CreateSpecCommand creates a spec command
func (f *Factory) CreateSpecCommand() Command {
	return &SpecCommand{
		Svc:  f.service,
		Tc:   f.transactions,
		Ctrl: f.cliCtrl,
	}
}

// CreateTxCommand creates a tx command
func (f *Factory) CreateTxCommand() Command {
	return &TxCommand{
		Svc:  f.service,
		Ctrl: f.cliCtrl,
	}
}

// CreateServerCommand creates a server command
func (f *Factory) CreateServerCommand() Command {
	var spec *iso8583.MessageSpec
	var routes []config.MockRouteConfig

	if f.service != nil {
		spec = f.service.GetSpec()
	}
	if f.transactions != nil {
		routes = f.transactions.GetMockRoutes()
	}
	return NewServerCommand(spec, routes, f.transactions)
}
// CreateAnalyzeCommand creates an analyze command
func (f *Factory) CreateAnalyzeCommand() Command {
	var spec *iso8583.MessageSpec
	if f.service != nil {
		spec = f.service.GetSpec()
	}
	return NewAnalyzeCommand(spec, f.transactions)
}
