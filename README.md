# JISO - JSON ISO8583 Client Tool

JISO (JSON ISO8583) is a command-line tool for simulating ISO8583 message transactions. It allows you to connect to ISO8583 servers, send predefined transactions, and manage multiple concurrent transaction streams.

## Features

- Connects to ISO8583 servers with various header formats (ASCII, Binary, BCD, NAPS)
- Polymorphic JSON schema (`"transaction"`, `"dataset"`, `"scenario"`, `"mock_route"`)
- Dynamic Target Switching (`target <ip:port>`, `set ip <addr>`, `set port <port>`)
- Stateful Multi-Step Scenario Engine with context memory extraction (`{{context.X}}`) and dataset interpolation (`{{data.X}}`)
- Embedded ISO8583 Mock Server Subsystem with edge-case disruption injection (`delay_ms`, `drop_connection`, fallback catch-all routes)
- Structured Scenario Execution Reports with terminal progress trees and JSON export (`TestReport`) for CI/CD pipelines
- Instant Boilerplate Generators (`init-spec`, `init-tx`) compiled into binary via `//go:embed`
- PCAP & TCP Stream Traffic Analyzer with flow aggregation and variance analysis
- High-TPS telemetry batcher preventing terminal UI choking during stress testing
- Automatic field handling (STAN, date/time, etc.)
- Transaction metrics collection (response time, success rate, etc.)
- Comprehensive stress testing summary with start/end time, target/actual TPS, response breakdown, latency profile, and per-transaction metrics
- Interactive command-line interface with shlex lexer and middleware interceptor pipeline

## Installation

### Prerequisites

- Go 1.18 or higher
- Make (optional, for using Makefile commands)

### Building from Source

Clone the repository and build the executable:

```bash
git clone https://github.com/Andrei-cloud/jiso.git
cd jiso
make build
```

This will create a `jiso` executable in the current directory.

## Quick Start

1. Start the JISO interactive shell:

```bash
jiso
```

2. Inside the interactive shell, configure your target, specification, and transaction file:

```
jiso> target 127.0.0.1:9999
Target updated successfully to: 127.0.0.1:9999

jiso> spec ./specs/spec_bcp.json
Specification updated successfully to: ./specs/spec_bcp.json

jiso> tx ./transactions/transaction.json
Transaction file updated successfully to: ./transactions/transaction.json
```

If run without arguments, `spec` and `tx` present an interactive file browser to pick files directly from the terminal.

### Configuration Options

JISO supports command-line options to customize connection behavior and timeouts:

- `-reconnect-attempts <n>`: Number of reconnection attempts on connection failure (default: 3)
- `-connect-timeout <duration>`: Timeout for individual connection attempts (default: 5s)
- `-total-connect-timeout <duration>`: Total timeout for connection establishment (default: 10s)
- `-response-timeout <duration>`: Timeout for waiting responses to async messages (default: 5s)
- `-hex`: Enable hex dump output for messages
- `-db-path <path>`: Path to SQLite database file for storing sessions

Example with custom timeouts:

```bash
jiso -reconnect-attempts 5 -connect-timeout 3s -total-connect-timeout 15s -response-timeout 45s
```

## Testing

### Test Server

JISO includes a built-in test server for local testing and development:

```bash
# Build the test server
make testserver

# Run the test server (default: localhost:9999)
./testserver

# Or run directly
make run-testserver
```

The test server accepts ISO 8583 connections and responds to all transaction types with success codes. Use it to test JISO functionality without connecting to a real ISO 8583 server.

### Running Tests

```bash
go test ./...
```

## Usage Examples

### Basic Workflow

After starting the application, follow these steps to send a transaction:

1. Establish a connection:
   ```
   jiso> connect
   ```
   Select the message length header format (ascii4, binary2, bcd2, or NAPS)

2. Send a transaction:
   ```
   jiso> send
   ```
   Select a transaction from the list (e.g., "Sign On")

3. View transaction details:
   ```
   jiso> info
   ```
   Select a transaction to view its fields and description

4. List available transactions:
   ```
   jiso> list
   ```

5. Disconnect:
   ```
   jiso> disconnect
   ```

### Background Transactions

Send transactions continuously in the background:

```
jiso> bgsend
```

Follow the prompts to:
1. Select a transaction
2. Specify the number of worker threads
3. Set the execution interval (e.g., "500ms", "1s", "2.5s")

### Manage Background Workers

View active workers:
```
jiso> stats
```

Stop all background workers:
```
jiso> stop-all
```

Stop a specific worker:
```
jiso> stop
```
Select the worker ID to stop

### Stress Testing

Perform stress testing with gradual TPS ramp-up to a target TPS:

```
jiso> stress
```

Follow the prompts to:
1. Select one or more transactions for stress testing (using spacebar to multi-select)
2. Enter target TPS (transactions per second)
3. Enter ramp-up duration (e.g. "30s", "1m")
4. Enter test duration after ramp-up (e.g. "1m", "5m")
5. Enter number of concurrent workers

During execution, the stress test worker will randomly pick from the selected transaction types to execute.

## Sample Output

### Connection and Sign On Transaction

```
Spec file loaded successfully, current spec: ISO8583_CoreASCII
Transactions loaded successfully. Count: 6
Welcome to JISO CLI v0.2.0
Type 'help' for available commands
jiso> connect
? Select length type: ascii4
Connecting to server...
Successfully connected to server: localhost:9999

jiso> send
? Select transaction: Sign On
--- REQUEST ---
ISO8583_CoreASCII Message:
MTI..........: 0800
Bitmap HEX...: 82200000080000000400000000000000
Bitmap bits..:
    [1-8]10000010    [9-16]00100000   [17-24]00000000   [25-32]00000000
  [33-40]00001000   [41-48]00000000   [49-56]00000000   [57-64]00000000
  [65-72]00000100   [73-80]00000000   [81-88]00000000   [89-96]00000000
 [97-104]00000000 [105-112]00000000 [113-120]00000000 [121-128]00000000
F0   Message Type Indicator...............: 0800
F7   Transmission Date & Time.............: 0412232900
F11  Systems Trace Audit Number (STAN)....: 000151
F37  Retrieval Reference Number...........: 251020000150
F70  Network Management Information Code..: 1

--- RESPONSE ---
ISO8583_CoreASCII Message:
MTI..........: 0810
Bitmap HEX...: 822000000A0000000400000000000000
Bitmap bits..:
    [1-8]10000010    [9-16]00100000   [17-24]00000000   [25-32]00000000
  [33-40]00001010   [41-48]00000000   [49-56]00000000   [57-64]00000000
  [65-72]00000100   [73-80]00000000   [81-88]00000000   [89-96]00000000
 [97-104]00000000 [105-112]00000000 [113-120]00000000 [121-128]00000000
F0   Message Type Indicator...............: 0810
F7   Transmission Date & Time.............: 0412232900
F11  Systems Trace Audit Number (STAN)....: 000151
F37  Retrieval Reference Number...........: 251020000150
F39  Response Code........................: 96
F70  Network Management Information Code..: 1

Elapsed time: 2ms
```

### Stress Test Summary

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
  Successful:           196        (100.00%)
  Failed:               0          (  0.00%)
  Response Code Breakdown:
    Code "00":             196        (100.00%)
  Latency Profile:
    Min Latency:        1.2ms           Median (p50):          2.1ms          
    Max Latency:        8.5ms           p90 Percentile:        3.9ms          
    Mean Latency:       2.3ms           p95 Percentile:        4.5ms          
                                        p99 Percentile:        6.8ms          
--------------------------------------------------------------------------------
Transaction: Balance Inquiry
  Total Executions:     198       
  Successful:           198        (100.00%)
  Failed:               0          (  0.00%)
  Response Code Breakdown:
    Code "00":             198        (100.00%)
  Latency Profile:
    Min Latency:        1.5ms           Median (p50):          2.6ms          
    Max Latency:        10.2ms          p90 Percentile:        5.1ms          
    Mean Latency:       2.9ms           p95 Percentile:        5.8ms          
                                        p99 Percentile:        7.9ms          
--------------------------------------------------------------------------------
Transaction: Purchase
  Total Executions:     194       
  Successful:           194        (100.00%)
  Failed:               0          (  0.00%)
  Response Code Breakdown:
    Code "00":             194        (100.00%)
  Latency Profile:
    Min Latency:        1.8ms           Median (p50):          2.8ms          
    Max Latency:        12.4ms          p90 Percentile:        5.4ms          
    Mean Latency:       3.2ms           p95 Percentile:        6.2ms          
                                        p99 Percentile:        8.2ms          
--------------------------------------------------------------------------------
================================================================================
```

## Transaction Configuration

Transactions are defined in JSON format in the `transactions/transaction.json` file:

```json
[
    {
        "name": "Sign On",
        "description": "Network Management: Sign On",
        "fields": {
            "0": "0800",
            "7": "auto",
            "11": "auto",
            "37": "auto",
            "70": 1
        }
    },
    ...
]
```

Field features:
- `auto` - automatically populated fields (STAN, date/time, etc.)
- `random` - randomly selected values from the dataset
- Fixed values - directly specified in the configuration

## Advanced Features

### Random Data Sets

You can define datasets with random values for fields:

```json
"dataset": [
    {
      "2": "1234567890123456",
      "14": "2206",
      "23": "001"
    },
    {
      "2": "9876543210987654",
      "14": "2206",
      "23": "002"
    }
]
```

### Metrics Collection

The tool collects metrics for transactions:
- Execution count
- Mean execution time
- Standard deviation
- Response code distribution

### Robust Networking Features

JISO includes several features to ensure reliable operation in production environments:

- **Automatic Reconnection**: Automatically attempts to reconnect to the server on connection failures, with configurable retry attempts and exponential backoff
- **Connection Health Checks**: Background workers check connection status before sending transactions, preventing wasteful operations when offline
- **Retry Mechanisms**: Failed send operations are automatically retried with exponential backoff, distinguishing between temporary and permanent errors
- **Circuit Breakers**: Background workers automatically stop after consecutive failures to prevent resource exhaustion
- **Message Validation**: Transactions are validated before sending to catch configuration errors early
- **Configurable Timeouts**: Connection and operation timeouts can be adjusted for different network conditions

## Troubleshooting

If you encounter connection issues:

1. Check if the ISO8583 server is running
2. Verify the correct header format is selected
3. Check firewall settings
4. Validate the specification file matches the server implementation
5. Adjust connection timeouts if network latency is high (`-connect-timeout`, `-total-connect-timeout`)
6. Increase reconnection attempts for unreliable networks (`-reconnect-attempts`)
7. Adjust response timeout for slow-responding servers (`-response-timeout`)

For background worker issues:

1. Check worker statistics with `stats` command
2. Workers automatically stop after 10 consecutive failures (circuit breaker)
3. Workers skip transactions when connection is offline (health checks)
4. Use `stop-all` or `stop` commands to manually manage workers

## License

This project is licensed under the Apache 2.0 License - see the LICENSE file for details.

## Acknowledgments

- Built using [moov-io/iso8583](https://github.com/moov-io/iso8583) library
- Uses [moov-io/iso8583-connection](https://github.com/moov-io/iso8583-connection) for network connectivity
