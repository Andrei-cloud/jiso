# Mutual TLS (mTLS) & Visa SMC Guide

JISO supports Mutual TLS (mTLS) zero-trust encryption and authentication for both client connections and the embedded mock server, fulfilling payment scheme security requirements such as Visa Secure Messaging Controller (SMC).

---

## Features Overview

- **Consolidated Configuration File**: Pass all TLS options via a single JSON file (`--tls-config <path>`).
- **Strict PEM Certificate Validation**: Enforces PEM-encoded format for Client Certificates, Client Keys, and CA Certificate bundles.
- **Relative Path Resolution**: Certificate file paths in `tls_config.json` resolve automatically relative to the JSON config file location.
- **Visa SMC Heartbeat Daemon**: Automatically sends periodic Visa 0800 Network Connection Status keep-alive messages (DE 70 = `0301`, DE 63 = `0002`). Active **ONLY** when the `visa` header format is selected.
- **mTLS Mock Server**: Embedded mock server supports `tls.NewListener` with client certificate verification (`RequireAndVerifyClientCert`).
- **Interactive Certificate Generation Tool**: Built-in script (`scripts/gen-test-certs.sh`) to generate Root CA, Server, and Client test certificates.

---

## TLS Configuration Format (`tls_config.json`)

All TLS parameters are defined in a single, consolidated JSON file:

```json
{
  "enabled": true,
  "client_cert": "./client.crt",
  "client_key": "./client.key",
  "server_cert": "./server.crt",
  "server_key": "./server.key",
  "ca_cert": "./ca.crt",
  "server_name": "smc.visa.com",
  "min_version": "1.2",
  "insecure_skip_verify": false
}
```

### Configuration Attributes

| Attribute              | Type     | Required     | Description                                                                                     |
| :--------------------- | :------- | :----------- | :---------------------------------------------------------------------------------------------- |
| `enabled`              | `bool`   | Yes          | Master toggle — set to `true` to enable TLS/mTLS; `false` uses plain TCP.                       |
| `client_cert`          | `string` | Optional\*   | Path to PEM-encoded client certificate file (`.crt` / `.pem`).                                  |
| `client_key`           | `string` | Optional\*   | Path to PEM-encoded client private key file (`.key` / `.pem`).                                  |
| `server_cert`          | `string` | Optional\*\* | Path to PEM-encoded server certificate file (`.crt` / `.pem`) used by mock server mode.         |
| `server_key`           | `string` | Optional\*\* | Path to PEM-encoded server private key file (`.key` / `.pem`) used by mock server mode.         |
| `ca_cert`              | `string` | Optional     | Path to PEM-encoded Root/Intermediate CA bundle file (`.crt` / `.pem`) for server verification. |
| `server_name`          | `string` | Optional     | SNI hostname override for TLS verification.                                                     |
| `min_version`          | `string` | Optional     | Minimum TLS version: `"1.2"` or `"1.3"` (default: `"1.2"`).                                     |
| `insecure_skip_verify` | `bool`   | Optional     | Set to `true` to skip server certificate verification (testing only, default: `false`).         |

_Required when client authentication (mTLS) is expected by the remote endpoint or mock server._

\*_Required when starting JISO's embedded TLS/mTLS mock server._

---

## Generating Test Certificates

Use the included interactive script to generate a full CA → Server → Client PKI test chain:

```bash
./scripts/gen-test-certs.sh
```

### Interactive & Custom Inputs

The script prompts for X.509 Subject DN parameters (or uses environment variables):

```bash
COUNTRY="US" STATE="California" LOCALITY="San Francisco" \
ORG="Visa Payment Testing" OU="Engineering" SERVER_CN="localhost" \
./scripts/gen-test-certs.sh
```

### Generated Files (`testdata/certs/`)

- `ca.crt` / `ca.key` — Self-signed Root CA certificate and key.
- `server.crt` / `server.key` — Server certificate (with SAN `localhost` and `127.0.0.1`) signed by CA.
- `client.crt` / `client.key` — Client certificate (with `clientAuth` extended key usage) signed by CA.
- `tls_config.json` — Consolidated JISO TLS configuration file.

---

## CLI & Usage Examples

### 1. Connecting JISO Client via mTLS

Pass the `--tls-config` flag on start or when connecting:

```bash
jiso --host smc.visa.com --port 443 \
     --spec specs/visa.json \
     --tls-config testdata/certs/tls_config.json
```

In the interactive REPL:

```
jiso> connect visa
Connecting to smc.visa.com:443 (Header: visa)...
Connection established to smc.visa.com:443 🟢
[SMC-HEARTBEAT] Started Visa 0800 echo keep-alive daemon (30s interval)
```

### 2. Starting Mock Server in mTLS Mode

```bash
jiso server start 9999 visa \
     --spec specs/visa.json \
     --file transactions/transaction.json \
     --tls-config testdata/certs/tls_config.json
```

Console Output:

```
   ✓ TLS/mTLS server security enabled (ServerName: localhost)
Embedded ISO8583 Mock Server started on port 9999 (Header: visa) 🟢
```

---

## Visa SMC Keep-Alive Heartbeat

When connecting with the Visa header format (`visa`), JISO automatically manages session control:

1. **Activation Condition**: Runs **ONLY** when `visa` header format is active.
2. **Heartbeat Request**: Sends periodic ISO 8583 `0800` Network Management Request:
   - **DE 70**: `0301` (Network Connection Status / Echo)
   - **DE 63**: `0002` (Visa Network Identification)
   - **DE 7**: UTC Timestamp (`MMDDhhmmss`)
   - **DE 11**: System Trace Audit Number (STAN)
3. **Session Control Flag**: Sets byte 3 of the TCP header to `0x20` (Session Control) during transmission.
4. **Validation**: Verifies `0810` response code (DE 39 = `00`).
