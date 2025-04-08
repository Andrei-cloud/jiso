package command

import (
	"jiso/internal/service"
	"jiso/internal/transactions"
)

// Factory creates commands with properly injected dependencies
type Factory struct {
	service      *service.Service
	transactions transactions.Repository
	controller   WorkerController
}

// NewFactory creates a new command factory
func NewFactory(
	svc *service.Service,
	tx transactions.Repository,
	controller WorkerController,
) *Factory {
	return &Factory{
		service:      svc,
		transactions: tx,
		controller:   controller,
	}
}

// CreateConnectCommand creates a connect command
func (f *Factory) CreateConnectCommand() Command {
	return &ConnectCommand{
		Svc: f.service,
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
		Tc:  f.transactions,
		Svc: f.service,
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
