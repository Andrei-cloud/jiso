# ISO8583 Specification Guide for JISO

This guide explains everything you need to know to create and customize ISO8583
specification files (`.json`) for use with the `jiso` tool and the underlying
[moov-io/iso8583](https://github.com/moov-io/iso8583) library.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Specification Architecture](#2-specification-architecture)
3. [Top-Level Keywords](#3-top-level-keywords)
4. [Field Keywords Reference](#4-field-keywords-reference)
5. [Field Types](#5-field-types)
6. [Encoders](#6-encoders)
7. [Prefixes (Length Headers)](#7-prefixes-length-headers)
8. [Padding](#8-padding)
9. [Composite Fields in Depth](#9-composite-fields-in-depth)
   - 9.1 [Positional Subfields](#91-positional-subfields)
   - 9.2 [TLV Subfields (explicit tag encoding)](#92-tlv-subfields-explicit-tag-encoding)
   - 9.3 [BER-TLV / EMV Subfields](#93-ber-tlv--emv-subfields)
   - 9.4 [Bitmap-Governed Composite](#94-bitmap-governed-composite)
   - 9.5 [Nested Composite Fields](#95-nested-composite-fields)
10. [Tag Spec Keywords](#10-tag-spec-keywords)
11. [Unknown-Tag Handling](#11-unknown-tag-handling)
12. [Complete Examples](#12-complete-examples)
13. [Validation & Debugging Tips](#13-validation--debugging-tips)

---

## 1. Overview

`jiso` uses the `moov-io/iso8583` library to parse, build, and send ISO8583
messages over a TCP connection. The message format is driven entirely by a
**specification file** — a plain JSON file you supply via the `-spec-file` flag.

```
./jiso -host localhost -port 9999 \
       -file transactions/transaction.json \
       -spec-file specs/my_spec.json
```

The spec file is loaded once at startup by `specs.ImportJSON` and converted into
a Go `*iso8583.MessageSpec` object that defines every field, its encoding, and
its length rules. You never need to recompile the binary; you only change the
JSON.

---

## 2. Specification Architecture

```
MessageSpec
 ├── name        (string)  – human-readable identifier shown in CLI output
 └── fields      (object)  – map of field index → field definition
       ├── "0"  – MTI (Message Type Indicator)
       ├── "1"  – Primary bitmap
       ├── "2" … "128" – ISO8583 data elements
       └── (extended fields up to "192" if supported)
```

Each field definition is a **`fieldDummy`** object that maps directly to a
`field.Spec` inside the Go library. The builder (`specs.ImportJSON`) reads the
JSON and constructs the matching Go objects automatically.

---

## 3. Top-Level Keywords

| Key      | Type   | Required | Description |
|----------|--------|----------|-------------|
| `name`   | string | Yes      | Spec identifier printed in the CLI (`current spec: …`). Must be unique if you load multiple specs. |
| `fields` | object | Yes      | Map from field number (as a quoted integer string) to a field definition object. The key must be parseable as an integer; non-numeric keys cause a load error. |

```json
{
  "name": "MyBank_v1",
  "fields": {
    "0":  { … },
    "1":  { … },
    "2":  { … }
  }
}
```

---

## 4. Field Keywords Reference

Every entry in `fields` (and every subfield within a composite) uses the same
set of keywords. Not all keywords apply to every field type.

| Keyword              | Type    | Required | Applies to      | Description |
|----------------------|---------|----------|-----------------|-------------|
| `type`               | string  | Yes      | all             | Field type. See [§5](#5-field-types). |
| `length`             | integer | Yes*     | primitive, composite | Maximum (or exact) field length in **logical units** (characters, bytes, or digits depending on encoding). Required for most types; omit only when `BerTLV` prefix handles it dynamically. |
| `description`        | string  | No       | all             | Human-readable label shown in message dumps and the `info` command. |
| `enc`                | string  | Yes*     | primitive types | Encoder for the field value. See [§6](#6-encoders). Required for all non-composite fields. |
| `prefix`             | string  | Yes      | all             | Length-indicator prefix placed before the field value on the wire. See [§7](#7-prefixes-length-headers). |
| `padding`            | object  | No       | String, Numeric | Optional padding applied before encoding/after decoding. See [§8](#8-padding). |
| `tag`                | object  | No       | Composite       | Configures how subfield tags are encoded/sorted. See [§10](#10-tag-spec-keywords). |
| `subfields`          | object  | No       | Composite       | Map of subfield key → field definition. Makes the field a Composite. |
| `bitmap`             | object  | No       | Composite       | Defines an internal bitmap that selects which subfields are present. See [§9.4](#94-bitmap-governed-composite). |
| `disableAutoExpand`  | boolean | No       | Bitmap          | When `true`, prevents the bitmap from automatically expanding to a secondary bitmap. Used in internal composite bitmaps. |

> **\* Note:** `enc` is required for all non-composite fields. For Composite fields,
> `enc` on the parent is omitted (subfields each carry their own `enc`). `length`
> is optional only when using `BerTLV` prefix for EMV subfields, because the
> BER-TLV encoding carries the length in the wire data itself.

---

## 5. Field Types

The `type` keyword selects the Go field constructor. Each type determines how
the value is stored in memory and how it round-trips through JSON marshaling.

| `type` value  | Go constructor           | Wire behaviour |
|---------------|--------------------------|----------------|
| `"String"`    | `field.NewString`        | General-purpose string; stores and encodes raw bytes. |
| `"Numeric"`   | `field.NewNumeric`       | Numeric-only data. Validated to contain only `[0-9]`. |
| `"Binary"`    | `field.NewBinary`        | Raw binary bytes; not interpreted as text. |
| `"Bitmap"`    | `field.NewBitmap`        | 64-bit or 128-bit bitmap. Special handling: automatically extends to secondary bitmap when field 65–128 are present. |
| `"Track2"`    | `field.NewTrack2`        | Magnetic-stripe Track 2 data (PAN, expiry, service code, discretionary). |
| `"Composite"` | `field.NewComposite`     | Container for subfields. Requires either `tag` or `bitmap` child, plus `subfields`. |

> **Tip:** For EMV field 55 use `"String"` with `enc: "Binary"` and `prefix:
> "BerTLV"` per subfield, not a top-level `"Binary"` type. The Composite wrapper
> handles the tag routing.

---

## 6. Encoders

The `enc` keyword controls how the field **value** is serialised to bytes (and
deserialised back). For Composite fields the `enc` keyword in the `tag` object
controls how **tag identifiers** are encoded.

### 6.1 Value Encoders

| `enc` value   | Wire representation | Typical use |
|---------------|---------------------|-------------|
| `"ASCII"`     | One byte per character in US-ASCII (0x00–0x7F). | Most printable fields: PAN, amounts, dates, response codes. |
| `"EBCDIC"`    | One byte per character in EBCDIC code page 037. | Mainframe-origin specifications (TSYS, FIS). |
| `"BCD"`       | Each pair of decimal digits packed into one nibble-pair (0x00–0x99). Right-padded with `0xF` if odd length. | Amount fields, STAN in BCD specs. |
| `"LBCD"`      | BCD, but left-padded (leading zeros) instead of right-padded. | Less common; LBCD-style PAN fields. |
| `"Binary"`    | Raw bytes; no character conversion. Value is treated as a hex string by the library when marshaling to/from JSON/Go. | EMV TLV values, PIN blocks, MAC. |
| `"HexToASCII"`| Two ASCII hex characters per source byte (`0A` → `0x30 0x41`). | MAC and bitmap fields in some ASCII-based specs where the bitmap must be human-readable. |
| `"ASCIIToHex"`| Reverse of `HexToASCII`: pairs of ASCII hex digits are collapsed to a single byte. | Tag encoders when the Data-Set ID is written as ASCII hex in the spec but packed as a single byte on the wire. |

### 6.2 Tag Encoders (used in `tag.enc`)

| `enc` value   | Usage | Details |
|---------------|-------|---------|
| `"ASCII"`     | Fixed-length ASCII tag identifier. | Tag is exactly `tag.length` ASCII characters. |
| `"EBCDIC"`    | Fixed-length EBCDIC tag identifier. | EBCDIC equivalent of the above. |
| `"BerTLVTag"` | BER-TLV variable-length tag. | Automatically reads 1 or 2 bytes as per BER rules (0x1F continuation bit). No `tag.length` needed. |
| `"ASCIIToHex"`| ASCII hex tag → packed byte(s). | Use when the spec key (e.g. `"56"`) is an ASCII hex representation of a 1-byte Data-Set ID. |

---

## 7. Prefixes (Length Headers)

The `prefix` keyword specifies how the field's **length** is encoded on the wire
immediately before the value bytes. The format is `<encoding>.<style>`.

### 7.1 Prefix Table

| `prefix` value | Encoding  | Style     | Wire bytes | Max length | Notes |
|----------------|-----------|-----------|------------|------------|-------|
| `"None.Fixed"` | None      | Fixed     | 0          | defined by `length` | No length indicator; field occupies exactly `length` units. |
| `"ASCII.Fixed"`| ASCII     | Fixed     | 0          | defined by `length` | No length indicator; field is exactly `length` characters. |
| `"ASCII.L"`    | ASCII     | 1-digit   | 1          | 9          | 1-char decimal length indicator. |
| `"ASCII.LL"`   | ASCII     | 2-digit   | 2          | 99         | 2-char decimal length indicator (LLVAR). |
| `"ASCII.LLL"`  | ASCII     | 3-digit   | 3          | 999        | 3-char decimal length indicator (LLLVAR). |
| `"ASCII.LLLL"` | ASCII     | 4-digit   | 4          | 9999       | 4-char decimal length indicator. |
| `"BCD.Fixed"`  | BCD       | Fixed     | 0          | defined by `length` | BCD-encoded value, fixed length. |
| `"BCD.L"`      | BCD       | 1-nibble  | 1          | 9          | 1-nibble BCD length indicator. |
| `"BCD.LL"`     | BCD       | 1-byte    | 1          | 99         | 1-byte BCD length indicator (2 nibbles). |
| `"BCD.LLL"`    | BCD       | 2 bytes   | 2          | 999        | BCD-encoded 3-digit length. |
| `"BCD.LLLL"`   | BCD       | 2 bytes   | 2          | 9999       | BCD-encoded 4-digit length. |
| `"Hex.Fixed"`  | Hex       | Fixed     | 0          | defined by `length` | Hex-encoded fixed-length field (e.g. binary bitmap as ASCII hex). |
| `"Hex.L"`      | Hex       | 1-char    | 1          | 9          | Hex-digit length indicator. |
| `"Hex.LL"`     | Hex       | 2-char    | 2          | 255        | 2-hex-char length indicator (0x00–0xFF). |
| `"Hex.LLL"`    | Hex       | 3-char    | 3          | 4095       | 3-hex-char length indicator. |
| `"Hex.LLLL"`   | Hex       | 4-char    | 4          | 65535      | 4-hex-char length indicator. |
| `"EBCDIC.Fixed"`| EBCDIC  | Fixed     | 0          | defined by `length` | Fixed-length EBCDIC field. |
| `"EBCDIC.LL"`  | EBCDIC    | 2-char    | 2          | 99         | 2-char EBCDIC length indicator. |
| `"EBCDIC.LLL"` | EBCDIC    | 3-char    | 3          | 999        | 3-char EBCDIC length indicator. |
| `"Binary.Fixed"`| Binary   | Fixed     | 0          | defined by `length` | Raw binary fixed-length. |
| `"Binary.L"`   | Binary    | 1-byte    | 1          | 255        | 1-byte binary length indicator. |
| `"Binary.LL"`  | Binary    | 2-byte    | 2          | 65535      | 2-byte big-endian binary length indicator. |
| `"BerTLV"`     | BER-TLV   | Dynamic   | 1–3        | 65535      | **Used for EMV subfields.** BER-TLV encodes the length automatically: 1 byte if length ≤ 127, 2 bytes if ≤ 255, 3 bytes if ≤ 65535. |

### 7.2 Choosing the Right Prefix

```
Fixed-length field (e.g. 4-digit MTI, 10-digit Datetime)
  → prefix: "ASCII.Fixed"   (zero-byte overhead)

Variable-length up to 99 chars (e.g. PAN, Track 2)
  → prefix: "ASCII.LL"      (2-byte ASCII decimal length)

Variable-length up to 999 chars (e.g. ICC data, Additional data)
  → prefix: "ASCII.LLL"     (3-byte ASCII decimal length)

EMV subfield inside a BER-TLV composite
  → prefix: "BerTLV"        (standard BER length encoding)

Binary fields in some specs (PIN block, MAC)
  → prefix: "Hex.Fixed"     (8 bytes as 16 ASCII hex chars)

Composite field with 1-byte binary length indicator
  → prefix: "Binary.L"
```

---

## 8. Padding

Padding is applied **after decoding** (strips pad bytes) and **before encoding**
(adds pad bytes). It is most useful for numeric fields that must always be a
fixed width.

```json
"padding": {
  "type": "Left",
  "pad":  "0"
}
```

| `type` value | Behaviour |
|--------------|-----------|
| `"Left"`     | Pad character added to the **left** (most significant position). Use for right-aligned numeric values (e.g. amounts zero-padded to 12 digits). |
| `"Right"`    | Pad character added to the **right** (least significant position). Use for left-aligned text fields. |
| `"None"`     | Explicit no-padding marker (same as omitting the `padding` object). |

The `pad` value must be exactly **one character**. Common choices:
- `"0"` — zero-padding for numeric fields
- `" "` — space-padding for text fields
- `"F"` — BCD nibble fill (less common in JSON specs)

---

## 9. Composite Fields in Depth

A **Composite** field is a container that holds multiple **subfields**. On the
wire the outer field has its own length prefix (the `prefix` keyword); inside
that boundary the subfields are packed according to their own specs.

There are four main composite patterns:

### 9.1 Positional Subfields

Subfields are **concatenated without any tag identifiers** in the wire data.
Each subfield occupies a fixed position by length. This is how classic
processing-code fields work.

```json
"3": {
  "type": "Composite",
  "length": 6,
  "description": "Processing Code",
  "prefix": "ASCII.Fixed",
  "tag": {
    "sort": "StringsByInt"
  },
  "subfields": {
    "1": {
      "type": "String", "length": 2,
      "description": "Transaction Type",
      "enc": "ASCII", "prefix": "ASCII.Fixed"
    },
    "2": {
      "type": "String", "length": 2,
      "description": "From Account",
      "enc": "ASCII", "prefix": "ASCII.Fixed"
    },
    "3": {
      "type": "String", "length": 2,
      "description": "To Account",
      "enc": "ASCII", "prefix": "ASCII.Fixed"
    }
  }
}
```

**Rules:**
- The `tag` object must be present but has **no `enc` or `length`** — only
  `sort` is needed to determine packing order.
- The subfield keys (`"1"`, `"2"`, `"3"`) serve as identifiers only; they do
  **not** appear in the wire data.
- The sum of subfield `length` values must equal the outer composite `length`
  (when all subfields are fixed-length).
- `sort: "StringsByInt"` packs subfield 1 before 2 before 3 (numeric order).

### 9.2 TLV Subfields (explicit tag encoding)

Each subfield is preceded by an explicit **tag** identifier on the wire, plus
its own length indicator. The tag format is defined in `tag.enc` and
`tag.length`.

```json
"48": {
  "type": "Composite",
  "length": 999,
  "description": "Additional Data – Private TLV",
  "prefix": "ASCII.LLL",
  "tag": {
    "length": 2,
    "enc": "ASCII",
    "sort": "StringsByInt",
    "padding": { "type": "Left", "pad": "0" },
    "skipUnknownTLVTags": true,
    "prefUnknownTLV": "ASCII.LL"
  },
  "subfields": {
    "01": {
      "type": "String", "length": 20,
      "description": "Terminal Serial Number",
      "enc": "ASCII", "prefix": "ASCII.LL"
    },
    "02": {
      "type": "String", "length": 40,
      "description": "Merchant Category Description",
      "enc": "ASCII", "prefix": "ASCII.LL"
    }
  }
}
```

**Rules:**
- `tag.length` — the fixed byte width of each tag ID on the wire (`2` for
  two-character ASCII tags like `01`, `02`).
- `tag.enc` — how tag identifiers are encoded (`"ASCII"` for plain text tags).
- `tag.padding` — optional: left-pad tag IDs with `0` so `"1"` becomes `"01"`.
- Each subfield's key must match what will appear on the wire (e.g. `"01"`,
  `"02"`).
- Each subfield carries its own `prefix` for the value length (e.g.
  `"ASCII.LL"`).

### 9.3 BER-TLV / EMV Subfields

ISO8583 Field 55 carries EMV cryptographic data as **BER-TLV** encoded
tag-length-value triplets. BER-TLV is self-describing: the tag and length
fields have variable widths determined by the BER encoding rules, not by the
spec.

```json
"55": {
  "type": "Composite",
  "length": 999,
  "description": "ICC Data – EMV BER-TLV",
  "prefix": "ASCII.LLL",
  "tag": {
    "enc": "BerTLVTag",
    "sort": "StringsByHex",
    "skipUnknownTLVTags": true,
    "storeUnknownTLVTags": true
  },
  "subfields": {
    "9F26": {
      "type": "String", "length": 8,
      "description": "Application Cryptogram",
      "enc": "Binary", "prefix": "BerTLV"
    },
    "9F36": {
      "type": "String", "length": 2,
      "description": "Application Transaction Counter",
      "enc": "Binary", "prefix": "BerTLV"
    },
    "82": {
      "type": "String", "length": 2,
      "description": "Application Interchange Profile",
      "enc": "Binary", "prefix": "BerTLV"
    }
  }
}
```

**Rules:**
- `tag.enc: "BerTLVTag"` — the library reads 1 or 2 bytes automatically per
  BER rules (bit 6 of the first byte signals continuation). **Do not set
  `tag.length`.**
- `tag.sort: "StringsByHex"` — sorts tag keys by their hexadecimal value, which
  matches the natural EMV packing order.
- Subfield keys are **hexadecimal strings** (`"9F26"`, `"82"`) matching the
  EMV tag registry.
- Every subfield uses `enc: "Binary"` and `prefix: "BerTLV"`. The `"BerTLV"`
  prefix handles the variable-length encoding automatically.
- `length` in EMV subfields is the **maximum** data length in bytes, not a wire
  indicator — the actual byte count is carried by `prefix: "BerTLV"`.

### 9.4 Bitmap-Governed Composite

Some proprietary fields use an **internal bitmap** to control which subfields
are present, mirroring the message-level bitmap mechanism.

```json
"126": {
  "type": "Composite",
  "length": 255,
  "description": "Private Use – Bitmap composite",
  "prefix": "Binary.L",
  "bitmap": {
    "type": "Bitmap",
    "length": 8,
    "description": "Internal subfield bitmap",
    "enc": "Binary",
    "prefix": "Binary.Fixed",
    "disableAutoExpand": true
  },
  "subfields": {
    "1": { "type": "String", "length": 2, "enc": "ASCII", "prefix": "ASCII.Fixed",
           "description": "Cardholder Certificate Serial Number" },
    "2": { "type": "String", "length": 2, "enc": "ASCII", "prefix": "ASCII.Fixed",
           "description": "Merchant Certificate Serial Number" },
    "3": { "type": "String", "length": 2, "enc": "ASCII", "prefix": "ASCII.Fixed",
           "description": "Transaction ID" }
  }
}
```

**Rules:**
- The `bitmap` object replaces the `tag` object. Both cannot be present
  simultaneously.
- `bitmap.type` must be `"Bitmap"`.
- `bitmap.disableAutoExpand: true` prevents the internal bitmap from growing
  to 128 bits automatically (the outer field governs its own size).
- Subfield keys are integers (`"1"` through `"64"` for a 64-bit bitmap).
- Subfields not present in the bit pattern are simply omitted from the wire
  data.

### 9.5 Nested Composite Fields

Composite fields can be nested arbitrarily. A common use is the **Data Set ID**
pattern: the outer composite uses a single-byte Data Set ID as a tag, and each
subfield is itself a BER-TLV composite.

```json
"125": {
  "type": "Composite",
  "length": 255,
  "description": "Extended Transaction Data",
  "prefix": "Binary.L",
  "tag": {
    "length": 1,
    "enc": "ASCIIToHex",
    "sort": "StringsByHex",
    "skipUnknownTLVTags": true,
    "prefUnknownTLV": "Binary.LL"
  },
  "subfields": {
    "56": {
      "type": "Composite",
      "length": 1535,
      "description": "Merchant Information Data Set",
      "prefix": "Binary.LL",
      "tag": {
        "enc": "BerTLVTag",
        "sort": "StringsByHex",
        "skipUnknownTLVTags": true
      },
      "subfields": {
        "01": {
          "type": "String", "length": 11,
          "description": "Merchant Identifier",
          "enc": "EBCDIC", "prefix": "BerTLV"
        },
        "02": {
          "type": "String", "length": 15,
          "description": "Terminal Identifier",
          "enc": "EBCDIC", "prefix": "BerTLV"
        }
      }
    }
  }
}
```

**How it works on the wire:**
```
[1-byte Data Set ID][2-byte length][BER-TLV tag][BER-TLV length][value] ...
```

---

## 10. Tag Spec Keywords

The `tag` object appears inside a Composite field definition to describe how
subfield tags are encoded on the wire.

| Keyword                | Type    | Description |
|------------------------|---------|-------------|
| `length`               | integer | Fixed byte-width of each tag identifier. Set for ASCII/EBCDIC/Binary fixed tags. **Omit** when using `"BerTLVTag"`. |
| `enc`                  | string  | Tag identifier encoding. See encoder table in [§6.2](#62-tag-encoders-used-in-tagenc). |
| `padding`              | object  | Optional padding for tag identifiers (e.g. left-pad `"1"` to `"01"`). Same structure as field padding. |
| `sort`                 | string  | Subfield sort function controlling packing order. See below. |
| `skipUnknownTLVTags`   | boolean | When `true`, tags not found in `subfields` are silently skipped during unpacking. |
| `storeUnknownTLVTags`  | boolean | When `true`, unknown tags are stored as `Binary` fields so they survive a repack. |
| `prefUnknownTLV`       | string  | A `prefix` value (e.g. `"ASCII.LL"`) telling the parser how to read the **length** of unknown tags so it can skip the right number of bytes. Required when `skipUnknownTLVTags` is `true` and `enc` is not `"BerTLVTag"` (BER-TLV handles this intrinsically). |

### Sort Functions

| `sort` value      | Ordering |
|-------------------|----------|
| `"StringsByInt"`  | Sort subfield keys numerically (1, 2, 3 … 10, 11). Use for positional and ASCII-integer-keyed TLV fields. |
| `"StringsByHex"`  | Sort subfield keys as hexadecimal values (sort `"82"` before `"9A"` before `"9F02"`). Use for BER-TLV / EMV fields. |

---

## 11. Unknown-Tag Handling

When you receive a message that contains a tag not defined in your `subfields`
map, the library must know what to do.

### Skipping Unknown Tags

```json
"tag": {
  "enc": "ASCII",
  "length": 2,
  "sort": "StringsByInt",
  "skipUnknownTLVTags": true,
  "prefUnknownTLV": "ASCII.LL"
}
```

- `skipUnknownTLVTags: true` — enable skip mode.
- `prefUnknownTLV` — tells the parser how the unknown tag's **value length** is
  encoded so it can jump past the right number of bytes.

For `"BerTLVTag"` fields, **do not set `prefUnknownTLV`** — BER-TLV length
rules are already built into the parser.

### Storing Unknown Tags

```json
"tag": {
  "enc": "BerTLVTag",
  "sort": "StringsByHex",
  "skipUnknownTLVTags": true,
  "storeUnknownTLVTags": true
}
```

With `storeUnknownTLVTags: true`, any unknown tag encountered is stored as a
`Binary` subfield keyed by its tag identifier. When the message is re-packed,
the stored unknown tags are included verbatim — enabling transparent proxying
of EMV data even when not all tags are defined.

---

## 12. Complete Examples

The file [`specs/example_composed_emv.json`](../specs/example_composed_emv.json)
in this repository demonstrates all four composite patterns in a single spec.
Use it as a starting point or reference.

### 12.1 Minimal ASCII Spec (Flat Fields Only)

```json
{
  "name": "MinimalASCII",
  "fields": {
    "0": { "type": "String", "length": 4, "description": "MTI",
           "enc": "ASCII", "prefix": "ASCII.Fixed" },
    "1": { "type": "Bitmap", "length": 8, "description": "Bitmap",
           "enc": "Binary", "prefix": "Hex.Fixed" },
    "2": { "type": "String", "length": 19, "description": "PAN",
           "enc": "ASCII", "prefix": "ASCII.LL" },
    "4": { "type": "String", "length": 12, "description": "Amount",
           "enc": "ASCII", "prefix": "ASCII.Fixed",
           "padding": { "type": "Left", "pad": "0" } },
    "11": { "type": "String", "length": 6, "description": "STAN",
            "enc": "ASCII", "prefix": "ASCII.Fixed" },
    "39": { "type": "String", "length": 2, "description": "Response Code",
            "enc": "ASCII", "prefix": "ASCII.Fixed" }
  }
}
```

### 12.2 Processing Code as Positional Composite

```json
"3": {
  "type": "Composite",
  "length": 6,
  "description": "Processing Code",
  "prefix": "ASCII.Fixed",
  "tag": { "sort": "StringsByInt" },
  "subfields": {
    "1": { "type": "String", "length": 2, "enc": "ASCII",
           "prefix": "ASCII.Fixed", "description": "Transaction Type" },
    "2": { "type": "String", "length": 2, "enc": "ASCII",
           "prefix": "ASCII.Fixed", "description": "From Account" },
    "3": { "type": "String", "length": 2, "enc": "ASCII",
           "prefix": "ASCII.Fixed", "description": "To Account" }
  }
}
```

### 12.3 Additional Data as TLV (Field 48)

```json
"48": {
  "type": "Composite",
  "length": 999,
  "description": "Additional Data Private",
  "prefix": "ASCII.LLL",
  "tag": {
    "length": 2,
    "enc": "ASCII",
    "sort": "StringsByInt",
    "padding": { "type": "Left", "pad": "0" },
    "skipUnknownTLVTags": true,
    "prefUnknownTLV": "ASCII.LL"
  },
  "subfields": {
    "01": { "type": "String", "length": 20,
            "description": "Terminal Serial Number",
            "enc": "ASCII", "prefix": "ASCII.LL" },
    "02": { "type": "String", "length": 40,
            "description": "MCC Description",
            "enc": "ASCII", "prefix": "ASCII.LL" }
  }
}
```

### 12.4 ICC/EMV Data as BER-TLV (Field 55)

```json
"55": {
  "type": "Composite",
  "length": 999,
  "description": "ICC Data – EMV BER-TLV",
  "prefix": "ASCII.LLL",
  "tag": {
    "enc": "BerTLVTag",
    "sort": "StringsByHex",
    "skipUnknownTLVTags": true,
    "storeUnknownTLVTags": true
  },
  "subfields": {
    "9F26": { "type": "String", "length": 8,
              "description": "Application Cryptogram",
              "enc": "Binary", "prefix": "BerTLV" },
    "9F36": { "type": "String", "length": 2,
              "description": "ATC",
              "enc": "Binary", "prefix": "BerTLV" },
    "9F10": { "type": "String", "length": 32,
              "description": "Issuer Application Data",
              "enc": "Binary", "prefix": "BerTLV" },
    "95":   { "type": "String", "length": 5,
              "description": "TVR",
              "enc": "Binary", "prefix": "BerTLV" },
    "82":   { "type": "String", "length": 2,
              "description": "AIP",
              "enc": "Binary", "prefix": "BerTLV" },
    "9C":   { "type": "String", "length": 1,
              "description": "Transaction Type",
              "enc": "Binary", "prefix": "BerTLV" }
  }
}
```

---

## 13. Validation & Debugging Tips

### Loading the Spec at Runtime

When JISO starts, it prints the loaded spec name:

```
Spec file loaded successfully, current spec: ISO8583_ExampleComposedEMV
```

If loading fails, the error message from `specs.ImportJSON` will identify the
problematic field and keyword:

```
error importing field: 55. unknown encoding: BadEnc for field: 9F26
```

### Programmatic Validation (Go)

```go
import (
    "os"
    "github.com/moov-io/iso8583/specs"
)

data, _ := os.ReadFile("specs/my_spec.json")
spec, err := specs.ImportJSON(data)
if err != nil {
    log.Fatalf("spec load error: %v", err)
}
fmt.Printf("OK: %d fields\n", len(spec.Fields))
```

### Common Mistakes

| Symptom | Likely cause |
|---------|--------------|
| `unknown prefix: …` | Typo in `prefix` value. Check the exact string against the table in [§7](#7-prefixes-length-headers). |
| `unknown encoding: …` | Typo in `enc` value. Encodings are case-sensitive (`"ASCII"` not `"ascii"`). |
| `no constructor for filed type: …` | Typo in `type`. Must be one of `String`, `Numeric`, `Binary`, `Bitmap`, `Track2`, `Composite`. |
| Composite field has no subfields | Forgot the `subfields` key, or the value is `{}` (empty). |
| BER-TLV fields not parsed | Used `prefix: "ASCII.LL"` instead of `prefix: "BerTLV"` for EMV subfields. |
| Unknown EMV tags cause parse error | Add `skipUnknownTLVTags: true` to the `tag` object of field 55. |
| Positional subfield wrong order | Check that `tag.sort` is `"StringsByInt"` and that subfield keys sort correctly. |
| Outer composite `length` mismatch | For fixed-length positional composites the outer `length` must equal the sum of all subfield `length` values. |

### Hex Dump Mode

Run JISO with `-hex` to see raw message bytes alongside the decoded output,
making it easy to trace encoding issues byte by byte:

```bash
./jiso -host localhost -port 9999 \
       -file transactions/transaction.json \
       -spec-file specs/example_composed_emv.json \
       -hex
```
