# Task 9.5 report — §I Sessions: load session detail as the cursor moves (UAT round 9, F-9f)

**Branch:** `fix/tui-uat-round8` · **BASE:** `e99ba85` · **Commit:** `94f7cf1`
`feat(sessions): load session detail as the cursor moves over the list`

## Execution boundary analyzed

- Page keyboard: `internal/tui/pages/sessions_keys.go` (`updateKey` → `updateNav`), identity tracking in `sessions.go` (`syncSelID`, `SelectedSessionID`, `rebuild`).
- Page view: `sessions_view.go` (`statsBody`/`statsEmptyText`) + `sessions.go` (`historyEmptyText` via `rebuild`'s `SetEmptyMessage`).
- Root: `root_sessions.go` (arm/fold legs + seq lifecycle), `root_routes.go` (`routeSessionsMsg`), `root_sessions_state.go` (`sessionsState` snapshot → `syncSessions` on every Update), `root_pages.go` (`armBatches` re-arm), `hitmap.go` (`handleSelectMsg` click seam).
- Async read stays in root (tea.Cmd legs); the page imports no `tui`/`app` (SCR-501/509 fences held — verified by the green suite; no import changes).

## Root cause (confirmed as briefed)

Detail loads were Enter-only: `SessionsSelectMsg` → `handleSessionsSelect`. A cursor move was page-local — `updateNav` forwarded to the table (which returns nil cmds for every key), re-synced `s.selID`, and dropped the (nil) cmd, so `m.sessionsSelected` never followed the cursor. The panes kept rendering the last Enter-selected session.

## The focus-msg design (mirrors §K CTF `ctf.go:418-443`)

- **New message** `SessionsFocusMsg{ID string}` (`sessions_state.go`, next to `SessionsSelectMsg`). `SessionsSelectMsg` keeps the Enter meaning verbatim — the narrow `drill` stays Enter-only.
- **Page hook** (`updateNav`): capture `before := s.selID`; forward to the focused table; for the list pane `s.syncSelID()`; `if s.selID != before && !s.filtering { cmd = s.focusCmd() }`. A clamped same-row move yields nil (no load loop). The `paneHistory` branch returns the table's cmd unchanged — the TX HISTORY cursor never fires a session load.
  - Gate placement note: review-open and drill early-return before `updateNav` (verified: `updateReview`/`updateDrill` never reach it), **but `updateFilter` falls through to `updateNav` for arrows** (§B live-filter contract lets arrows move the filtered cursor). Hence the explicit `!s.filtering` guard in `updateNav` — arrows while filtering move the cursor visibly but the load waits until Enter yields the keyboard.
- **`focusCmd()`** mirrors `ctf.go:418-426`: returns `func() tea.Msg { return SessionsFocusMsg{ID: id} }`, nil when the filtered view is empty. Never touches `s.drill`.

## Root handler + seq/staleness handling

`routeSessionsMsg` gains `case pages.SessionsFocusMsg: → handleSessionsFocus(msg.ID)`.

`handleSessionsFocus(id)` = `handleSessionsSelect` MINUS the drill, in the `handleCtfSelect` shape:
1. guards: non-empty id, `m.Current().ID() == pages.SessionsPageID`, façade leg present (the `armSessions` gates, re-checked because the mouse seam calls in directly);
2. `m.sessionsSelected = id`; clear `m.sessionsStats`/`m.sessionsHistory`;
3. `m.sessionsSeq++` — in-flight detail/review legs turn stale instead of landing on the wrong row (the `root_ctf.go:248` pattern);
4. `armSessionsDetail(id)` — sets `sessionsDetailWait = true`, `sessionsDetailStale = false`, captures the new seq; `applySessionsDetail` folds (wait cleared BEFORE the stale checks, the uniform E5-FIX/M3 pattern); the Update-wrapper `armBatches` re-arms when needed.

**Seq-bump tradeoff (documented, same as §K):** a focus bump between a `r`-refresh arm and its result turns the list leg stale; `applySessionsList` clears the wait flag, and since `sessionsLoaded` stays true the list is not silently re-armed — the visible rows stay valid and the next `r`/dirty event re-queries. Arrows cannot race the *first* list load (an empty list has no cursor to move). §K accepts exactly this shape.

## DetailWait loading state

- `SessionsState.DetailWait bool` (doc: in-flight stats/history load for `SelectedID`).
- Stamped in `sessionsState()` from `m.sessionsDetailWait` — true from the moment Enter/focus arms the leg until the fold (or a stale drop) clears it.
- `statsEmptyText()` and `historyEmptyText()` return `detailLoadingText()` = `th.Ellipsis() + " loading"` while `DetailWait` — instead of the currently-false "select a session to see its stats" / "no transactions recorded for this session". Glyph comes from the theme (`~` under ASCII, `…` otherwise): 7-bit guard held (the guard test bans literals; the theme route is the sanctioned one, same as §K's `th.Ellipsis()+" computing"`).
- The history empty message refreshes through `rebuild()` on every `SetState`, so the flip is visible without extra plumbing.

## Mouse-click path — seam choice

`handleSelectMsg` (hitmap.go) after `sl.SelectRegion(msg.region, msg.index)`: when `msg.region == pages.RegionSessionsList` (and `m.sessions != nil`), it returns `m.handleSessionsFocus(m.sessions.SelectedSessionID())` — reading back the page's post-click identity (`SelectRegion` already moved the cursor + `syncSelID`). **Choice rationale:** the generic seam has no per-page cmd contract, so rather than inventing one I reused the single keyboard seam as the task sanctioned; all page/leg guards live inside `handleSessionsFocus`, so a straggler click msg (page jumped, no façade leg) is inert exactly like the keyboard path. Other regions keep select-only semantics; the `modalOpen()` gate ahead of this branch already freezes clicks under overlays.

## Staleness gap — late-fold guard (chose the fold-site guard over `leaveSessions()`)

`applySessionsDetail` now drops a fold when `m.Current().ID() != pages.SessionsPageID` (mirroring `applyCtfPreview`'s `root_ctf.go:287`), **and marks `m.sessionsDetailStale = true` on that drop** so `armSessions` re-queries when §I becomes current again — without the stale mark the drop would leave a `stale=false/wait=false` hole showing the previous session's cached rows forever. Chosen over a `leaveSessions()` in Push/Replace because:
- the fold-site guard covers every navigation path (Push, Replace, Pop back to a page) with one condition, vs. two call sites + seq bump + wait-flag resets;
- it preserves the cached-rows-across-navigation behavior outside the narrow late-fold case (existing tests like `TestSessionsWorkerStoppedTriggersRefresh` rely on cache semantics around navigation);
- §K set the precedent that the fold guards, not the leave hook, is the correctness line (leaveCtf is an optimization that also bumps seq; the guard is what makes mislanding impossible).
Pinned by `TestSessionsLateDetailFoldStaysOffPage` (late fold dropped on §B, §I re-arms and shows stats on return).

## Tests (TDD: all written red first, then implemented)

Page (`sessions_focus_test.go` — new file, moved out of `sessions_test.go` to respect the repohealth 600-line budget; `sessions_test.go` was 616):
- `TestSessionsCursorMoveEmitsFocus` — mirrors `TestCtfCursorMoveEmitsSelect`: down → `SessionsFocusMsg{77b255c9}`, second down → `31a000f4`, clamped down at last row → **nil** (no load loop), up → focus of the previous row.
- `TestSessionsNoFocusWhileModesOwnKeys` — no focus msg while reviewOpen / drill / filtering own the keyboard.
- `TestSessionsDetailWaitLoadingText` — `DetailWait` shows "~ loading" in both detail panes and suppresses both false empty texts.
- `TestSessionsHistoryCursorMoves` strengthened: the history-pane cursor move asserts a nil cmd (no session load).

Root (`root_sessions_test.go`):
- `TestSessionsCursorMoveLoadsDetail` — bare down (no Enter): `sessionsSelected` follows the cursor, `detailN` 1→2, last detail id = new session; clamped down adds nothing.
- `TestSessionsDetailWaitShowsLoadingText` — focus delivered but the leg left unpumped: frame shows the loading marker, not "no transactions recorded"; the fold clears it.
- `TestSessionsClickLoadsDetail` — click-select on `sessions:list` loads detail through the same seam.
- `TestSessionsLateDetailFoldStaysOffPage` — the fold-site page guard + re-arm on return.
- Kept green verbatim: `TestSessionsEnterSelectLoadsDetail` (Enter still `SessionsSelectMsg` + drill), `TestSessionsEntryLoadsThroughFacade` (`detailN==1` — entry alone loads exactly once; regression guard against a load loop), `TestSessionsNarrowDrill` (Enter-only).

## Golden review

`git status` shows **zero testdata changes**: all sessions goldens (pages + program layer `page_sessions`/`floor_*_sessions`) are driven by SetState fixtures with `DetailWait` false (program goldens run without a façade leg, so no wait flag is ever set), and no new bound key was added (§M parity untouched). Goldens stay byte-stable.

## Validation

- `go test ./...` — all green (fresh run).
- `go test -race ./internal/tui/...` — clean (the detail cmd leg runs concurrently; fake is mutex-guarded).
- `golangci-lint run --timeout=120s ./...` — 0 issues.
- `make qa` — "qa: all gates green" (includes the 7-bit goldens guard, §M parity, repohealth line budgets).
- Red→green TDD confirmed for every new test before implementing.

## Self-review notes

- Import fences held: `pages` still imports no `tui`/`app`; the async read remains root-side (page emits a msg; root arms the query).
- Goroutine/cmd lifecycle: every armed leg is a seq-tokened `tea.Cmd` reporting once; `applySessionsDetail` clears `sessionsDetailWait` before every drop path, so no arm state can wedge (no deadlock, no permanently-true wait flag).
- `handleSessionsFocus` never touches `sessionsReviewWait`/`sessionsListWait`; their legs clear their own flags on stale drops.

## Concerns / residual risks (need environment-level validation if they matter to UAT)

1. **Seq-bump vs. in-flight refresh/review (accepted §K tradeoff):** arrowing immediately after `r` (before the list result lands) or right after `[t]` (before the review result lands) turns that leg stale; the UI keeps showing the previous valid rows/note and the action is re-issuable. Same as §K's cursor-follow.
2. **Repeated clicks on the same row** re-query (clear + reload) instead of hitting the cache — matches §K's per-click preview; no loop risk since clicks are user-paced and the keyboard leg fires only on identity change.
3. Fast arrow spam fires one query per distinct row landed-on (not per keypress — clamped moves are silent, and intermediate rows between two quick presses do load if they were rendered/landed on). With the real SQLite façade this is bounded by `SessionStats`+`TxHistory` per press; a debounce layer would deviate from the §K pattern and was not introduced. If UAT round-9 perf testing shows DB strain, the seam for a trailing-edge debounce is `handleSessionsFocus` (root-side only).
4. `TestSessionsLateDetailFoldStaysOffPage` asserts the drop via root cache fields; a real `tea.Program` end-to-end run of the late-fold race was not executed (program-layer goldens run without a façade leg).
