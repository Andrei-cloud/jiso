# JISO Scenario Testing Documentation

This document explains the multi-step scenario testing framework in JISO.

---

## 1. Overview

JISO scenario testing allows simulating stateful, multi-step transaction flows on ISO8583 payment switches (e.g., Sign On -> Purchase -> Extract AuthId -> Reverse Purchase).

---

## 2. Configuration Schema

All configuration components (transactions, reusable datasets, and scenarios) are defined in a polymorphic JSON array. The `"type"` discriminator determines the component type:

| Type | Description |
|---|---|
| `"transaction"` | A standalone transaction template (default if `type` is omitted). |
| `"dataset"` | A database-like card data pool reused across scenarios. |
| `"scenario"` | An ordered sequence of test steps. |

### Configuration Example

```json
[
  {
    "type": "transaction",
    "name": "Sign On",
    "description": "Network Management: Sign On",
    "fields": {
      "0": "0800",
      "7": "auto",
      "11": "auto",
      "70": 1
    }
  },
  {
    "type": "transaction",
    "name": "Purchase Template",
    "description": "Financial request base template",
    "dataset_name": "card_pool",
    "fields": {
      "0": "0200",
      "2": "{{data.2}}",
      "3": "000000",
      "4": "1000",
      "7": "auto",
      "11": "auto",
      "14": "{{data.14}}",
      "23": "{{data.23}}",
      "35": "{{data.35}}",
      "37": "auto",
      "41": "77973588",
      "43": "TEST MERCHANT            DOHA        QAT",
      "49": "634"
    }
  },
  {
    "type": "dataset",
    "name": "card_pool",
    "data": [
      {
        "2": "1234567890123456",
        "14": "2512",
        "23": "001",
        "35": "1234567890123456=2512123"
      }
    ]
  },
  {
    "type": "scenario",
    "name": "E2E Purchase and Reversal",
    "description": "Purchase transaction, extraction of Auth ID, and reversal",
    "dataset_name": "card_pool",
    "steps": [
      {
        "name": "Purchase Authorization",
        "use_transaction_id": "Purchase Template",
        "fields": {
          "4": "2500"
        },
        "extract": {
          "AuthId": "38"
        },
        "validate": [
          {
            "field": "39",
            "expect": "00"
          },
          {
            "field": "38",
            "exists": true
          }
        ]
      },
      {
        "name": "Reversal of Purchase",
        "use_transaction_id": "Reversal Template",
        "fields": {
          "4": "2500"
        },
        "validate": [
          {
            "field": "39",
            "expect": "00"
          }
        ]
      }
    ]
  }
]
```

---

## 3. Variable Interpolation & State Persistence

Scenario execution runs inside a stateful context:

- **Dataset Value**: Selected from the loaded datasets. Variables in templates or steps matching `{{data.X}}` (where `X` is the field ID, like `{{data.2}}`) are replaced with the matching field value from a randomly selected dataset entry of the transaction/scenario's configured dataset.
- **Localized Session Map**: Extracted variables stored from previous step responses. Variables matching `{{context.VariableName}}` are replaced with values dynamically extracted using `"extract"` rules.

### 3.1 Standalone Command Interpolation

When executing standard non-scenario commands (e.g. `send`, `bgsend`, `stress`):
1. JISO retrieves the transaction template.
2. Placeholders matching `{{data.X}}` are automatically interpolated using a randomly selected item from the respective dataset before validation and transmission.
3. Placeholders matching `{{context.VariableName}}` are resolved to empty strings since no active scenario session is active.

---

## 4. Response Validation

Step execution runs validation checks on received ISO8583 response fields:

- **Exact Match (`expect`)**: Checks if the field value matches exactly.
  ```json
  {"field": "39", "expect": "00"}
  ```
- **Regex Match (`regex`)**: Checks if the field matches a regular expression.
  ```json
  {"field": "38", "regex": "^[0-9]{6}$"}
  ```
- **Existence Check (`exists`)**: Asserts whether a field is present (`true`) or absent (`false`) in the response message.
  ```json
  {"field": "38", "exists": true}
  ```

---

## 5. CLI Operations

JISO CLI provides subcommands for initializing templates and executing scenarios.

### 5.1 Direct CLI Mode

Execute operations directly from the terminal without entering the interactive shell:

```bash
# Generate default specification schema
./jiso init-spec [custom_spec_path.json]

# Generate default comprehensive unified transactions config
./jiso init-tx [custom_tx_path.json]

# List all available scenarios
./jiso -spec-file specs/spec.json -file transactions/transaction.json scenarios

# Run a test scenario with ANSI color-coded tree logs and export JSON test report
./jiso -host localhost -port 9999 -spec-file specs/spec.json -file transactions/transaction.json run-scenario "E2E Purchase and Reversal" --report report.json --length ascii4
```

### 5.2 Interactive Shell Mode

Type commands directly inside the `jiso>` prompt:

```text
jiso> scenarios
jiso> run-scenario "E2E Purchase and Reversal"
jiso> init-tx custom.json
```
