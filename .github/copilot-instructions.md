# JISO - JSON ISO8583 Client Tool

## Architecture Overview
JISO is a CLI tool for simulating ISO 8583 financial transactions. It connects to ISO 8583 servers, sends predefined transactions from JSON configs, and collects metrics. Key components:
- **CLI Layer** (`internal/cli/`): Interactive command interface with background worker management
- **Command Layer** (`internal/command/`): Factory-pattern commands (connect, send, background) with dependency injection
- **Service Layer** (`internal/service/`): Wraps connection manager and message specs
- **Connection Manager** (`internal/connection/`): Handles ISO 8583 connections with header formats (ASCII4, Binary2, BCD2, NAPS)
- **Transaction Repository** (`internal/transactions/`): Loads JSON transaction definitions, handles auto/random field population
- **Utils** (`internal/utils/`): Persistent counters for STAN/RRN, ISO-specific helpers

Data flows: CLI → Command → Service/Connection → ISO Server, with metrics collection and response rendering.

## Critical Workflows
- **Build**: `make build` creates `jiso` executable
- **Run**: `make run` starts CLI with defaults (localhost:9999, ./transactions/transaction.json, ./specs/spec_bcp.json)
- **Test**: `go test ./...` runs all tests including connection and transaction mocks
- **Debug**: Use `internal/connection/manager.go` debug mode for hex dumps of messages
- **Persistence**: Counters and transaction logs saved to temp dir (`/tmp/jiso/` on Unix)

## Project-Specific Conventions
- **Auto Fields**: Set `"auto"` in transaction JSON for automatic population:
  - Field 7: Transmission date/time (MMDDhhmmss)
  - Field 11: STAN (incrementing counter)
  - Field 37: RRN (formatted date + counter)
- **Random Fields**: `"random"` picks from `dataset` array in transaction JSON
- **Header Formats**: Select ascii4/binary2/bcd2/NAPS when connecting
- **Command Factory**: All commands created via `command.NewFactory()` with injected dependencies
- **Transaction Caching**: Repository caches transactions by name for fast lookups
- **Metrics**: Tracks response time, success rate, response code distribution per transaction

## Examples
- **Send Transaction**: `jiso> send` → select "Sign On" → auto-populates MTI 0800, STAN, timestamps
- **Background Workers**: `jiso> bgsend` → specify threads and interval (e.g., "500ms") for continuous sending
- **Custom Transaction**: In `transactions/transaction.json`, define fields with fixed values, `"auto"`, or `"random"` from dataset

## Integration Points
- **External Dependencies**: moov-io/iso8583, moov-io/iso8583-connection libraries
- **ISO Server**: Connects to any ISO 8583 compliant server
- **Configs**: JSON specs in `specs/`, transactions in `transactions/`
- **Persistence**: State saved as JSON in temp directory for counters and logs