// root_model.go holds the RootModel struct: the root router state the
// root_update/root_keys/root_view/root_stack method files operate on.
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
// global keymap layer. The stack always holds one page; depth 1 is "root"
// (q quits there, deeper q pops).
type RootModel struct {
	app   *app.App
	keys  globalKeyMap
	stack []Page

	// registry holds the canonical page instances behind the 1..8 jump
	// slots; the stack references the live copies.
	registry []Page

	// pal is the command-palette overlay widget; nil means closed.
	pal *palette.Model

	// help is the §M overlay — a frame-level modal above the page stack
	// (never a page in PageIDs); nil means closed. While open it owns the
	// keyboard: `?` toggles, Esc closes first, everything else is swallowed.
	help *helpOverlay

	// errModal is the root-owned error screen (the topmost overlay above
	// the page stack — only toasts compose above it — never a page in
	// PageIDs); nil means closed. While open it owns the keyboard over
	// every overlay: enter/esc close, j/k and pgup/pgdown scroll,
	// everything else is swallowed so the page below stays frozen.
	errModal *errorModal

	// Event bridge: eventSrc is installed by SetEventSource and armed as
	// a Cmd on the next Update; conn is the latest ConnectionEvent the
	// frame's connection slot shows.
	eventSrc      <-chan events.Event
	eventSender   bridge.Sender
	eventCtx      context.Context
	bridge        *bridge.Bridge
	bridgePending bool
	conn          *events.ConnectionEvent

	// Resize coalescing: width/height always hold the latest size;
	// appliedW/H is the size pages last saw and resizePending marks a
	// trailing flush in flight.
	appliedW, appliedH int
	resizePending      bool
	resizeWindow       time.Duration

	// theme overrides the frame/palette theme when set; nil means
	// theme.Default.
	theme *theme.Theme

	// debug is the lifecycle logger (nil = off); nil-safe hooks keep
	// Update I/O-free. run installs it; tests may too.
	debug *debugLogger

	// Dashboard: dash is the canonical page-1 instance (registry[0], a
	// pointer so cursor state survives jumps); root pushes snapshots via
	// SetState. now is the injectable clock; connSince the live start.
	dash      *pages.Dashboard
	now       func() time.Time
	connSince *time.Time
	// connHeader is the framing the live (or last live) link actually
	// speaks, captured at connect; the card and chip render it, never the
	// config value alone.
	connHeader  string
	dashActions []palette.Action

	// Transactions: tx is the canonical page-2 instance (registry[1], a
	// pointer so filter/sort/cursor state survive jumps); root derives
	// its state from the app and pushes it via SetState.
	tx *pages.Transactions

	// Inspector: the canonical page-3 instance (registry[2], a pointer
	// so tab/tree/pool state survive jumps), pushed by Enter on §B.
	// Root builds and pushes its state on the compose-without-send path.
	inspector *pages.Inspector

	// Send: send is the §D live exchange page pushed by TxSendMsg,
	// sendRun the stage-machine truth (nil = never run); lastSend/At
	// hold the last completed run and its frozen end time.
	lastSend   *pages.SendState
	lastSendAt time.Time
	// lastStress is the §A LAST STRESS card snapshot, stamped once when
	// WorkerStopped folds the App summary (never per tick).
	lastStress *pages.LastStressCard
	// The live send op: the goroutine reports SendStageMsg values via
	// sendSender; liveConnect/liveSend override the app legs (nil =
	// production); sendGen numbers runs so superseded tick chains drop.
	send        *pages.Send
	sendRun     *sendRun
	sendGen     uint64
	sendSender  bridge.Sender
	liveConnect func(ctx context.Context) error
	liveSend    func(ctx context.Context, txName string) (*liveExchange, error)

	// Connect dialog: dlg is the §E modal overlay (the stack is never
	// touched while it is open); connectRun is the attempt-loop truth
	// (nil = idle); connectSession remembers values for session prefill.
	dlg              *pages.ConnectDialog
	connectRun       *connectRun
	connectInitiated bool                 // a dialog/wizard-issued attempt is in flight (bus failures outside it are background flaps)
	connectHost      *pages.ConnectDialog // dialog the in-flight stamps target (§E or wizard step 0)
	connectSession   *pages.ConnectFormState
	connectSender    bridge.Sender
	dialConnect      func(ctx context.Context, opts app.ConnectOptions) error
	connectBackoff   func(attempt int) time.Duration

	// Send wizard: wizard is the send-wizard modal overlay; wizardSpec/
	// wizardFile hold the paths committed with the send; wizardFiles is
	// the session's step-2 recents.
	wizard      *pages.SendWizard
	wizardSpec  string
	wizardFile  string
	wizardFiles []string
	// lastSentTemplate is the most recent send's template; the
	// one-keystroke dashboard send reuses it.
	lastSentTemplate string
	sendHistory      *pages.SendHistory       // send-history overlay page
	sends            []pages.SendHistoryEntry // bounded ring of completed sends

	// serverLog is the §4 server page's bounded ring of mock-server
	// output; it renders only on that page's LOG pane, never elsewhere.
	serverLog []string

	// lastConn is the state-dir remembered last-successful connect,
	// the form's prefill source; lastConnLoaded marks the one read.
	lastConn       *app.LastConnection
	lastConnLoaded bool

	// lastServer is the state-dir remembered last mock-server start
	// (prefill source for the §G form); lastServerLoaded marks the read.
	lastServer       *app.LastServerStart
	lastServerLoaded bool

	// Disconnect: disconnectFn overrides the App.Disconnect leg (nil =
	// the app method); disconnectSeq/Wait are its seq-token lifecycle;
	// disconnectConfirm is the §N3 confirm when work is still active.
	disconnectSeq     uint64
	disconnectWait    bool
	disconnectConfirm *widgets.ConfirmDialog
	disconnectFn      func() error

	// Scenarios: the §F page (a registry entry after the hotkey slots;
	// its ops live in root_scenario_run.go / root_scenario_export.go).
	// scenarioDetail carries the step preview and its seq-token load.
	scenarios          *pages.Scenarios
	scenarioRun        *scenarioRun
	scenarioSender     bridge.Sender
	runScenario        scenarioEngine
	scenarioLastReport *transactions.TestReport
	scenarioStatusLine string
	scenarioDetail     scenarioDetailState

	// Scenario spec gate: a run or a step preview that would
	// resolve through the engine default waits on the shared spec browse.
	// Esc drops the pending work — the run with a one-line notice, the
	// preview with its honest line.
	pendingScenarioRun       string
	pendingScenarioPreviewID string
	pendingScenarioPreviewAt int

	// Scenario export single-flight: scenarioWriteWait marks an in-flight
	// `e` leg; scenarioConfirm is the §N3 overwrite confirm (default No);
	// scenarioExportPending carries the confirmed path.
	scenarioWriteWait     bool
	scenarioStatFn        func(string) (os.FileInfo, error)
	scenarioConfirm       *widgets.ConfirmDialog
	scenarioExportPending string

	// scenarioExportPath overrides the export destination
	// (tests; empty = default).
	scenarioExportPath string

	// Server: the canonical §G page instance (registry[4]). Root owns the
	// live serve op; serve*Fn/serverTickf override the app legs (nil =
	// production); serverTickSeq/Wait are the tick lifecycle tokens.
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
	serveSpecFn    func() string

	// Workers: the canonical §H page (hotkey slot 5). Root folds worker
	// bus events into workerRows (the row cache is truth, never written
	// optimistically); worker*Fn override the app legs (nil = the App).
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

	// Sessions: the canonical §I page (hotkey slot 6). Root owns every
	// DB read via the App façade (sessionsSrc overrides it for tests);
	// the caches are page truth, folded from query-result msgs only.
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

	// Session stats: the §A SESSION card's async counters; sessionStatsFn
	// overrides the App leg (nil = production) and the seq/wait/dirty trio
	// reuses the serverTickSeq lifecycle; Snap holds the last good values.
	sessionStatsFn    func(ctx context.Context, sessionID string) (*app.DbSessionStats, error)
	sessionStatsTickf func(time.Duration, func() tea.Msg) tea.Cmd
	sessionStatsSnap  *app.DbSessionStats
	sessionStatsSeq   uint64
	sessionStatsWait  bool
	sessionStatsDirty bool

	// homeDir caches os.UserHomeDir (resolved once at construction) so the
	// SESSION card can shorten "~/..." db paths without per-Update work.
	homeDir string

	// Analyze: the canonical §J page instance (registry[6]). Root owns
	// the wizard and engine legs through analyzeSrc (nil = the App
	// façade); analyzeSeq is the load lifecycle token.
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
	analyzeOutputPath     string // [o]-edited output file for generated items ("" = engine default)
	analyzeFlows          []app.AnalyzeFlowView
	analyzeParsed         int
	analyzeUnparsable     int
	analyzeUnparsableRows []pages.AnalyzeUnparsableRow // unparsable-message reviewer roster
	analyzeUnparsableID   int                          // bumped per enum: re-arms the reviewer
	analyzeSpecWait       bool
	analyzeEnumWait       bool
	analyzeRunWait        bool
	analyzeWriteWait      bool
	analyzeRunStale       bool
	analyzeOutput         *app.AnalyzeOutput
	analyzeRunStart       time.Time
	analyzeElapsed        string
	analyzePreview        string
	analyzeItemRows       []pages.AnalyzeItemRow // generated-item picker roster
	analyzeExcluded       []string               // picker-deselected item keys
	analyzeItemsID        int                    // bumped per run attach: re-opens the picker
	analyzeWriteLine      string
	analyzeWriteOK        bool
	analyzeNote           string
	analyzeSpecError      string
	analyzeCaptureError   string
	analyzeConfirm        *widgets.ConfirmDialog

	// Analyze write gate: analyzeStatFn overrides os.Stat (tests);
	// analyzeOverwriteConfirm is the §N3 overwrite confirm (default No);
	// analyzeWriteCancel cancels the in-flight write leg.
	analyzeStatFn           func(string) (os.FileInfo, error)
	analyzeOverwriteConfirm *widgets.ConfirmDialog
	analyzeWriteCancel      context.CancelFunc

	// Ctf: the §K page (a registry entry after the hotkey slots, like
	// §F). Root owns every DB read, preview, and write through ctfSrc
	// (nil = the App façade); ctfSeq is the load lifecycle token.
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

	// Settings: the §L page (a registry entry after the hotkey slots,
	// like §F and §K). Root owns every config read/apply/save through
	// settingsSrc (nil = the App façade); settingsSeq is the load token.
	// filePick is the shared FilePicker modal (nil = closed; Esc = its
	// cancel); production roots it at "/" so every pick can climb back.
	// pendingTxFile holds a specless tx-file pick that waits on the
	// chained spec-for-file browse (Esc drops it, a spec pick applies
	// both). toast is the Toast stack (armToastTick/toastTickf).
	filePick        *widgets.FilePicker
	filePickTarget  string
	filePickRootFn  func(key, value string) (root, label string)
	txFilePickFromB bool
	txFileLoadErr   string
	pendingTxFile   string
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

	// mouseEnabled gates the whole mouse leg — hit map, MouseModeNone
	// and DECSET arm all follow this one flag (see hitmap_mouse.go).
	mouseEnabled bool
}
