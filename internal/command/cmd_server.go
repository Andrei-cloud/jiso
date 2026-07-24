package command

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"jiso/internal/config"
	"jiso/internal/server"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/AlecAivazis/survey/v2"
	"github.com/moov-io/iso8583"
)

// ServerCommand manages the embedded ISO8583 mock server from REPL or CLI
type ServerCommand struct {
	srv    *server.Server
	spec   *iso8583.MessageSpec
	routes []config.MockRouteConfig
	tc     transactions.Repository
	args   []string
}

func (sc *ServerCommand) Name() string     { return "serve" }
func (sc *ServerCommand) Synopsis() string { return "Manage embedded ISO8583 mock server (serve start [port] [headerType], serve stop, routes list)" }

func (sc *ServerCommand) SetArgs(args []string) {
	sc.args = args
}

func (sc *ServerCommand) Execute() error {
	if len(sc.args) == 0 {
		var action string
		options := []string{"Start Server", "List Routes"}
		if sc.srv != nil && sc.srv.IsRunning() {
			options = []string{"Stop Server", "Server Statistics", "List Routes", "Restart Server"}
		}

		prompt := &survey.Select{
			Message: "Select Mock Server action:",
			Options: options,
		}
		if err := survey.AskOne(prompt, &action); err != nil {
			return err
		}

		switch action {
		case "Start Server", "Restart Server":
			if sc.srv != nil && sc.srv.IsRunning() {
				_ = sc.StopServer()
			}
			return sc.promptStartServer()
		case "Stop Server":
			return sc.StopServer()
		case "Server Statistics":
			sc.PrintStats()
			return nil
		case "List Routes":
			sc.ListRoutes()
			return nil
		}
		return nil
	}

	subCmd := strings.ToLower(sc.args[0])
	switch subCmd {
	case "start":
		port := ""
		headerType := ""
		if len(sc.args) > 1 {
			port = sc.args[1]
		}
		if len(sc.args) > 2 {
			headerType = sc.args[2]
		}
		if len(sc.args) > 3 {
			specPath := sc.args[3]
			if loadedSpec, err := utils.CreateSpecFromFile(specPath); err == nil {
				sc.spec = loadedSpec
			}
		}
		if port == "" || headerType == "" {
			return sc.promptStartServer()
		}
		return sc.StartServer(port, headerType)

	case "stop":
		return sc.StopServer()

	case "stats", "status":
		sc.PrintStats()
		return nil

	case "routes", "list":
		sc.ListRoutes()
		return nil

	default:
		return fmt.Errorf("unknown server command '%s'. Available: start [port] [headerType] [specPath], stop, stats, routes", subCmd)
	}
}

func (sc *ServerCommand) PrintStats() {
	if sc.srv == nil || !sc.srv.IsRunning() {
		fmt.Println("Mock server is currently stopped")
		return
	}
	stats := sc.srv.GetStats()
	stats.PrintSummary(sc.srv.GetPort(), sc.srv.GetHeaderType(), sc.srv.ActiveConnections())
}

func (sc *ServerCommand) promptStartServer() error {
	// 1. Select Specification File
	specFiles := utils.FindAvailableSpecFiles()
	var selectedSpec string
	specPrompt := &survey.Select{
		Message: "Select ISO8583 Specification File:",
		Options: specFiles,
		Default: specFiles[0],
	}
	if err := survey.AskOne(specPrompt, &selectedSpec); err != nil {
		return err
	}

	if selectedSpec == "Custom Path..." {
		inputPrompt := &survey.Input{
			Message: "Enter path to specification JSON file:",
		}
		if err := survey.AskOne(inputPrompt, &selectedSpec); err != nil {
			return err
		}
	}

	if selectedSpec != "" && !strings.HasPrefix(selectedSpec, "[Default") {
		if loadedSpec, err := utils.CreateSpecFromFile(selectedSpec); err == nil {
			sc.spec = loadedSpec
			fmt.Printf("Loaded spec from: %s\n", selectedSpec)
		} else {
			fmt.Printf("Warning: Failed to load spec from '%s' (%v), using default spec\n", selectedSpec, err)
		}
	}

	// 1b. Select Transaction File (containing Mock Routes)
	selector := NewFileSelector("transaction")
	if txPath, err := selector.SelectFile(); err == nil && txPath != "" {
		if tcLoaded, err := transactions.NewTransactionCollection(txPath, sc.spec); err == nil && tcLoaded != nil {
			sc.routes = tcLoaded.GetMockRoutes()
			sc.tc = tcLoaded
			fmt.Printf("Loaded %d mock routes from: %s\n", len(sc.routes), txPath)
		}
	}

	// 2. Enter Port Number
	var port string
	portPrompt := &survey.Input{
		Message: "Enter server port:",
		Default: "9999",
	}
	if err := survey.AskOne(portPrompt, &port); err != nil {
		return err
	}

	// 3. Select TCP Header Type
	var headerType string
	headerPrompt := &survey.Select{
		Message: "Select TCP header type:",
		Options: []string{"ascii4", "binary2", "bcd2", "NAPS", "visa"},
		Default: "binary2",
	}
	if err := survey.AskOne(headerPrompt, &headerType); err != nil {
		return err
	}

	return sc.StartServer(port, headerType)
}

// RunDirectServer blocks in direct CLI mode until Ctrl+C (SIGINT/SIGTERM)
func (sc *ServerCommand) RunDirectServer(port string, headerType string) error {
	if err := sc.StartServer(port, headerType); err != nil {
		return err
	}

	fmt.Println("Press Ctrl+C to stop the mock server.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nStopping mock server...")
	sc.PrintStats()
	return sc.StopServer()
}

// NewServerCommand creates a new ServerCommand instance
func NewServerCommand(spec *iso8583.MessageSpec, routes []config.MockRouteConfig, tc transactions.Repository) *ServerCommand {
	return &ServerCommand{
		spec:   spec,
		routes: routes,
		tc:     tc,
	}
}

// StartServer starts the embedded mock server on the requested port with chosen header format
func (sc *ServerCommand) StartServer(port string, headerType string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		port = "9999"
	}
	headerType = strings.TrimSpace(headerType)
	if headerType == "" {
		headerType = "binary2"
	}

	if sc.srv != nil && sc.srv.IsRunning() {
		return fmt.Errorf("mock server is already running on port %s", sc.srv.GetPort())
	}

	sc.srv = server.NewServer(sc.spec, sc.routes, headerType)
	if err := sc.srv.Start(port); err != nil {
		return err
	}

	fmt.Printf("Embedded ISO8583 Mock Server started on port %s (Header: %s) 🟢\n", port, headerType)
	return nil
}

// StopServer stops the embedded mock server
func (sc *ServerCommand) StopServer() error {
	if sc.srv == nil || !sc.srv.IsRunning() {
		fmt.Println("Mock server is not running")
		return nil
	}

	port := sc.srv.GetPort()
	if err := sc.srv.Stop(); err != nil {
		return fmt.Errorf("failed to stop mock server: %w", err)
	}

	fmt.Printf("Embedded ISO8583 Mock Server on port %s stopped 🔴\n", port)
	return nil
}

// ListRoutes displays all active mock routes
func (sc *ServerCommand) ListRoutes() {
	if len(sc.routes) == 0 {
		fmt.Println("No mock routes configured")
		return
	}

	fmt.Println("================================================================================")
	fmt.Println(" CONFIGURED MOCK ROUTES")
	fmt.Println("================================================================================")
	for i, r := range sc.routes {
		var matchDesc strings.Builder
		if len(r.MatchFields) == 0 {
			matchDesc.WriteString("ANY")
		} else {
			first := true
			for k, v := range r.MatchFields {
				if !first {
					matchDesc.WriteString(", ")
				}
				matchDesc.WriteString(fmt.Sprintf("%s=%v", k, v))
				first = false
			}
		}
		delayStr := ""
		delayMs := r.DelayMs
		if delayMs == 0 && r.LatencyMs > 0 {
			delayMs = r.LatencyMs
		}
		if delayMs > 0 || r.JitterMs > 0 {
			delayStr = fmt.Sprintf(" | Latency: %dms (Jitter: ±%dms)", delayMs, r.JitterMs)
		}
		fmt.Printf(" Route %d: %-25s | Match: %s | Resp MTI: %s%s\n",
			i+1, r.Name, matchDesc.String(), r.ResponseMTI, delayStr)
	}
	fmt.Println("================================================================================")
}
