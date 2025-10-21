# JISO Test Server

A simple ISO 8583 test server for testing JISO locally.

## Features

- Accepts ISO 8583 connections on configurable host/port
- Handles basic transaction types:
  - Network Management (0800/0810) - Sign On, Sign Off, Echo/Test
  - Transaction requests (0200/0210)
  - Authorization requests (0100/0110)
- Responds with success codes (00) for all requests
- Logs incoming messages and processing

## Building

```bash
make testserver
```

This creates a `testserver` binary in the project root.

## Running

### Default (localhost:9999)
```bash
./testserver
# or
make run-testserver
```

### Custom port
```bash
./testserver 8888
```

### Custom host and port
```bash
./testserver 8888 192.168.1.100
```

## Usage with JISO

1. Start the test server:
   ```bash
   ./testserver
   ```

2. In another terminal, run JISO:
   ```bash
   make run
   ```

3. Use JISO commands like `connect`, `send`, `stresstest`, etc.

## Message Format

The server uses a simplified ISO 8583 v1987 message specification with the following fields:
- MTI (Message Type Indicator)
- Bitmap
- Processing Code
- Transmission Date/Time
- STAN (Systems Trace Audit Number)
- Local Transaction Time/Date
- Response Code

## Stopping

Press `Ctrl+C` to stop the server gracefully.

## Protocol

The server expects messages with a 2-byte big-endian length prefix, followed by the ISO 8583 message data. This matches the format used by JISO's connection handling.