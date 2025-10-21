# JISO - JSON ISO8583 Client Tool

JISO (JSON ISO8583) is a command-line tool for simulating ISO8583 message transactions. It allows you to connect to ISO8583 servers, send predefined transactions, and manage multiple concurrent transaction streams.

## Features

- Connects to ISO8583 servers with various header formats (ASCII, Binary, BCD, NAPS)
- Sends predefined ISO8583 transactions from JSON configuration files
- Supports single transactions and background transaction streams
- Automatic field handling (STAN, date/time, etc.)
- Transaction metrics collection (response time, success rate, etc.)
- Interactive command-line interface with command history
- Persistent counter management for STAN and RRN values
- Robust networking with automatic reconnection, retry mechanisms, and circuit breakers
- Configurable timeouts and connection parameters for different environments

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

1. Start the application with default configuration:

```bash
make run
```

This runs the application with the following default parameters:
- Host: localhost
- Port: 9999
- Transaction file: ./transactions/transaction.json
- Specification file: ./specs/spec_bcp.json

2. Or run it manually specifying parameters:

```bash
go run ./cmd/main.go -host <hostname> -port <port> -file <transaction-file> -spec-file <spec-file> [OPTIONS]
```

### Configuration Options

JISO supports several command-line options to customize connection behavior and timeouts:

- `-host <hostname>`: Server hostname (default: none, required)
- `-port <port>`: Server port (default: none, required)
- `-file <transaction-file>`: Path to transaction JSON file (default: none, required)
- `-spec-file <spec-file>`: Path to ISO8583 specification JSON file (default: none, required)
- `-reconnect-attempts <n>`: Number of reconnection attempts on connection failure (default: 3)
- `-connect-timeout <duration>`: Timeout for individual connection attempts (default: 5s)
- `-total-connect-timeout <duration>`: Total timeout for connection establishment (default: 10s)

Example with custom timeouts:

```bash
go run ./cmd/main.go -host localhost -port 9999 -file ./transactions/transaction.json -spec-file ./specs/spec_bcp.json -reconnect-attempts 5 -connect-timeout 3s -total-connect-timeout 15s
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
