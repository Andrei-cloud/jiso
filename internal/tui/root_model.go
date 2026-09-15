// root_model.go holds the RootModel struct: the root router state —
// page registry slots, live-op bookkeeping, overlay ownership and the
// wiring seams the root_update/root_keys/root_view/root_stack method
// files operate on. It lives apart from root.go (construction) so each
// file keeps one review unit (repohealth split).
package tui

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/app/events"
	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// RootModel is the Bubble Tea v2 root model: a page-stack router with a
// global keymap layer and the command-palette mode state machine. The stack
// always holds at least one page; depth 1 is "root" (q quits there, deeper q
// pops).
type RootModel struct {
	app   *app.App
	keys  globalKeyMap
	stack []Page

	// registry holds the canonical page instances behind the 1..8 jump
	// slots; the stack references the live copies.
	registry []Page

	// pal is the command-palette overlay widget; nil means closed.
	pal *palette.Model

	// help is the §M help overlay (SCR-513) — a frame-level modal above
	// the page stack (palette/confirm mechanism, never a page in PageIDs);
	// nil means closed. While open it owns the keyboard: `?` toggles, Esc
	// closes first (§N1), everything else is swallowed.
	help *helpOverlay

	// Event bridge (TUI-404): eventSrc is installed by SetEventSource; the
	// bridge is armed as a Cmd on the next Update. eventCtx/eventSender are
	// wired by run to the caller ctx and program Send (run.stopBridge is the
	// program-exit stop); conn is the latest ConnectionEvent shown in the
	// frame's connection slot.
	eventSrc      <-chan events.Event
	eventSender   bridge.Sender
	eventCtx      context.Context
	bridge        *bridge.Bridge
	bridgePending bool
	conn          *events.ConnectionEvent

	// Resize coalescing (TUI-408): width/height always hold the latest
	// WindowSizeMsg so View tracks the last size instantly; appliedW/H is the
	// size pages last saw via forwardAll, and resizePending marks a trailing
	// resizeFlushCmd in flight that will relayout with the latest size.
	appliedW, appliedH int
	resizePending      bool
	resizeWindow       time.Duration

	// theme overrides the frame/palette theme when set; nil means
	// theme.Default(). The golden harness (TUI-407) pins an explicit
	// colorless theme: theme.Default() is a sync.Once seeded by whichever
	// test renders first, so ambient env cannot be trusted.
	theme *theme.Theme

	// debug is the TUI-409 lifecycle logger (nil = off); nil-safe hooks keep
	// Update I/O-free. run() installs it; tests may too.
	debug *debugLogger

	// Dashboard wiring (SCR-501): dash is the canonical page-1 instance
	// (registry[0]; a pointer type, so the quick-actions cursor survives
	// jumps). Root owns all App access and pushes DashboardState snapshots
	// via SetState. now is the injectable clock stamping EventMsg times,
	// uptime and card timestamps; connSince the live connection start.
	dash      *pages.Dashboard
	now       func() time.Time
	connSince *time.Time
	// connHeader is the header framing the live (or last live) connection
	// actually used, captured from the successful connect attempt. The
	// CONNECTION card and the header chip render it — the config value
	// alone lied when the form selected a different framing (UAT).
	connHeader  string
	dashActions []palette.Action

	// Transactions wiring (SCR-502): tx is the canonical page-2 instance
	// (registry[1]; pointer type, so filter/sort/cursor state survive jumps).
	// Root builds TransactionsState from the app and pushes it via SetState.
	tx *pages.Transactions

	// Inspector wiring (SCR-503): inspector is the canonical page-3 instance
	// (registry[2]; pointer type, so tab/tree/pool state survive jumps) and
	// the page pushed by Enter on §B. Root builds InspectorState on the
	// compose-without-send path and pushes it via SetState.
	inspector *pages.Inspector

	// Send wiring (SCR-504): send is the §D live exchange page pushed by
	// TxSendMsg, and sendRun the stage-machine truth (nil = never run).
	// lastSend/lastSendAt are the last completed run's state and frozen end
	// time (the ":last send" action, the §A quick row, UAT).
	lastSend   *pages.SendState
	lastSendAt time.Time
	// lastStress is the §A LAST STRESS card snapshot: the most recent
	// COMPLETED stress worker, stamped once when WorkerStopped folds the
	// App summary (never per tick).
	lastStress *pages.LastStressCard
	// Root owns the live op: the goroutine reports SendStageMsg values
	// through sendSender (run wires program.Send; tests inject a
	// collector — the bridge pattern), and liveConnect/liveSend override
	// the app legs (nil = the app-derived production implementations).
	// Elapsed stamps come from the injectable now; the page never ticks.
	// sendGen numbers runs so tick chains of a superseded run drop (single-flight).
	send        *pages.Send
	sendRun     *sendRun
	sendGen     uint64
	sendSender  bridge.Sender
	liveConnect func(ctx context.Context) error
	liveSend    func(ctx context.Context, txName string) (*liveExchange, error)

	// Connect dialog wiring (SCR-505): dlg is the §E modal overlay — the
	// page stack is never modified while it is open (Esc returns to the
	// SAME page). connectRun is the attempt-loop truth (nil = idle); the
	// goroutine reports ConnectAttemptMsg/ConnectResultMsg values through
	// connectSender (run wires program.Send; tests inject a collector —
	// the bridge pattern), and dialConnect overrides the app leg (nil =
	// the app-derived production implementation). connectSession holds
	// the last successful form values for prefill — session-only, never
	// written to the config file.
	dlg            *pages.ConnectDialog
	connectRun     *connectRun
	connectHost    *pages.ConnectDialog // dialog the in-flight stamps target (§E or wizard step 0)
	connectSession *pages.ConnectFormState
	connectSender  bridge.Sender
	dialConnect    func(ctx context.Context, opts app.ConnectOptions) error
	connectBackoff func(attempt int) time.Duration

	// Send wizard wiring (proposal 04 §B): wizard is the send-wizard modal
	// overlay; wizardSpec/wizardFile hold the paths picked in steps 1-2
	// (committed together with the send), wizardFiles remembers the tx
	// files used this session for the step-2 recents.
	wizard      *pages.SendWizard
	wizardSpec  string
	wizardFile  string
	wizardFiles []string
	// lastSentTemplate is the template of the most recent send; the
	// one-keystroke dashboard send reuses it (UAT round 5).
	lastSentTemplate string
	sendHistory      *pages.SendHistory       // send-history overlay page (UAT round 5)
	sends            []pages.SendHistoryEntry // bounded ring of completed sends

	// console is the bounded ring of NON-TUI system output lines
	// (connection manager stderr) rendered in the bottom console strip
	// (UAT: raw stderr writes corrupted the frame).
	console []string

	// serverLog is the bounded ring of internal/server (mock server)
	// output lines; unlike console it renders ONLY on the §4 server
	// page's LOG pane — server output must not leak to other screens
	// (UAT round 3).
	serverLog []string

	// lastConn is the state-dir remembered last-successful connect
	// (UAT prefill); lastConnLoaded marks the one read per session.
	lastConn       *app.LastConnection
	lastConnLoaded bool

	// lastServer is the state-dir remembered last mock-server start
	// (UAT: the §G form must prefill previous values, not fabricated
	// defaults); lastServerLoaded marks the one read per session.
	lastServer       *app.LastServerStart
	lastServerLoaded bool

	// Disconnect wiring (TUI-514, closes REGRESSION-1): disconnectFn
	// overrides the App.Disconnect leg (nil = the app method — the same
	// entry the REPL `disconnect` shim drives); disconnectSeq/
	// disconnectWait are the leg's seq-token lifecycle (the serverTickSeq
	// pattern: Push/Replace → leaveDisconnect bumps the seq so an
	// in-flight result turns stale), and disconnectConfirm is the §N3
	// confirm opened while workers are active or the serve engine runs
	// (default No). The card flip is NOT stored here — App.Disconnect
	// publishes the Disconnected event and updateBridgeMsg owns conn.
	disconnectSeq     uint64
	disconnectWait    bool
	disconnectConfirm *widgets.ConfirmDialog
	disconnectFn      func() error

	// Scenarios wiring (SCR-506): scenarios is the §F page — a registry
	// entry after the 8 hotkey slots, reachable via the palette
	// ":scenarios" (the wire-compat slot id "scenario" stays with the
	// merged §C inspector; §F steals no hotkey). scenarioRun is the
	// live-run truth (nil = never run); the goroutine reports
	// scenarioStepMsg/scenarioDoneMsg values through scenarioSender
	// (run wires program.Send through wireSenders — run.go; tests
	// inject a collector — the bridge pattern), and runScenario
	// overrides the engine leg (nil = the
	// production transactions.ScenarioRunner, the SAME engine the CLI
	// scenario run uses). scenarioLastReport holds the last completed
	// report for `e`; scenarioStatusLine is the toast-less export line.
	scenarios          *pages.Scenarios
	scenarioRun        *scenarioRun
	scenarioSender     bridge.Sender
	runScenario        scenarioEngine
	scenarioLastReport *transactions.TestReport
	scenarioStatusLine string

	// Scenario export single-flight (E5-FIX/M6): scenarioWriteWait
	// marks an in-flight `e` leg (stat or write — two rapid `e` must
	// not interleave), scenarioStatFn overrides the os.Stat leg
	// (tests), scenarioConfirm is the §N3 overwrite confirm (default
	// No), and scenarioExportPending carries the confirmed path so a
	// trailing "?" in it survives (never parsed back out of the
	// question text).
	scenarioWriteWait     bool
	scenarioStatFn        func(string) (os.FileInfo, error)
	scenarioConfirm       *widgets.ConfirmDialog
	scenarioExportPending string

	// scenarioExportPath overrides the export destination (tests; empty
	// = scenarioReportDefaultPath — a palette-set path lands with
	// TUI-406b forms).
	scenarioExportPath string

	// Server wiring (SCR-507): server is the canonical §G page instance
	// (registry[4]; the wire-compat slot id "server" stays, the frame
	// title is the §G one). Root owns the live serve op: serveStartFn /
	// serveStopFn override the app's in-process serve legs (nil =
	// app.ServeStart / app.ServeStop — the same engine and config
	// resolution the cobra `serve start` shim runs), serveStatsFn /
	// serveRoutesFn override the stats/routes readers (nil = the app's
	// in-process accessors; no snapshot file), and serverTickf overrides
	// the ~1s poll scheduler (nil = tea.Tick). serverTickSeq /
	// serverTickWait are the tick lifecycle tokens: leaving the page or
	// stopping the server bumps the sequence so an in-flight (un-
	// cancelable) tea.Tick turns stale and no new tick is armed.
	// serverDlg is the start-form modal — the §E ConnectDialog machinery
	// reused with Title/EnterLabel — and serverConfirm the
	// widgets.ConfirmDialog opened by `s` while connections are live.
	// serverStartAt (zero = stopped) / serverPort / serverHeader are the
	// running truth; serverSnap is the last stats snapshot — refreshed
	// only by ticks while running and frozen at the stop-time value once
	// stopped; serverStarting marks an in-flight start.
	server         *pages.Server
	serverSnap     *app.ServerStats
	serverStartAt  time.Time
	serverPort     string
	serverHeader   string
	serverError    string
	serverStarting bool
	serverDlg      *pages.ConnectDialog
	serverConfirm  *widgets.ConfirmDialog
	serverTickSeq  uint64
	serverTickWait bool
	serverTickf    func(time.Duration, func() tea.Msg) tea.Cmd
	serveStartFn   func(port, header, spec, txPath, routesFile string) error
	serveStopFn    func() error
	serveStatsFn   func() *app.ServerStats
	serveRoutesFn  func() []config.MockRouteConfig

	// Workers wiring (SCR-508): workers is the canonical §H page
	// instance (hotkey slot 5).
	// Root is the bus's worker-event consumer: updateBridgeMsg folds
	// WorkerStarted/WorkerProgress/WorkerStopped into workerRows (the
	// row cache is the table's truth — App Workers() snapshots enrich
	// live rows, events flip terminal ones; stops never write status
	// optimistically). workerRing holds the TPS sparkline samples,
	// workerRuns the stress start-form parameters (expected counts,
	// ETA), workersSummary the latest finished summary, and
	// workersStatus the root-stamped action line. workerWiz is the
	// worker start wizard modal (UAT round 4: the b/t start options
	// became a tx ▸ rate/params ▸ run wizard, not two form dialogs),
	// workersConfirm the §N3 stop-all/quit-with-workers confirm
	// (confirmQuit marks a pending quit over a pending stop-all), and
	// workerTickSeq/Wait/workerTickf the runtime-refresh tick tokens
	// (the §G seq pattern reused). The *Fn fields override the app
	// worker-manager legs (nil = the App methods — the same
	// WorkerStart/StressStart/WorkerStop/WorkerStopAll/
	// StressSummaryByID entry points the CLI shims drive).
	workers         *pages.Workers
	workerRows      map[string]*workerRowState
	workerRing      []float64
	workerRuns      map[string]workerRunParams
	workersSummary  *pages.StressSummaryState
	workersStatus   string
	workerWiz       *pages.WorkerWizard
	workersConfirm  *widgets.ConfirmDialog
	confirmQuit     bool
	workerTickSeq   uint64
	workerTickWait  bool
	workerTickf     func(time.Duration, func() tea.Msg) tea.Cmd
	workerStartFn   func(name string, count int, interval time.Duration) (string, error)
	stressStartFn   func(names []string, tps int, ramp, duration time.Duration, workers int) (string, error)
	workerStopFn    func(id string) error
	workerStopAllFn func() error
	workerSummaryFn func(id string) (*app.StressSummary, error)

	// Sessions wiring (SCR-509): sessions is the canonical §I page
	// instance (hotkey slot 6). Root owns every DB read through
	// the App read façade — sessionsSrc overrides those legs for
	// tests (nil = the App methods; nil App = no leg, the page keeps
	// its empty state). The caches (list/stats/history/review) are the
	// page's truth, folded from query-result msgs only;
	// sessionsNote carries the typed façade error as empty-state text.
	// sessionsSeq + the three wait flags are the load lifecycle (the
	// serverTickSeq pattern): a list load bumps the seq, so in-flight
	// detail/review results from the old generation turn stale.
	// sessionsDirty is set by WorkerStopped bus events and by a Done
	// send run (both write the DB); the wrapper's armSessions re-queries
	// while the page is current.
	sessions            *pages.Sessions
	sessionsSrc         sessionsSource
	sessionsList        []app.DbSessionView
	sessionsStats       *app.DbSessionStats
	sessionsHistory     []app.DbTransactionView
	sessionsReview      *app.DbTransactionRetrospective
	sessionsNote        string
	sessionsSelected    string
	sessionsLoaded      bool
	sessionsDirty       bool
	sessionsDetailStale bool
	sessionsSeq         uint64
	sessionsListWait    bool
	sessionsDetailWait  bool
	sessionsReviewWait  bool

	// Session-stats wiring (proposal 05 §3 P4): the §A SESSION card's
	// async counters. sessionStatsFn overrides the App.SessionStats leg
	// (nil = the App method; nil App = no leg, the card keeps its dashes)
	// and sessionStatsTickf the ~2s scheduler (nil = tea.Tick).
	// sessionStatsSeq / sessionStatsWait / sessionStatsDirty are the tick
	// lifecycle (the serverTickSeq pattern): leaving the dashboard with no
	// pending re-read bumps the seq so an in-flight read turns stale,
	// while a send/worker completion dirties the read so one query still
	// runs off-page. sessionStatsSnap is the last successful snapshot the
	// card renders (failures leave the previous values).
	sessionStatsFn    func(ctx context.Context, sessionID string) (*app.DbSessionStats, error)
	sessionStatsTickf func(time.Duration, func() tea.Msg) tea.Cmd
	sessionStatsSnap  *app.DbSessionStats
	sessionStatsSeq   uint64
	sessionStatsWait  bool
	sessionStatsDirty bool

	// homeDir caches os.UserHomeDir (resolved once at construction) so
	// the SESSION card can shorten "~/..." db paths without per-Update
	// environment work.
	homeDir string

	// Analyze wiring (SCR-510): analyze is the canonical §J page instance
	// (registry[6]; the wire-compat slot id "analyze" stays, the frame tab
	// title is the §J one). Root owns the wizard state machine and every
	// engine leg through the injectable analyzeSrc (nil = the App façade;
	// nil App = no leg). analyzeSeq is the load lifecycle token (the
	// serverTickSeq pattern): step jumps, leaves, and aborts bump it so
	// in-flight spec/enum/run/write results turn stale and cancel.
	// analyzeSelected holds the run's per-direction flows (UAT round 7);
	// analyzeRecents remembers capture picks; analyzeFlowFilter is the run
	// step's "/" filter; analyzeRunStale re-arms Enter on change; confirm §N3.
	analyze               *pages.Analyze
	analyzeSrc            analyzeSource
	analyzeSeq            uint64
	analyzeStep           int
	analyzeStatus         string
	analyzeGoal           string
	analyzeSpecPath       string
	analyzeHeader         string
	analyzeCapturePath    string
	analyzeMaskRaw        bool
	analyzeSelected       []app.FlowSelection
	analyzeRecents        []string
	analyzeFlowFilter     string
	analyzeOutputPath     string // [o]-edited output file for generated items ("" = engine default, UAT round 5)
	analyzeFlows          []app.AnalyzeFlowView
	analyzeParsed         int
	analyzeUnparsable     int
	analyzeUnparsableRows []pages.AnalyzeUnparsableRow // unparsable-message reviewer roster (UAT round 6)
	analyzeUnparsableID   int                          // bumped per enum: re-arms the reviewer
	analyzePrefilled      bool
	analyzeSpecWait       bool
	analyzeEnumWait       bool
	analyzeRunWait        bool
	analyzeWriteWait      bool
	analyzeRunStale       bool
	analyzeOutput         *app.AnalyzeOutput
	analyzeRunStart       time.Time
	analyzeElapsed        string
	analyzePreview        string
	analyzeItemRows       []pages.AnalyzeItemRow // generated-item picker roster (UAT round 6)
	analyzeExcluded       []string               // picker-deselected item keys
	analyzeItemsID        int                    // bumped per run attach: re-opens the picker
	analyzeWriteLine      string
	analyzeWriteOK        bool
	analyzeNote           string
	analyzeSpecError      string
	analyzeCaptureError   string
	analyzeConfirm        *widgets.ConfirmDialog

	// Analyze write gate (E5-FIX/B2): analyzeStatFn overrides the
	// overwrite stat for `w` (tests; nil = os.Stat),
	// analyzeOverwriteConfirm is the §N3 overwrite confirm (default
	// No — a same-named item set is never silently replaced), and
	// analyzeWriteCancel cancels the in-flight write leg on abort or
	// leave (the double-SaveItems guard).
	analyzeStatFn           func(string) (os.FileInfo, error)
	analyzeOverwriteConfirm *widgets.ConfirmDialog
	analyzeWriteCancel      context.CancelFunc

	// Ctf wiring (SCR-511): ctf is the canonical §K page instance (a
	// registry entry after the 8 hotkey slots — like §F scenarios, the
	// 1..8 keys keep their wire-compat targets; the palette ":ctf"
	// jump resolves it). Root owns every DB read, preview, and write
	// through the injectable ctfSrc (nil = the App façade; nil App =
	// no leg, the page keeps its empty state). ctfSeq is the load
	// lifecycle token (the serverTickSeq pattern): generate, write,
	// leave, and abort bump it so in-flight list/preview/write results
	// turn stale and cancel. ctfParams carries the committed form
	// values; ctfSummary the last preview (the SUMMARY line + overlay
	// source) with its overwrite stat; ctfPreviewID re-arms the
	// overlay per push; ctfWriteLine/WriteOK the toast-style write
	// result; ctfConfirm the §N3 overwrite confirm (the os.Stat leg
	// runs root-side off the UI thread; default No). ctfStatFn
	// overrides os.Stat for tests.
	ctf             *pages.Ctf
	ctfSrc          ctfSource
	ctfSeq          uint64
	ctfList         []app.CtfSessionView
	ctfSelected     string
	ctfNote         string
	ctfListLoaded   bool
	ctfListWait     bool
	ctfListDirty    bool
	ctfPrefilled    bool
	ctfParams       pages.CtfParams
	ctfSummary      *app.CtfExportSummary
	ctfSummaryID    string
	ctfPreviewID    int
	ctfOverlayOpen  bool
	ctfPreviewWait  bool
	ctfPreviewOverw bool
	ctfWriteWait    bool
	ctfWriteLine    string
	ctfWriteOK      bool
	ctfConfirm      *widgets.ConfirmDialog
	ctfStatFn       func(string) (os.FileInfo, error)

	// Settings wiring (SCR-512): settings is the canonical §L page
	// instance (a registry entry after the 8 hotkey slots — like §F
	// and §K, the palette ":settings" jump resolves it). Root owns
	// every config read/apply/save through the injectable settingsSrc
	// (nil = the App façade; nil App = no leg, the page keeps its
	// empty state). settingsSeq is the load lifecycle token (the
	// serverTickSeq pattern): commit, save, leave, and refresh legs
	// bump it so in-flight results turn stale. settingsView caches
	// the last snapshot (sync pushes display data only — Update
	// never does file I/O); settingsDirty arms the reload;
	// settingsErrs carries per-field validation text inline;
	// settingsChanged holds the keys committed since the last save
	// (the changed-only save patch); settingsSaveOpen drives the [w]
	// confirm overlay; settingsSavedLine/OK the toast-style result.
	// Picker/toast plumbing (TUI-406b): filePick is the shared FilePicker
	// modal (nil = closed; owns the keyboard while open, Esc via the widget's
	// own cancel); filePickTarget is the §L key a selection commits through;
	// filePickRootFn overrides the start dir (tests). toast is the shared
	// Toast stack (armToastTick prunes by toastTTL; toastTickf overrides it).
	// txFilePickFromB marks a tx-file pick opened from §B (not the §L grid)
	// so its result surfaces there; txFileLoadErr is why a picked file did
	// not load (UAT round 7: never fail the transactions page in silence).
	filePick        *widgets.FilePicker
	filePickTarget  string
	filePickRootFn  func(key, value string) (root, label string)
	txFilePickFromB bool
	txFileLoadErr   string
	toast           *widgets.Toast
	toastTTL        time.Duration
	toastTickWait   bool
	toastTickf      func(time.Duration, func() tea.Msg) tea.Cmd

	settings          *pages.Settings
	settingsSrc       settingsSource
	settingsSeq       uint64
	settingsView      *app.SettingsView
	settingsLoaded    bool
	settingsDirty     bool
	settingsLoadWait  bool
	settingsApplyWait bool
	settingsSaveWait  bool
	settingsErrs      map[string]string
	settingsChanged   map[string]string
	settingsSaveOpen  bool
	settingsSavedLine string
	settingsSavedOK   bool
	settingsNote      string

	width, height int
}
