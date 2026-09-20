# Track2 (DE35) cannot be parsed from `specs/visa.json` — a moov-io/iso8583 gap

**Status:** open finding, verified empirically against live VisaNet traffic.
**Scope:** field 35 (Track 2 Data). No other field in the capture fails.
**Bottom line:** `specs/visa.json` is already correct and *cannot* be made to
parse these 96 messages through JSON. The fix is a small change in the
upstream library `github.com/moov-io/iso8583` (used at v0.26.0, not vendored).

---

## 1. How this was verified (ground truth = the capture)

Every raw message was pulled out of `visaonlnode1.pcap` (the operator's real
VisaNet channel capture) and parsed with **nothing but moov + `specs/visa.json`**.
Message byte boundaries come from the transport header codec only; the message
text is handed verbatim to `iso8583.NewMessage(spec).Unpack(payload)`. No custom
field-parsing code is involved — the spec JSON is the sole parser.

| MTI | framed | parse OK | parse FAIL |
|-----|-------:|---------:|-----------:|
| 0100 (auth request) | 209 | 113 | **96** |
| 0110 (auth response) | 209 | 209 | 0 |
| 0302 / 0312 (file mgmt) | 95 / 95 | 95 / 95 | 0 / 0 |
| 0400 / 0410 (reversal) | 1 / 1 | 1 / 1 | 0 / 0 |
| 0620 / 0630 (admin advice) | 26 / 26 | 26 / 26 | 0 / 0 |
| 0800 / 0810 (network mgmt) | 2 / 2 | 2 / 2 | 0 / 0 |
| **total** | **666** | **570** | **96** |

Because this is production traffic, every message *is* valid — so 96 failures
are a defect on **our** side (spec/library), not the network's.

## 2. The failures are one field, one message class

A bitmap-presence histogram (present-in-OK vs present-in-FAIL) isolates the
culprit exactly:

```
field : okCount / failCount
DE23     0 / 96    <<< present only in FAIL
DE35     0 / 96    <<< present only in FAIL   (Track 2 Data)
DE55     0 / 96    <<< present only in FAIL   (ICC/EMV data)
DE44    55 / 80     (parses fine when reached at the right offset — see §4)
DE49   113 / 96, DE51 104 / 95, DE53 0/3 …  (downstream misalignment victims)
```

The 96 failures are exactly the **chip-card 0100 requests** (Track2 + EMV
present). Every failure *reports* at DE44 ("data length: 159 is larger than
maximum 25") or cascades into DE49/51/53/55 BCD errors — all symptoms, never a
independent cause.

## 3. Root cause: the Track2 length prefix counts **digits**, moov reads **bytes**

The official spec is explicit (`Field 35 - Attributes`):

> Variable length 1 byte, binary + **37 N, 4-bit BCD (unsigned packed); maximum 20 bytes**

So DE35 = a **1-byte binary length prefix whose value is the number of track2
*digits*** (≤37), followed by those digits in **packed BCD — 2 digits/byte — so
the data is at most ⌈37/2⌉ = 19–20 bytes**, and the packed nibbles are *not*
decimal-only: the field separator is stored as nibble `0xD` and pad as `0xF`.

`specs/visa.json` DE35 uses `type=Track2, enc=ASCIIToHex (encoding.ASCIIHexToBytes),
prefix=Binary.L` — identical to moov's own `specs/track2.go`. moov's default
packer does:

```
dataLen      = Binary.L.DecodeLength(...)     // = LL byte = 37 (a DIGIT count)
read, value  = ASCIIHexToBytes.Decode(data, 37) // reads 37 BYTES
```

`ASCIIHexToBytes.Decode(data, n)` reads **n bytes**. So moov consumes **37
bytes** for a field that is only **19 bytes** on the wire — a **18-byte
over-read** — and every subsequent field is then mis-aligned by 18 bytes.

## 4. Byte-level proof on one real request (offset 112 in the stream)

After MTI + 16-byte bitmap, fields align **identically** up through DE32. The
only variable field before the reported failure is DE35 at offset 69, LL byte
`0x25` (=37). Two interpretations, everything after DE35:

| offset | Track2 = **37 bytes** (moov today) | Track2 = **19 bytes** (spec-correct) |
|--------|------------------------------------|--------------------------------------|
| 69  | DE35 LL=37, data 37 B | DE35 LL=37, data 19 B = `4085653077770572D30042210000036199995` |
| 89  | *(inside track2 data)* | **DE37 RRN `621509018851`** ✓ |
| 101 | *(inside track2 data)* | **DE41 terminal `10018993`** ✓ |
| 107 | DE37 RRN `931001104110` (wrong) | *(inside DE42)* |
| 109 | | **DE42 merchant `10011041101    `** ✓ |
| 124 | | **DE43 `AL GHAZAL ALTHAHBI SMON  Abu Dhabi    AE`** (full 40 B) ✓ |
| 164 | | **DE44 LL=9 `"    2   2"`** ✓ |
| 174 | | **DE49 currency `0784` (AED)** ✓ |
| 176 | | **DE51 currency `0784` (AED)** ✓ |
| 178 | | **DE55 LL=136** + well-formed EMV TLV ✓ |
| 182 | DE44 LL=**159** → **"larger than maximum 25"** ✗ | *(inside DE55)* |

The "moov today" column produces one plausible field (a track2-shaped RRN)
purely because EBCDIC text masks the shift, then dies at DE44. The
spec-correct column yields real, self-consistent values for *every* field.
The 18-byte gap is exactly `37 − ⌈37/2⌉`.

## 5. Why no JSON change can fix it (proven, not asserted)

The moov spec-JSON vocabulary is types {String, Track2, Numeric, Binary,
Composite, Bitmap} × prefixers {Fixed, L, LL, LLL, LLLL ∈ ASCII/BCD/Hex/EBCDIC/Binary}
× encs {ASCII, BCD, EBCDIC, Binary, HexToASCII, ASCIIToHex, LBCD}.

For DE35 we need: a **1-byte binary LL** read as a digit value (=37), the data
consuming **⌈LL/2⌉ = 19 bytes**, and the decoder **tolerating hex nibbles A–F**
(the `D` separator, `F` pad, BCD service/discretionary digits). Only BCD-family
encodings consume ⌈length/2⌉ bytes; only the byte-opaque encodings tolerate any
nibble. No combination does both. Each candidate was run against all 666 messages:

| DE35 JSON patch | result (all 96) |
|-----------------|-----------------|
| `Track2 / ASCIIToHex / Binary.L` (current) | reads 37 B → DE44+ misaligned (18 B over-read) |
| `Track2 / BCD / Binary.L` | `field 35 … failed to perform BCD decoding` |
| `String / BCD / Binary.L` | `field 35 … failed to perform BCD decoding` |
| `Track2 / LBCD / Binary.L` | `field 35 … failed to perform BCD decoding` |
| `Binary / len 19 / Binary.L` | `field 35 … data length: 37 is larger than maximum 19` |

Why:

* `BCD`/`LBCD` both decode with `yerden/go-util/bcd.Standard`, which is
  **decimal-only** — its nibble map contains only `'0'–'9'`; any nibble `A–F`
  returns `ErrBadBCD`. Track2's `D`/`F` nibbles are therefore undecodable, and
  field 35 itself fails.
* `ASCIIHexToBytes` / `Binary` consume **LL bytes** (2× the data) → alignment
  shift, the current failure.
* A fixed `length 19` is rejected because the LL prefix value is 37 (digits),
  and `Binary.L` compares it against the max as if it were bytes.

## 6. The gap, stated precisely

> moov-io/iso8583 has **no encoder that consumes `⌈length/2⌉` bytes while
> accepting non-decimal nibbles A–F**. For ISO 8583 packed-BCD variable-length
> fields whose binary length prefix counts *digits* (Track2 being the canonical
> Visa case), this combination is mandatory: the byte count is `⌈digits/2⌉`, but
> the content legitimately contains nibbles `A–F`. Decimal-only BCD cannot carry
> them; byte-opaque encodings cannot get the count right.

This affects Track2 and nothing else in the VisaNet auth-only traffic: PAN
(DE2), acquirer IDs (DE32/33) are also "digit-count binary LL + packed BCD", but
their digits are strictly 0–9, so moov's decimal `BCD` reads them correctly
(⌈LL/2⌉ bytes). Track2 is the only packed-BCD field that carries hex nibbles.

## 7. Proposed change to moov-io/iso8583 (with proof it works)

**Recommended (additive, backward compatible):** add a hex-tolerant packed-BCD
encoder, e.g. `encoding.PackedBCDHex`, and one vocabulary entry in
`specs/builder.go`. Its `Decode(src, length)` treats `length` as a **digit
count**: consume `⌈length/2⌉` bytes and map every nibble `0x0–0xF` to its hex
character `0–9A–F`, right-aligned exactly like the existing `bcdEncoder.Decode`
(drop the leading fill nibble on odd digit counts). `Encode(value)` is the exact
inverse (write LL = digit count; pack nibbles; left-pad odd values with `0xF`).

```go
// encoding/packedbcdhex.go  (Sketch — belongs in moov, NOT in jiso)
var PackedBCDHex = &packedBCDHexEncoder{}
type packedBCDHexEncoder struct{}
const hexpairs = "0123456789ABCDEF"

func (e *packedBCDHexEncoder) Decode(src []byte, length int) ([]byte, int, error) {
    digits := length
    if digits%2 != 0 {
        digits++                          // same even-up rule as bcdEncoder.Decode
    }
    read := digits / 2                    // ⌈length/2⌉ bytes — the count moov gets wrong
    if len(src) < read {
        return nil, 0, errors.New("not enough data to decode")
    }
    out := make([]byte, digits)
    for i, b := range src[:read] {        // hex-tolerant: any nibble → its hex char
        out[2*i] = hexpairs[b>>4]
        out[2*i+1] = hexpairs[b&0x0f]
    }
    return out[digits-length:], read, nil // right-align odd, matching bcdEncoder
}
```

Then in `specs/builder.go`:

```go
"PackedBCDHex": encoding.PackedBCDHex,   // in the enc map (add one line, ~line 72)
```

`specs/visa.json` then changes **one word** for DE35:

```jsonc
"35": { "type": "Track2", "length": 37, "enc": "PackedBCDHex", "prefix": "Binary.L" }
```

**Alternate (more invasive):** teach `field.Track2` itself that its `Binary.L`
prefix is a digit count and read `⌈LL/2⌉` bytes hex-tolerantly. This fixes
Track2 for everyone but changes the semantics of the existing `ASCIIHexToBytes`
Track2 spec, so the additive encoder above is the safer proposal.

**Proof it resolves everything and the rest of the spec is sound:** loading the
exact `specs/visa.json` through moov's importer and swapping **only DE35** for
the digit-count / `⌈LL/2⌉` / hex-tolerant behaviour above, then unpacking the
whole capture with moov:

```
WITH Track2 FIX -> framed: 666 | OK: 666 | FAIL: 0   (every MTI, 0 failures)
```

Because a single field swap takes 570 → 666, every other field/encoding/prefixer
in `specs/visa.json` is empirically correct for this live traffic (including the
EMV composite DE55, DE60/62/63, DE104/123, and the 0302/0312, 0400/0410,
0620/0630, 0800/0810 message families).

## 8. Impact on jiso today

* `specs/visa.json` needs **no** change to be *correct*: once moov ships the
  encoder above, flipping DE35's `enc` to `PackedBCDHex` is the only follow-up.
  Today jiso gracefully reports these 96 as "unparsable" in the §J reviewer;
  that count is a faithful reflection of the upstream limitation, not a jiso bug.
* The only way for jiso to parse these *today* would be to vendor/`replace` moov
  with the patch — a dependency-policy decision left to the maintainer, not a
  spec-JSON change, and out of scope for this verification pass.

## 9. Appendix — accuracy measured against the whole capture

The same run that reaches 666/666 records the largest real value seen for
every field. Because every message parsed, each field's declared Length in
`specs/visa.json` is **≥** what this production traffic carries — the spec is
empirically accurate for the traffic. Two families of *notation* differences
are noted (none is a failure), and **one latent reject risk**:

| field | official doc | `visa.json` declared | observed max (666 msgs) | verdict |
|-------|--------------|----------------------|-------------------------|---------|
| DE23  | Fixed 3 N, packed BCD; 2 bytes | Numeric / BCD.Fixed / 3 | 1 | exact ✓ |
| DE34  | LLVAR + ≤1535 bytes | Composite / Binary.LL / 1535 | 131 | exact ✓ |
| DE35  | **LLVAR + ≤37 digits packed BCD; ≤20 bytes** | Track2 / ASCIIToHex / 37 | 37 | the §1–7 gap |
| DE46  | LLVAR + ≤216 ANS | String / Binary.L / 255 | 33 | looser than doc (harmless) |
| DE55  | LLVAR + ≤255 bytes | Composite / Binary.L / 255 | 150 | exact ✓ |
| DE59  | LLVAR + ≤14 ANS; ≤15 bytes | String / Binary.L / **999** | 14 | far looser (harmless) |
| DE60  | LLVAR + 12 N packed BCD; 7 bytes total | Binary / Binary.L / 255 | 6 | wire LL is a *byte count* here → Binary correct ✓ |
| DE61  | LLVAR + 12 N BCD (7 B) **or 36 N** | Binary / Binary.L / 18 | 18 | 36-digit variant = 18 B, LL=byte count → parses ✓ |
| DE118 | LLVAR + 3 ANS + 252 ANS; **≤256 bytes** | String / Binary.L / **12** | (absent) | **tighter than doc → would reject a valid longer DE118** |
| DE104/123/125/126/127 | LLVAR + ≤255 bytes | …/ Binary.L / 255 | 152 / 119 / 197 / 55 / 67 | exact ✓ |

Reading the table: raising a variable-length max (e.g. **DE118 12 → 256** to
match the doc) is the **safe** direction — a larger max never rejects a message
that parses today — and is the one spec-JSON change this verification would
endorse for robustness, even though no DE118 appears in `visaonlnode1.pcap`.
Tightening a loose max (DE46/DE59) toward the doc is **not** endorsed from this
evidence alone: it cannot make the capture parse better and risks rejecting
valid traffic not in this sample. DE118's change is offered to the maintainer,
not applied here, to keep this round's edits only what the capture itself tests.
