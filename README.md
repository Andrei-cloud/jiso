# JISO — JSON ISO8583 Client & Mock Server Tool

JISO is a feature-rich command-line tool for simulating, testing, and debugging ISO8583 payment message flows. It connects to ISO8583 servers, composes and sends transactions from JSON templates, runs multi-step test scenarios, stress-tests payment switches, hosts an embedded mock server, and reverse-engineers PCAP traffic captures — all from a single binary.

## Features

- **Interactive REPL** with readline history, tab-completion, and a shlex-based command lexer
- **Polymorphic JSON configuration** — define `transaction`, `dataset`, `scenario`, and `mock_route` items in one file
- **Dynamic target switching** at runtime (`target`, `set ip`, `set port`) with auto-reconnect
- **Specification hot-swapping** (`spec`) and transaction file reloading (`tx`, `reload`)
- **Multi-step Scenario Engine** with context memory extraction (`{{context.X}}`), dataset interpolation (`{{data.X}}`), and response validation (exact, regex, exists)
- **Embedded ISO8583 Mock Server** with route matching, echo fields, auto-generated auth codes, configurable latency/jitter, required-field validation, and `drop_connection` chaos testing
- **Unsolicited message handling** — connect with `mock_routes` to auto-respond to incoming server-initiated messages
- **PCAP & TCP stream traffic analyzer** with flow aggregation, variance analysis, and auto-generation of transaction templates or mock server routes
- **Stress testing** with gradual TPS ramp-up, concurrent workers, and comprehensive summary reports (latency percentiles, response code breakdown, latency budget, histograms)
- **Background workers** (`bgsend`) with interval-based continuous sending, circuit breakers, and health-check gating
- **Boilerplate generators** (`init-spec`, `init-tx`) compiled into the binary via `//go:embed`
- **SQLite session logging** for transaction history and analytics (`--db-path`, `dbstats`)
- **VISA Base I header support** with station-ID management and session control
- **Hex dump mode** (`-hex`) for byte-level message inspection
- **Structured test reports** — ANSI-colored terminal trees and JSON export (`--report`) for CI/CD pipelines
- **Automatic field generation** — STAN, RRN, Auth Code, date/time fields populated at runtime
- **Per-transaction specification override** (`"spec"` key) for multi-network testing
- **Composite field support** — positional, TLV, BER-TLV/EMV, and bitmap-governed composites

## Installation

### Prerequisites

- Go 1.18 or higher
- Make (optional, for using Makefile commands)

### Building from Source

```bash
git clone https://github.com/Andrei-cloud/jiso.git
cd jiso
make build
```

The binary is written to `bin/jiso`. To build for Linux from macOS:

```bash
make build-linux
```

### Running without Building

```bash
go run ./cmd/main.go
```

---

## Quick Start

### 1. Generate Default Configuration Files

If starting from scratch, generate the default specification and transaction files:

```bash
# Generate a default ISO8583 specification
jiso init-spec

# Generate a comprehensive sample transaction configuration
# (includes transaction templates, datasets, scenarios, and mock routes)
jiso init-tx
```

These write to `./specs/spec.json` and `./transactions/transaction.json` respectively. You can pass a custom output path:

```bash
jiso init-spec ./specs/my_custom_spec.json
jiso init-tx   ./transactions/my_transactions.json
```

### 2. Start the Interactive Shell

```bash
jiso
```

### 3. Configure Target, Specification, and Transactions

```
jiso> target 127.0.0.1:9999
Target updated successfully to: 127.0.0.1:9999

jiso> spec ./specs/spec_bcp.json
Specification updated successfully to: ./specs/spec_bcp.json (Spec: ISO8583_CoreASCII)

jiso> tx ./transactions/transaction.json
Transaction file updated successfully to: ./transactions/transaction.json (Count: 5)
```

> **Tip:** Run `spec` or `tx` without arguments to get an interactive file browser that scans local directories for matching JSON files.

### 4. Connect and Send

```
jiso> connect
? Select length type: ascii4
? Parse and process unsolicited incoming messages via mock_routes? No
Connecting to server...
Successfully connected to server: 127.0.0.1:9999

jiso> send
? Select transaction: Sign On
--- REQUEST ---
ISO8583_CoreASCII Message:
MTI..........: 0800
...
--- RESPONSE ---
...
Elapsed time: 2ms
```

---

## Command-Line Interface

JISO operates in two modes: **Direct CLI** for automation and CI/CD, and **Interactive REPL** for exploratory testing.

### Direct CLI Subcommands

Run subcommands directly from your shell without entering the interactive session:

| Subcommand | Description |
|---|---|
| `init-spec [path]` | Generate a default ISO8583 specification JSON file |
| `init-tx [path]` | Generate a comprehensive sample transaction configuration file |
| `serve [start] [port] [headerType] [specPath]` | Start the embedded mock server (blocks until Ctrl+C) |
| `scenarios` | List all defined test scenarios (requires `-spec-file` and `-file`) |
| `run-scenario <name> [--report path] [--length type]` | Execute a named scenario against a live server |
| `analyze [args...]` | Launch the interactive PCAP/TCP stream traffic analyzer |
| `version` | Print version information |

**Examples:**

```bash
# Start mock server on port 9999 with binary2 headers
jiso serve start 9999 binary2

# Start mock server with custom spec and routes
jiso -spec-file specs/visa.json -file transactions/routes.json serve start 8080 binary2

# List scenarios
jiso -spec-file specs/spec.json -file transactions/transaction.json scenarios

# Run a scenario with JSON report export
jiso -host localhost -port 9999 \
     -spec-file specs/spec.json \
     -file transactions/transaction.json \
     run-scenario "E2E Purchase and Reversal" --report report.json --length ascii4

# Generate boilerplate
jiso init-spec ./specs/my_spec.json
jiso init-tx   ./transactions/my_tx.json

# Analyze a PCAP file interactively
jiso analyze
```

### Command-Line Flags

| Flag | Default | Description |
|---|---|---|
| `-reconnect-attempts <n>` | `3` | Number of reconnection attempts on connection failure |
| `-connect-timeout <duration>` | `5s` | Timeout for individual connection attempts |
| `-total-connect-timeout <duration>` | `10s` | Total timeout for connection establishment |
| `-response-timeout <duration>` | `5s` | Timeout for waiting responses to async messages |
| `-hex` | `false` | Enable hex dump output for request/response messages |
| `-db-path <path>` | `""` | Path to SQLite database file for session logging |
| `-visa-station-id <id>` | `""` | VISA Local Station ID (6-digit hex or decimal) |

**Example with custom timeouts and database logging:**

```bash
jiso -reconnect-attempts 5 -connect-timeout 3s -total-connect-timeout 15s \
     -response-timeout 45s -db-path ./jiso_sessions.db
```

---

## Interactive REPL Command Reference

After launching `jiso`, type `help` (or `h` / `?`) to see all available commands, organized by category:

### 🔌 Connection & Configuration Management

| Command | Aliases | Description |
|---|---|---|
| `connect` | — | Connect to the configured target server. Prompts for TCP length header type (`ascii4`, `binary2`, `binary4`, `bcd2`, `NAPS`, `visa`) and optional unsolicited message handling via `mock_routes`. |
| `disconnect` | — | Disconnect from the current server. |
| `target <host:port>` | `set` | Set or display the network target address. Without arguments, shows current target and connection status. |
| `spec [<path>]` | `use-spec` | Load an ISO8583 specification file. Without a path, opens an interactive file browser scanning `./specs/` for `.json` files. |
| `tx [<path>]` | `use-tx`, `transaction` | Load a transaction configuration file. Without a path, opens an interactive file browser scanning `./transactions/` for `.json` files. |

**Target switching examples:**

```
jiso> target 10.0.0.5:8080
Target updated successfully to: 10.0.0.5:8080

jiso> target
Target: 10.0.0.5:8080 | Connection Status: ONLINE 🟢
```

### 💳 Transaction & Load Testing Execution

| Command | Description |
|---|---|
| `send` | Send a single transaction interactively. Prompts to select from loaded transaction templates, validates the message, sends with automatic retry (up to 3 retries with exponential backoff), and verifies STAN correlation on the response. |
| `bgsend` | Start a continuous background worker. Prompts for transaction selection, number of worker threads, and execution interval (e.g., `500ms`, `1s`, `2.5s`). Workers include health-check gating and a circuit breaker (auto-stop after 10 consecutive failures). |
| `stress` | Start a stress test with gradual TPS ramp-up. Prompts for: transaction selection (multi-select), target TPS (1–1000), ramp-up duration, test duration, and concurrent workers (1–50). Produces a comprehensive summary report on completion. |
| `list` | List all available transaction templates by name. |
| `info` | Show detailed information about a selected transaction: MTI, processing code, field values, sample packed message (with hex dump), and parsed field view with dataset interpolation. |

### 🧪 Scenario & Test Automation

| Command | Aliases | Description |
|---|---|---|
| `scenarios` | `scenario` | List all defined test scenarios with their names and descriptions. |
| `run-scenario [<name>]` | — | Execute a named scenario. Without a name argument, prompts to select from available scenarios. Requires an active server connection. Prints an ANSI-colored execution report with step-by-step pass/fail status, latencies, and validation errors. |

### ⚙️ Embedded Mock Server Subsystem

| Command | Aliases | Description |
|---|---|---|
| `serve [subcommand]` | `server` | Manage the embedded ISO8583 mock server. Interactive menu when run without arguments. |

**Server subcommands (interactive or CLI):**

| Subcommand | Description |
|---|---|
| `serve start [port] [headerType]` | Start the mock server. Prompts for spec file, transaction file (for routes), port, and header type. |
| `serve stop` | Stop the running mock server. (Interactive mode only; in standalone mode use Ctrl+C.) |
| `serve stats` / `serve status` | Display server statistics: messages served, route hit counts, active connections. |
| `serve routes` / `serve list` | Display all configured mock routes with match criteria, response MTI, and latency settings. |

The mock server supports:
- **Route matching** by field values (MTI, Processing Code, Network Management Code, etc.)
- **Echo fields** — automatically copies specified request fields into the response
- **Required field validation** — responds with RC `30` (Format Error) if mandatory fields are missing
- **Auto-generated auth codes** — `"38": "auth_code"` generates a random 6-character authorization code
- **Latency simulation** — `delay_ms`/`latency_ms` with random `jitter_ms` variation
- **Connection dropping** — `"drop_connection": true` for chaos/timeout testing
- **Catch-all fallback** — unmatched requests get a response with RC `12` (Invalid Transaction)

### 📊 Worker & Operational Management

| Command | Aliases | Description |
|---|---|---|
| `stats` | `status` | Display active worker statistics in a table: ID, type (background/stress_test), transaction name, status, worker count, interval/TPS metrics, runtime, success/failure counts. Also shows networking statistics. |
| `stop <worker-id>` | — | Stop a specific background worker by ID. |
| `stop-all` | — | Stop all running background workers and stress tests. |
| `dbstats [session-id]` | — | Show SQLite database statistics for the current (or specified) session: total/successful/failed transactions, average processing time, response code distribution. Requires `--db-path` flag. |
| `reload` | — | Full service reload: stops all workers, closes connections and database, reinitializes the service, and re-registers all commands. |

### 📁 Scaffolding & Setup Utilities

| Command | Aliases | Description |
|---|---|---|
| `init-spec [path]` | — | Generate a default ISO8583 specification file. Defaults to `./specs/spec.json`. |
| `init-tx [path]` | — | Generate a comprehensive sample transaction configuration file. Defaults to `./transactions/transaction.json`. |
| `analyze` | `pcap` | Launch the interactive PCAP/TCP stream traffic analyzer. See [Traffic Analyzer](#traffic-analyzer-analyze--pcap) section. |

### 🛠️ General & Session Utilities

| Command | Aliases | Description |
|---|---|---|
| `help` | `h`, `?` | Display the categorized command reference. |
| `version` | `v` | Display JISO CLI version and author information. |
| `clear` | `cls` | Clear the terminal screen. |
| `exit` | `quit` | Exit the interactive CLI session. All workers are gracefully stopped and connections closed. |

---

## Transaction Configuration

All configuration items — transactions, datasets, scenarios, and mock routes — live in a single flat JSON array file. Each item declares a `"type"` discriminator.

> **See also:** [docs/SCHEMA.md](docs/SCHEMA.md) for the complete schema reference.

### Transaction Definition (`"type": "transaction"`)

```json
{
  "type": "transaction",
  "name": "Sign On",
  "description": "Network Management: Sign On (0800)",
  "fields": {
    "0": "0800",
    "7": "auto",
    "11": "auto",
    "37": "auto",
    "70": 1
  }
}
```

#### Autogenerated Field Keywords

Field values can use reserved keywords for dynamic runtime value generation:

| Keyword | Aliases | Generated Value |
|---|---|---|
| `"auto"` | `"$auto"` | Context-aware auto-population: DE 7 = timestamp, DE 11 = STAN, DE 12 = local time, DE 13/15/17 = local date, DE 37 = RRN, DE 38 = auth code |
| `"STAN"` | `"$STAN"`, `"stan"` | Atomic thread-safe 6-digit System Trace Audit Number (cyclic, persisted) |
| `"RRN"` | `"$RRN"` | 12-digit Retrieval Reference Number (cyclic, persisted) |
| `"auth_code"` | `"$auth_code"` | Random 6-character authorization code |
| `"datetime"` | `"$datetime"` | MMDDhhmmss transmission timestamp |
| `"date"` | — | MMDD current date |
| `"time"` | — | hhmmss current time |
| `"random"` | — | Randomly selected value from the linked dataset |

#### Per-Transaction Specification Override

Transactions can override the global specification by including a `"spec"` key pointing to a different spec file:

```json
{
  "type": "transaction",
  "name": "Echo Mastercard",
  "description": "Network Management: Echo Mastercard",
  "spec": "specs/mastercard.json",
  "fields": { ... }
}
```

#### Dataset Interpolation

Transactions can reference a named dataset for dynamic field values:

```json
{
  "type": "transaction",
  "name": "Purchase Template",
  "dataset_name": "card_pool",
  "fields": {
    "0": "0200",
    "2": "{{data.2}}",
    "14": "{{data.14}}",
    "35": "{{data.35}}"
  }
}
```

Placeholders matching `{{data.X}}` are replaced at runtime with values from a randomly selected entry in the named dataset.

### Dataset Definition (`"type": "dataset"`)

```json
{
  "type": "dataset",
  "name": "card_pool",
  "description": "Sample card data pool for testing scenarios",
  "data": [
    {
      "2": "1234567890123456",
      "14": "2512",
      "23": "001",
      "35": "1234567890123456=2512123"
    },
    {
      "2": "9876543210987654",
      "14": "2601",
      "23": "002",
      "35": "9876543210987654=2601123"
    }
  ]
}
```

### Scenario Definition (`"type": "scenario"`)

Scenarios define multi-step transaction flows with state persistence across steps.

> **See also:** [docs/scenarios.md](docs/scenarios.md) for the full scenario testing documentation.

```json
{
  "type": "scenario",
  "name": "E2E Purchase and Reversal",
  "description": "Sign On -> Purchase (extract AuthId) -> Reversal (use AuthId)",
  "dataset_name": "card_pool",
  "steps": [
    {
      "name": "Network Sign On",
      "use_transaction_id": "Sign On",
      "validate": [
        { "field": "39", "expect": "00" }
      ]
    },
    {
      "name": "Purchase Authorization",
      "use_transaction_id": "Purchase Template",
      "fields": { "4": "2500" },
      "extract": {
        "AuthId": "38",
        "OrigMTI": "0",
        "OrigSTAN": "11",
        "OrigDateTime": "7"
      },
      "validate": [
        { "field": "39", "expect": "00" },
        { "field": "38", "exists": true }
      ]
    },
    {
      "name": "Reversal of Purchase",
      "use_transaction_id": "Reversal Template",
      "fields": { "4": "2500" },
      "validate": [
        { "field": "39", "expect": "00" }
      ]
    }
  ]
}
```

#### Step Features

- **`use_transaction_id`** — References a named transaction template as the base message.
- **`fields`** — Override specific fields for this step (e.g., set amount to `"2500"`).
- **`extract`** — Extract response field values into the scenario context. Subsequent steps can reference extracted values via `{{context.VariableName}}`.
- **`validate`** — Assert response field values:
  - `"expect": "00"` — Exact match
  - `"regex": "^[0-9]{6}$"` — Regular expression match
  - `"exists": true` — Field presence/absence check

### Mock Route Definition (`"type": "mock_route"`)

```json
{
  "type": "mock_route",
  "name": "Purchase Authorization Approval",
  "match_fields": {
    "0": "0200",
    "3": "000000"
  },
  "required_fields": ["0", "2", "3", "4", "7", "11", "14", "41", "49"],
  "echo_fields": [2, 3, 4, 7, 11, 14, 37, 41, 49],
  "response_mti": "0210",
  "response_fields": {
    "38": "auth_code",
    "39": "00"
  },
  "latency_ms": 100,
  "jitter_ms": 25,
  "drop_connection": false
}
```

| Key | Type | Description |
|---|---|---|
| `match_fields` | object | Fields to match against incoming requests. Empty or omitted matches any request. |
| `required_fields` | array | Fields that must be present in the request; missing fields trigger RC `30` (Format Error). |
| `echo_fields` | array | Field IDs to copy from request to response. |
| `response_mti` | string | MTI for the response message. |
| `response_fields` | object | Static or dynamic response field values. `"auth_code"` generates a random 6-char code. |
| `delay_ms` / `latency_ms` | integer | Base response delay in milliseconds. `delay_ms` takes precedence if both are set. |
| `jitter_ms` | integer | Random variation applied to the base delay: `±jitter_ms`. |
| `drop_connection` | boolean | If `true`, closes the TCP connection without sending a response (chaos testing). |

---

## Traffic Analyzer (`analyze` / `pcap`)

JISO includes an interactive traffic analyzer that parses raw PCAP captures, groups traffic by MTI + Processing Code, performs variance analysis, and auto-generates reusable configuration items.

```
jiso> analyze
```

Or from the CLI:

```bash
jiso analyze
```

The interactive flow guides you through:

1. **Analysis Goal Selection**:
   - **Generate Transactions & Datasets** — Produces `"type": "transaction"` templates and `"type": "dataset"` pools from client request traffic.
   - **Generate Mock Server Routes** — Produces `"type": "mock_route"` definitions from server response traffic, with `match_fields` and `echo_fields` auto-detected.

2. **ISO8583 Specification Selection** — Pick from discovered spec files or enter a custom path.

3. **TCP Length Header Type** — Choose the framing format: `ascii4`, `binary2`, `binary4`, `bcd2`, `NAPS`, or `visa`.

4. **PCAP Capture File Selection** — Scans `./`, `./captures/`, `./pcap/`, `./dumps/` for `.pcap` / `.pcapng` files with file browser fallback.

5. **Directional Traffic & Port Filtering** — Inspect capture statistics and filter by direction (e.g., *Incoming Requests → Dst Port 9999*).

6. **Output File** — Write generated configuration to a target path (e.g., `transactions/pcaped.json`, `transactions/mock_routes.json`).

---

## Stress Testing

The `stress` command performs stress testing with gradual TPS ramp-up:

```
jiso> stress
? Select transactions for stress testing: [Sign On, Purchase Template]
? Enter target TPS: 10
? Enter ramp-up duration: 30s
? Enter test duration after ramp-up: 1m
? Enter number of concurrent workers: 1
```

During the test, transactions are randomly selected from the chosen types. On completion, a comprehensive summary is printed:

```
================================================================================
                          STRESS TEST SUMMARY - Worker a1b2c3d4
================================================================================
Start Time:             2026-07-05 17:44:30 MST
End Time:               2026-07-05 17:45:30 MST
Selected Transactions:  Sign On, Balance Inquiry, Purchase
--------------------------------------------------------------------------------
ALL TESTING SUMMARY
--------------------------------------------------------------------------------
Target TPS:             10         Concurrency (Workers): 1
Actual TPS:             9.8        Total Test Duration:   1m0s
--------------------------------------------------------------------------------
Transaction Counts:
  Total Executions:     588
  Successful:           588        (100.00%)
  Failed:               0          (  0.00%)
--------------------------------------------------------------------------------
Response Code Breakdown:
  Code "00":             588        (100.00%)
--------------------------------------------------------------------------------
Latency Profile:
  Min Latency:          1.2ms           Median (p50):          2.5ms
  Max Latency:          12.4ms          p90 Percentile:        4.8ms
  Mean Latency:         2.8ms           p95 Percentile:        5.5ms
                                        p99 Percentile:        8.2ms
--------------------------------------------------------------------------------
Latency Budget (Timeout: 5s):
  Satisfactory (<= 50% of timeout):  588        (100.00%)
  Tolerable    (51%-100% of timeout): 0          (  0.00%)
  Exceeded     (> 100% of timeout):   0          (  0.00%)
--------------------------------------------------------------------------------
Latency Histogram:
  [  0ms -  10ms]: ██████████████████████████████  580        (98.64%)
  [ 10ms -  50ms]: █                               8          ( 1.36%)
================================================================================
                    PER TRANSACTION TYPE DETAILS
================================================================================
Transaction: Sign On
  Total Executions:     196
  ...
================================================================================
```

The `stats` command monitors active stress tests and workers in real-time during execution.

---

## Connection Types

JISO supports multiple TCP message length header formats:

| Type | Description |
|---|---|
| `ascii4` | 4-byte ASCII decimal length header |
| `binary2` | 2-byte big-endian binary length header |
| `binary4` | 4-byte big-endian binary length header |
| `bcd2` | 2-byte BCD-encoded length header |
| `NAPS` | NAPS (National Australian Payment Switch) framing |
| `visa` | VISA Base I header with station ID, session control, and reject/accept data |

When connecting with the `visa` header type, JISO prompts for or uses the Local Station ID (configurable via `-visa-station-id` flag).

---

## Unsolicited Message Handling

When establishing a connection (`connect`), JISO can optionally process unsolicited incoming messages (server-initiated requests) by matching them against `mock_route` definitions:

```
jiso> connect
? Select length type: binary2
? Parse and process unsolicited incoming messages via mock_routes? Yes
Loaded 4 mock route(s) from 'transactions/transaction.json' for unsolicited incoming message handling.
Connecting to server...
Successfully connected to server: localhost:9999
```

This enables JISO to act as both client and responder — useful for testing bidirectional payment flows.

---

## Session Database

When launched with `--db-path`, JISO logs every transaction to a SQLite database for post-test analysis:

```bash
jiso -db-path ./sessions.db
```

Each session gets a unique UUID. View session statistics interactively:

```
jiso> dbstats
Database Statistics for Session: abc12345-...
=====================================
Total Transactions: 150
Successful Transactions: 148
Failed Transactions: 2
Average Processing Time: 3.45 ms

Response Code Distribution:
  00: 148
  96: 2

Full Statistics (JSON):
{ ... }
```

---

## Robust Networking

JISO includes production-grade networking features:

- **Automatic Reconnection** — Configurable retry attempts with exponential backoff
- **Connection Health Checks** — Background workers verify connection status before sending
- **Retry Mechanisms** — Failed send operations are retried with exponential backoff, distinguishing temporary from permanent errors
- **Circuit Breakers** — Background workers auto-stop after 10 consecutive failures
- **Message Validation** — Transactions are validated before sending to catch configuration errors early
- **STAN Correlation** — Request/response STAN matching verified for every transaction
- **Configurable Timeouts** — Connection, total connection, and response timeouts adjustable for different network conditions

---

## ISO8583 Specification Files

Specification files define the message format, field types, encodings, and composite field structures. JISO ships with several example specs in the `specs/` directory:

| File | Description |
|---|---|
| `spec.json` | Generic ISO8583 ASCII specification |
| `spec_bcp.json` | BCP (Base Communication Protocol) spec |
| `mastercard.json` | Mastercard specification |
| `visa.json` | VISA specification |
| `flex.json` | Flexible specification |
| `tsys_dhi.json` | TSYS DHI specification |
| `example_composed_emv.json` | Example demonstrating all composite patterns (positional, TLV, BER-TLV, bitmap) |

> **See also:** [docs/specifications.md](docs/specifications.md) for the complete specification authoring guide covering field types, encoders, prefixes, padding, composite fields, tag spec keywords, and unknown-tag handling.

---

## Project Structure

```
jiso/
├── cmd/main.go              # Application entry point
├── internal/
│   ├── cli/                 # Interactive REPL, worker management, display helpers
│   ├── client/              # Client configuration and target management
│   ├── command/             # All CLI commands (connect, send, stress, serve, analyze, etc.)
│   │   └── templates/       # Embedded default spec/transaction JSON templates
│   ├── config/              # Global configuration and flag parsing
│   ├── connection/          # ISO8583 connection wrapper and STAN normalization
│   ├── db/                  # SQLite session logging and async batch writer
│   ├── metrics/             # Transaction and networking statistics collectors
│   ├── repl/                # Shlex lexer for command tokenization
│   ├── reporter/            # Test report formatting
│   ├── server/              # Embedded mock server engine, route matcher, stats
│   ├── service/             # Service layer (spec loading, connection lifecycle)
│   ├── transactions/        # Transaction collection, scenario runner, compose/interpolate
│   ├── utils/               # Header adapters, spec loader, RRN/STAN generators
│   └── view/                # ISO message rendering
├── specs/                   # ISO8583 specification JSON files
├── transactions/            # Transaction configuration JSON files
├── docs/                    # Documentation
│   ├── SCHEMA.md            # Polymorphic configuration schema reference
│   ├── scenarios.md         # Scenario testing documentation
│   └── specifications.md   # ISO8583 specification authoring guide
└── Makefile                 # Build targets
```

---

## Testing

```bash
go test -v ./...
```

All packages include comprehensive test suites covering transaction composition, validation, configuration parsing, network utilities, RRN/STAN generation, server route matching, and more.

---

## Troubleshooting

### Connection Issues

1. Verify the ISO8583 server is running and reachable
2. Confirm the correct TCP header format is selected (`ascii4`, `binary2`, `bcd2`, `NAPS`, `visa`)
3. Check that the specification file matches the server's message format
4. Check firewall and network connectivity
5. Adjust timeouts for high-latency networks: `-connect-timeout`, `-total-connect-timeout`, `-response-timeout`
6. Increase retries for unreliable networks: `-reconnect-attempts`

### Background Worker Issues

1. Monitor worker status with `stats`
2. Workers auto-stop after 10 consecutive failures (circuit breaker)
3. Workers skip transactions when the connection goes offline (health checks)
4. Use `stop-all` or `stop <id>` to manage workers manually
5. Use `reload` to reinitialize the entire service without restarting the application

### Message Issues

1. Use `info` to inspect a transaction's composed message with hex dump
2. Run with `-hex` flag for byte-level request/response inspection
3. Check spec file loading errors — the error message identifies the problematic field and keyword
4. Verify `auto` keywords are applied to correct field IDs (see [Autogenerated Field Keywords](#autogenerated-field-keywords))
5. For composite fields, verify that subfield lengths sum to the parent composite length

---

## License

This project is licensed under the Apache 2.0 License — see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Built on [moov-io/iso8583](https://github.com/moov-io/iso8583) for message parsing and encoding
- Uses [moov-io/iso8583-connection](https://github.com/moov-io/iso8583-connection) for network connectivity
- Interactive prompts powered by [AlecAivazis/survey](https://github.com/AlecAivazis/survey)
- Readline history and tab-completion via [chzyer/readline](https://github.com/chzyer/readline)
