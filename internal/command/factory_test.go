package command

import (
	"testing"

	"jiso/internal/metrics"
	"jiso/internal/service"
	"jiso/internal/transactions"
)

func TestNewFactory(t *testing.T) {
	// Create mock dependencies
	svc := &service.Service{}
	tx := &transactions.TransactionCollection{}
	networkStats := metrics.NewNetworkingStats()
	var controller WorkerController

	factory := NewFactory(svc, tx, networkStats, controller)

	if factory == nil {
		t.Fatal("NewFactory returned nil")
	}

	if factory.service != svc {
		t.Error("service not set correctly")
	}

	if factory.transactions != tx {
		t.Error("transactions not set correctly")
	}

	if factory.networkStats != networkStats {
		t.Error("networkStats not set correctly")
	}

	if factory.controller != controller {
		t.Error("controller not set correctly")
	}
}

func TestCreateCommands(t *testing.T) {
	// Create mock dependencies
	svc := &service.Service{}
	tx := &transactions.TransactionCollection{}
	networkStats := metrics.NewNetworkingStats()
	var controller WorkerController

	factory := NewFactory(svc, tx, networkStats, controller)

	// Test CreateConnectCommand
	connectCmd := factory.CreateConnectCommand()
	if connectCmd == nil {
		t.Error("CreateConnectCommand returned nil")
	}
	if _, ok := connectCmd.(*ConnectCommand); !ok {
		t.Error("CreateConnectCommand did not return ConnectCommand")
	}

	// Test CreateDisconnectCommand
	disconnectCmd := factory.CreateDisconnectCommand()
	if disconnectCmd == nil {
		t.Error("CreateDisconnectCommand returned nil")
	}
	if _, ok := disconnectCmd.(*DisconnectCommand); !ok {
		t.Error("CreateDisconnectCommand did not return DisconnectCommand")
	}

	// Test CreateSendCommand
	sendCmd := factory.CreateSendCommand()
	if sendCmd == nil {
		t.Error("CreateSendCommand returned nil")
	}
	if _, ok := sendCmd.(*SendCommand); !ok {
		t.Error("CreateSendCommand did not return SendCommand")
	}

	// Test CreateBackgroundCommand
	backgroundCmd := factory.CreateBackgroundCommand()
	if backgroundCmd == nil {
		t.Error("CreateBackgroundCommand returned nil")
	}
	if _, ok := backgroundCmd.(*BackgroundCommand); !ok {
		t.Error("CreateBackgroundCommand did not return BackgroundCommand")
	}

	// Test CreateStressTestCommand
	stressTestCmd := factory.CreateStressTestCommand()
	if stressTestCmd == nil {
		t.Error("CreateStressTestCommand returned nil")
	}
	if _, ok := stressTestCmd.(*StressTestCommand); !ok {
		t.Error("CreateStressTestCommand did not return StressTestCommand")
	}

	// Test CreateListCommand
	listCmd := factory.CreateListCommand()
	if listCmd == nil {
		t.Error("CreateListCommand returned nil")
	}
	if _, ok := listCmd.(*ListCommand); !ok {
		t.Error("CreateListCommand did not return ListCommand")
	}

	// Test CreateInfoCommand
	infoCmd := factory.CreateInfoCommand()
	if infoCmd == nil {
		t.Error("CreateInfoCommand returned nil")
	}
	if _, ok := infoCmd.(*InfoCommand); !ok {
		t.Error("CreateInfoCommand did not return InfoCommand")
	}
}
