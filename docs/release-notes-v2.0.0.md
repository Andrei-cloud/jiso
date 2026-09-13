# jiso v2.0.0

The full CLI/TUI overhaul: a v2 Cobra command tree with a strict exit-code
taxonomy and machine-clean stdout, an `internal/app` core façade, complete
headless parity with the old interactive REPL, and a full-screen Bubble Tea
v2 TUI. The legacy line-oriented REPL is removed.

## Highlights

- **v2 command tree** — `spec`, `tx`, `connect`, `send`, `inspect`,
  `scenario`, `server`, `stress`, `analyze`, `ctf`, `db`, `version`, `tui`,
  with a bare-invocation usage hint (never auto-enters interactive mode).
- **Exit-code taxonomy** — 0 OK, 1 error, 2 usage/unavailable, 3 config
  (names the offending file), 4 failed test run, 130 SIGINT. Help always
  exits 0.
- **Output flags** — `--json` (pure JSON on stdout), `--quiet`, `--dry-run`;
  notices and warnings go to stderr, so `--json` pipes stay clean.
- **Config precedence** — flag > `JISO_*` env > user config > default;
  `JISO_DEBUG` prints stderr diagnostics.
- **Version stamping** — `-v`/`--version` prints version, commit, and build
  time (ldflags). Untagged builds show the short SHA; the `v2.0.0` stamp
  appears once this tag exists.
- **Headless parity (PAR-3xx)** — everything the REPL could do interactively
  now runs non-interactively: `send` one-shot, `inspect`, `connect check`,
  `serve start|stop|stats|routes`, `db stats`/`db tx`, `stress` with summary
  JSON, `analyze` with `--yes`/modes/`-o`, `ctf list|export`.
- **Full-screen TUI (`jiso tui`)** — Bubble Tea v2, page-stack router, command
  palette, and a documented page map: dashboard §A, transactions §B, message
  inspector §C, send exchange §D, connect dialog §E, scenarios §F, mock
  server §G, workers & stress §H, sessions §I, analyze wizard §J, CTF export
  §K, settings §L, help §M. Requires a TTY (without one: notice + exit 2).
- **PCAP analyze wizard (§J)** — a four-step wizard (capture → spec → header →
  run) that enumerates flows, selects each direction (`dst` requests / `src`
  responses) independently, reviews unparsable messages (the fields that parsed
  before the failure plus a hexdump with the unparsed bytes marked), and lets you
  choose exactly which generated items to write. Generated transactions record
  the spec they were composed with, so the written file reloads on the
  transactions screen regardless of the session's global spec.
- **Masking contract** — PAN and other sensitive fields stay masked across
  hex, auto, subfield, and raw-JSON views unless explicitly unmasked
  (`--unsecure`).
- **Docs** — a generated TUI user guide (`docs/tui.md`, pinned to the live help
  registry) and a REPL × CLI × TUI capability parity matrix.

## Breaking changes vs v1

- **REPL removed.** `jiso repl` prints
  `REPL was removed in v2.0.0 — use 'jiso tui' or the v2 command tree` to
  stderr and **exits 2**. The readline loop, tab-completion history, and
  `jiso>` prompt are gone (`github.com/chzyer/readline` dropped from the
  module).
- **`info` → `inspect`.** `jiso info <tx>` is now `jiso inspect <tx>`.
- **Flag taxonomy.** `-o` stays the output-file flag on commands that have
  it; `analyze --scenario` and `ctf export --session-id` are long-only
  (no `-s`/`-S` shorthands). Bare `jiso` prints a usage hint and exits 0
  instead of dropping into the REPL.
- **Exit codes are now contractual.** Scripts that treated any nonzero exit
  alike should branch on 2 (usage/unavailable), 3 (config), and 4 (failed
  test run); SIGINT is 130.

## Upgrade notes

- **Environment variables are unchanged**: `JISO_SPEC`, `JISO_FILE`, `JISO_DB`,
  `JISO_HOST`, `JISO_PORT`, `JISO_HEADER`, `JISO_TLS_CONFIG`,
  `JISO_VISA_STATION_ID`, `JISO_JSON`, `JISO_QUIET`, `JISO_DEBUG`,
  `JISO_UNSECURE`, `JISO_CONFIG`, `JISO_STATE_DIR`.
- **The user config file moved to the XDG path**:
  `<UserConfigDir>/jiso/config.yaml` (Linux `~/.config/jiso/config.yaml`,
  macOS `~/Library/Application Support/jiso/config.yaml`). If you had a v1
  config elsewhere, move it there or point `JISO_CONFIG` at it.
- **Replace REPL usage** with `jiso tui` (exploratory) or the headless
  commands (automation); see the migration table in CHANGELOG.md.
- **CI**: pipe-safe — use `--json` for machine output; without a TTY use the
  headless commands instead of `jiso tui`.
