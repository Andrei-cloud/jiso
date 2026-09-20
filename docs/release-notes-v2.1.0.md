# jiso v2.1.0

The UAT-hardening release. Every headline item was found during user
acceptance testing against live VisaNet-style traffic and verified against
a real capture before merge.

## Session recording & CTF fidelity

- **Messages record their own dialect.** A transaction row records the
  specification its messages actually spoke: a stamped Visa exchange keeps
  `ISO8583_VISA` even while the session is dialled on another spec, and the
  CTF eligibility filter (§8, `ctf list`) follows the messages — approved
  sessions are no longer invisible to the export because of a config switch.
- **Recorded digits match the wire.** Fixed numeric fields no longer lose
  their leading zeros on the way into the session database; recorded JSON,
  describe trees and mock-route matching show the digits that travelled
  (`0920160705`, not `920160705`).
- **Faithful session review.** Session review renders recorded values
  directly: composite fields as nested `SUBFIELDS` trees (never Go map
  dumps), wire bytes recorded as plain hex on every send and preferred by
  the HEX pane; legacy formatted hex blobs still parse.

## Specifications

- `specs/visa.json`: DE55 dataset `01` tag `9F34` (CVM Results) — EMV
  templates carrying CVM data now compose, pack and replay end to end;
  DE118 max length aligned to the documentation.
- **Track2 (DE35) parses.** VisaNet packs the Track2 LL prefix as a digit
  count with `D`/`F` nibbles — supported via the `PackedBCDHex` value
  encoder (moov fork, upstream PR moov-io/iso8583#457). Every message of a
  666-message live capture parses.

## Connection correctness

- **Spec changes take effect live.** `Manager.SetSpec` rebuilds the live
  connection; previously a spec change while online left the connection
  unpacking with the old spec and silently-dropped responses timed out the
  STAN wait while the server had already matched and responded.
- The connection adopts the composed message's dialect for the exchange.

## TUI

- **§D send page scrolls.** Request and response panes scroll
  independently: `j`/`k` and arrows, `pgup`/`pgdown`, `home`/`end`,
  `tab` switches the keyboard's target pane, and the wheel scrolls the
  pane under the cursor; the focused, scrolled pane shows its position.
- **A timed-out send keeps its request tree** — the composed half of the
  exchange stays readable.
- **The send wizard asks only for what is unresolved**, and `s` on a
  transaction in §B fires that transaction's send the moment the wizard's
  connect step succeeds — no redundant transaction picker.
- §4 mock server draws one aligned grid with a flat stats card; the error
  modal owns the keyboard at true top z-order; one hotkey surface per
  state with a deduplicated footer.

## Analyze journey

- **The written extract serves and replays itself**: analyze → one file
  with transactions, scenario and mock routes → the same file starts the
  mock server and replays the capture.
- **Matching wizard**: mock routes are built by grouping the capture on
  operator-chosen fields (side-aware match conditions, scan-once preview);
  scaffolded routes no longer match on captured PAN; generated scenarios
  record the capture spec and the spec gate seats on it.

## Repository hygiene

- Test fixtures use neutral synthetic PANs only; UAT artifacts
  (session databases, capture files, capture-derived templates) are
  git-ignored so they can never enter the repository.

## Downloads

Self-contained binaries (no runtime dependencies; static `CGO_ENABLED=0`
builds with the default spec baked in) for `linux/{amd64,arm64}`,
`darwin/{amd64,arm64}` and `windows/{amd64,arm64}` are attached to this
release as `jiso-v2.1.0-<os>-<arch>.tar.gz` (`.zip` on Windows), each
archive containing the binary plus `LICENSE`.

Verify a download:

```
shasum -a 256 -c checksums.txt
```
