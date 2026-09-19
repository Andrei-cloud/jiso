// Package app is the application-core façade that both the legacy CLI/REPL
// today and the TUI later drive. It owns configuration access, service
// lifecycle, connection state, and the one-shot send pipeline, wrapping the
// existing internal/service and internal/connection types rather than
// reimplementing their internals.
//
// Import rules (intended to be lint-enforceable later): internal/app must
// not import github.com/spf13/cobra, github.com/chzyer/readline,
// github.com/AlecAivazis/survey, or jiso/internal/cli/cmd, and must not
// write to stdout/stderr. Results are returned as structs (see result.go)
// and terminal I/O belongs to the frontends. Errors are typed locally
// (see errors.go), duplicating the minimal exit-taxonomy types instead of
// importing the cobra-side ones.
package app

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moov-io/iso8583"

	"jiso/internal/app/events"
	"jiso/internal/config"
	iconn "jiso/internal/connection"
	"jiso/internal/metrics"
	"jiso/internal/server"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// DefaultLengthType is the exported view of the fallback header (the §1
// header chip shows the EFFECTIVE header).
const DefaultLengthType = defaultLengthType

// defaultLengthType is the length header used by Connect when the
// configuration carries no header selection (mirrors the legacy REPL's
// first/default prompt option).
const defaultLengthType = "ascii4"

// App is the single typed entry point for application capabilities. It owns
// the configuration, the service (and through it the connection manager),
// the transaction repository, and the networking stats recorded on sends.
type App struct {
	cfg          *config.Config
	svc          *service.Service
	tc           transactions.Repository
	networkStats *metrics.NetworkingStats
	events       *events.Bus

	// dbReadMu serialises the §I read-only session-DB queries:
	// the db package keeps one process-wide connection, so the TUI's
	// off-UI-thread reads must never interleave with each other.
	dbReadMu sync.Mutex

	// tcMu guards the live transaction collection, swapped on a
	// successful tx-file apply (settingsapply).
	tcMu sync.Mutex

	mu     sync.Mutex
	closed bool

	// Worker manager state (APP-204). wmu guards the worker maps and the
	// sender resolver; it is never held while calling into the sender or
	// publishing events. svcKnobsMu serialises the service settings the
	// stress workers temporarily override (debug mode, max pending
	// requests) so concurrent workers do not race on them.
	wmu            sync.Mutex
	workers        map[string]*backgroundWorker
	stressWorkers  map[string]*stressWorker
	finishedStress map[string]*StressSummary
	resolveSender  func() WorkerSender

	svcKnobsMu sync.Mutex

	// Mock-server facade state (TUI §G). serveMu guards the
	// embedded engine and its route set; it is never held across the
	// engine's accept loop (Start returns once the listener is up).
	serveMu     sync.Mutex
	srv         *server.Server
	serveRoutes []config.MockRouteConfig
	serveSpec   string

	// Settings facade state (TUI §L). settingsMu guards
	// settingsOverrides, the session-only values for §L keys the
	// session config cannot hold (output); live-safe keys mutate
	// config.Config directly under its own lock.
	settingsMu        sync.Mutex
	settingsOverrides map[string]string
}

// New validates cfg and wires the service, TLS configuration, and
// transaction repository. A nil cfg falls back to the shared
// config.GetConfig singleton. Error texts match the legacy REPL
// initialization path so shims can return them verbatim.
func New(cfg *config.Config) (*App, error) {
	if cfg == nil {
		cfg = config.GetConfig()
	}

	if err := cfg.Validate(); err != nil {
		return nil, &ConfigError{Err: fmt.Errorf("configuration validation failed: %w", err)}
	}

	svc, err := service.NewService(
		cfg.GetHost(),
		cfg.GetPort(),
		cfg.GetSpec(),
		true, // Enable debug mode for testing reflection (legacy REPL wiring)
		cfg.GetReconnectAttempts(),
		cfg.GetConnectTimeout(),
		cfg.GetTotalConnectTimeout(),
		cfg.GetResponseTimeout(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	if tlsFileCfg := cfg.GetTLSConfig(); tlsFileCfg != nil && tlsFileCfg.Enabled {
		cryptoTLS, err := tlsFileCfg.BuildCryptoTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS configuration: %w", err)
		}
		svc.SetTLSConfig(cryptoTLS)
	}

	txPath := cfg.GetFile()
	if strings.TrimSpace(txPath) != "" && strings.TrimSpace(cfg.GetSpec()) == "" {
		return nil, &ConfigError{Err: errors.New(
			"specification must be defined before loading transaction file. " +
				"Please select a specification using 'spec <path>' first",
		)}
	}

	tc, err := transactions.NewTransactionCollection(txPath, svc.GetSpec())
	if err != nil {
		return nil, err
	}

	return &App{
		cfg:            cfg,
		svc:            svc,
		tc:             tc,
		networkStats:   metrics.NewNetworkingStats(),
		events:         events.New(),
		workers:        make(map[string]*backgroundWorker),
		stressWorkers:  make(map[string]*stressWorker),
		finishedStress: make(map[string]*StressSummary),
	}, nil
}

// isClosed reports whether Close has run. Callers holding wmu may use it
// to close the registration race against Close.
func (a *App) isClosed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.closed
}

// Config returns the configuration instance the App drives.
func (a *App) Config() *config.Config {
	return a.cfg
}

// Service returns the wrapped service. Transitional escape hatch for
// frontends whose commands are not shimmed over App yet (listener mode,
// workers); do not grow new logic against it.
func (a *App) Service() *service.Service {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.svc
}

// Events returns the typed event bus this App publishes to (connection
// state today; worker progress once the worker manager moves behind App).
// The bus is closed by Close.
func (a *App) Events() *events.Bus {
	return a.events
}

// publishLogf routes a diagnostic line to the app debug hook: the events.Logf
// event frontends render in their log view (instead of scraping stdout). It is
// a no-op on a bus-less App (test fixtures construct App without an event bus)
// and after Close, when the bus silently discards.
func (a *App) publishLogf(level, msg string) {
	if a.events != nil {
		a.events.Publish(events.Logf{Level: level, Msg: msg})
	}
}

// Transactions returns the live transaction repository. The collection
// is swapped when a tx-file apply succeeds (settingsapply); tcMu keeps
// the swap race-free against send-path readers.
func (a *App) Transactions() transactions.Repository {
	a.tcMu.Lock()
	defer a.tcMu.Unlock()

	return a.tc
}

// swapTransactions replaces the live collection after a successful
// tx-file apply (the send/compose path picks it up on the next call).
func (a *App) swapTransactions(tc transactions.Repository) {
	a.tcMu.Lock()
	a.tc = tc
	a.tcMu.Unlock()
}

// NetworkingStats returns the stats instance that sends record into.
func (a *App) NetworkingStats() *metrics.NetworkingStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.networkStats
}

// SetNetworkingStats redirects send error recording to stats owned by the
// frontend (the legacy CLI keeps one instance across reloads for its
// worker statistics view).
func (a *App) SetNetworkingStats(stats *metrics.NetworkingStats) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.networkStats = stats
}

// Close gracefully stops all background and stress workers, stops the embedded
// serve engine (listener + accept loop), releases the underlying service and
// closes the event bus so subscribers terminate. Workers publish their final
// WorkerStopped events before the bus closes. It is idempotent; the returned
// error reports a worker stop that timed out, a serve-engine stop failure,
// and/or the service close failure, if any.
func (a *App) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	svc := a.svc
	a.mu.Unlock()

	stopErr := a.stopAllWorkers(stopAllWorkersTimeout)

	// Stop the embedded serve engine so its listener and accept loop do not
	// outlive the App. ServeStop takes serveMu and is safe on a never-started
	// engine; because ServeStart holds serveMu across its closed-check and
	// bind, a start racing with this Close either observes closed (ErrClosed)
	// or binds before this call and is stopped here.
	serveErr := a.ServeStop()

	a.events.Close()

	if svc == nil {
		return errors.Join(stopErr, serveErr)
	}

	return errors.Join(stopErr, serveErr, svc.Close())
}

// Send composes transaction txName, validates and sends it, correlates the
// response STAN, records the execution in the repository and the session
// database, and returns the renderable result. It performs no terminal I/O;
// failures are reported via the error and the result's Error/Warnings
// fields. A nil result with a non-nil error means nothing was packed or
// sent (closed app, not connected, compose/validation failures).
func (a *App) Send(txName string) (*SendResult, error) {
	svc, err := a.openService()
	if err != nil {
		return nil, err
	}

	if !svc.IsConnected() {
		host := a.cfg.GetHost()
		port := a.cfg.GetPort()
		if strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
			return nil, errors.New(
				"target host and port are not configured. " +
					"Please set target using 'target <host:port>' first",
			)
		}

		return nil, &NotConnectedError{Address: fmt.Sprintf("%s:%s", host, port)}
	}

	msg, err := a.tc.Compose(txName)
	if err != nil {
		return nil, err
	}

	if err := ValidateMessage(msg); err != nil {
		return nil, fmt.Errorf("message validation failed: %w", err)
	}

	rawMsg, err := msg.Pack()
	if err != nil {
		return nil, err
	}

	result := &SendResult{Hex: utils.HexDump(rawMsg)}

	rebuiltMsg := iso8583.NewMessage(msg.GetSpec())
	if err := rebuiltMsg.Unpack(rawMsg); err != nil {
		return result, err
	}

	startTime := time.Now()
	response, err := svc.Send(msg)
	if stats := a.NetworkingStats(); err != nil && stats != nil {
		stats.RecordError(IsRetriableError(err))
	}

	success := err == nil
	a.tc.LogTransaction(txName, success)

	elapsed := time.Since(startTime)
	result.Elapsed = elapsed

	LogTransactionToDB(config.GetConfig().GetSessionID(), txName, msg, response, int(elapsed.Milliseconds()), success)

	if err != nil {
		result.Error = err.Error()

		return result, err
	}

	// Verify STAN correlation, mirroring the legacy interactive send.
	if err := verifySTAN(msg, response, txName, result); err != nil {
		return result, err
	}

	if responsePacked, packErr := response.Pack(); packErr == nil {
		result.ResponseHex = utils.HexDump(responsePacked)
	}

	result.Description = renderRequestResponse(rebuiltMsg, response, elapsed)
	result.Fields = describeFields(response)

	return result, nil
}

// renderRequestResponse produces the exact text the legacy REPL renderer
// wrote to stdout for a request/response pair.
func renderRequestResponse(request, response *iso8583.Message, elapsed time.Duration) string {
	var buf bytes.Buffer

	_, _ = fmt.Fprintln(&buf, "--- REQUEST ---")
	_ = utils.Describe(request, &buf, iso8583.DoNotFilterFields()...)

	_, _ = fmt.Fprintln(&buf, "\n--- RESPONSE ---")
	_ = utils.Describe(response, &buf, iso8583.DoNotFilterFields()...)

	_, _ = fmt.Fprintf(&buf, "\nElapsed time: %s\n", elapsed.Round(time.Millisecond))

	return buf.String()
}

// describeFields flattens the response fields into JSON-serializable views
// ordered by field number.
func describeFields(message *iso8583.Message) []FieldView {
	if message == nil {
		return nil
	}

	fields := message.GetFields()
	ids := make([]int, 0, len(fields))
	for id := range fields {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	views := make([]FieldView, 0, len(ids))
	for _, id := range ids {
		f := fields[id]

		name := ""
		if spec := f.Spec(); spec != nil {
			name = spec.Description
		}

		value, err := f.String()
		if err != nil {
			continue
		}

		views = append(views, FieldView{ID: strconv.Itoa(id), Name: name, Value: value})
	}

	return views
}

// verifySTAN mirrors the legacy interactive send's STAN correlation: a missing or
// unreadable STAN on either message, or a mismatch after normalization, is an
// error (a mismatch also records a warning on the result).
func verifySTAN(request, response *iso8583.Message, txName string, result *SendResult) error {
	requestStanField := request.GetField(11)
	if requestStanField == nil {
		result.Error = "request STAN field missing"

		return errors.New(result.Error)
	}
	requestStan, err := requestStanField.String()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get request STAN: %v", err)

		return fmt.Errorf("failed to get request STAN: %w", err)
	}

	responseStanField := response.GetField(11)
	if responseStanField == nil {
		result.Error = "response STAN field missing"

		return errors.New(result.Error)
	}
	responseStan, err := responseStanField.String()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get response STAN: %v", err)

		return fmt.Errorf("failed to get response STAN: %w", err)
	}

	reqStanNorm := iconn.NormalizeStan(requestStan)
	respStanNorm := iconn.NormalizeStan(responseStan)

	if reqStanNorm != respStanNorm {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"STAN mismatch detected: request=%s, response=%s for transaction %s",
			reqStanNorm,
			respStanNorm,
			txName,
		))
		result.Error = fmt.Sprintf("STAN mismatch: request=%s, response=%s", reqStanNorm, respStanNorm)

		return errors.New(result.Error)
	}

	return nil
}
