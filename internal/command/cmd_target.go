package command

import (
	"fmt"
	"strings"

	"jiso/internal/client"
	"jiso/internal/config"
	"jiso/internal/service"
)

// TargetCommand handles dynamic target switching REPL commands
type TargetCommand struct {
	ClientCfg *client.ClientConfig
	Svc       *service.Service
}

func (tc *TargetCommand) Name() string     { return "target" }
func (tc *TargetCommand) Synopsis() string { return "Set network target address (target <host:port>)" }

func (tc *TargetCommand) Execute() error {
	fmt.Println(tc.GetStatus())
	return nil
}

// NewTargetCommand creates a new TargetCommand instance
func NewTargetCommand(cfg *client.ClientConfig, svc *service.Service) *TargetCommand {
	return &TargetCommand{
		ClientCfg: cfg,
		Svc:       svc,
	}
}

// SetTarget updates the target endpoint dynamically and reconnects if active
func (tc *TargetCommand) SetTarget(targetAddr string) error {
	if tc.ClientCfg == nil {
		return fmt.Errorf("client configuration unavailable")
	}

	if err := tc.ClientCfg.SetTarget(targetAddr); err != nil {
		return err
	}

	host, port := tc.ClientCfg.GetTarget()
	config.GetConfig().SetHost(host)
	config.GetConfig().SetPort(port)

	if tc.Svc != nil && tc.Svc.IsConnected() {
		fmt.Printf("Reconnecting to new target %s:%s...\n", host, port)
		_ = tc.Svc.Disconnect()
		// Auto-reconnect with new address
		tc.Svc.Address = fmt.Sprintf("%s:%s", host, port)
	}

	fmt.Printf("Target updated successfully to: %s:%s\n", host, port)
	return nil
}

// SetIP updates only the host IP address
func (tc *TargetCommand) SetIP(ip string) error {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return fmt.Errorf("ip address cannot be empty")
	}
	_, currentPort := tc.ClientCfg.GetTarget()
	return tc.SetTarget(fmt.Sprintf("%s:%s", ip, currentPort))
}

// SetPort updates only the port
func (tc *TargetCommand) SetPort(port string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		return fmt.Errorf("port cannot be empty")
	}
	currentHost, _ := tc.ClientCfg.GetTarget()
	return tc.SetTarget(fmt.Sprintf("%s:%s", currentHost, port))
}

// GetStatus prints current network target and connection status
func (tc *TargetCommand) GetStatus() string {
	host, port := "unknown", "unknown"
	if tc.ClientCfg != nil {
		host, port = tc.ClientCfg.GetTarget()
	}

	connected := false
	if tc.Svc != nil {
		connected = tc.Svc.IsConnected()
	}

	statusStr := "OFFLINE 🔴"
	if connected {
		statusStr = "ONLINE 🟢"
	}

	return fmt.Sprintf("Target: %s:%s | Connection Status: %s", host, port, statusStr)
}
