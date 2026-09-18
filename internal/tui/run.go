package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/bridge"
	"jiso/internal/utils"
)

// wireSenders installs the program's Send seam on every live-operation
// leg the model runs in a goroutine: the send stages, the
// connect attempts, and the scenario run. run()
// calls it once per session; tests call it directly with a collector.
// The scenario leg was once left unwired here, so §F silently
// dropped every step/done message and wedged at "running" forever —
// this single seam (plus its regression test) keeps all three wired
// together.
func wireSenders(m *RootModel, send bridge.Sender) {
	m.SetSendSender(send)
	m.SetConnectSender(send)
	m.SetScenarioSender(send)
}

// Run launches the full-screen TUI session and blocks until it terminates.
// Terminal lifecycle is owned by tea.Program per the design contract, and
// v2.0.9 implements every non-negotiable without app-side signal handling:
//
//   - Alt screen: declared per frame via View.AltScreen (root.go View);
//     the cursed renderer writes ansi SetModeAltScreenSaveCursor on the
//     first flush (cursed_renderer.go:354-362, 562-572).
//   - Restore on normal quit and program error: Program.Run defers
//     p.shutdown(killed) → stopRenderer + restoreTerminalState
//     (tea.go:1180-1187, 1261-1283).
//   - Restore on panic inside Update/View: Run's deferred recover
//     (tea.go:1026-1033) converts the panic into ErrProgramKilled+
//     ErrProgramPanic and recoverFromPanic calls p.shutdown(true)
//     (tea.go:1284-1306), which still closes the renderer — and
//     cursedRenderer.close writes the alt-screen exit sequence
//     unconditionally (cursed_renderer.go:175-201, 257-265). So the
//     terminal is never left in alt screen after a panic, and this
//     package must not add its own recover or restore (never
//     "defer p.RestoreTerminal" per contract).
//
// This function only wires the model and the program, never installs signal
// handlers, and never calls os.Exit. A clean quit returns nil; application
// may be nil only in tests.
//
// Event bridge: the model subscribes to the app's event
// bus and pumps it into the program via program.Send. stopBridge runs on
// every path after program.Run returns — normal quit, program error, or the
// v2-recovered panic — so no bridge pump goroutine outlives Run (the pump's
// ctx is the caller's and program exit does not cancel it; the source-close
// backstop is the deferred unsubscribe).
func Run(ctx context.Context, application *app.App) error {
	return run(ctx, NewRootModel(application))
}

// run is the program-wiring seam Run uses; opts are appended after
// WithContext so lifecycle tests inject WithInput/WithOutput/WithWindowSize
// (the idiom bubbletea's own tea_test.go:556-577 uses to drive Run without
// a TTY, including its panic-exit assertions).
func run(ctx context.Context, model *RootModel, opts ...tea.ProgramOption) error {
	// Debug side channels, both env-gated and nil when off:
	// $JISO_DEBUG opens <state>/tui.log for lifecycle milestones,
	// $JISO_PROFILE binds loopback pprof for the session's lifetime.
	dbg := newDebugLogger()
	model.setDebug(dbg)

	defer dbg.close()

	stopProfile, _ := maybeStartProfile(dbg)

	dbg.logf("program start")

	// Terminal hygiene before the alt screen opens (UAT): app.New wires
	// the service with debugMode=true for legacy REPL parity, and that
	// mode makes the connection manager hex-dump every send/receive plus
	// trace prints onto os.Stderr. Raw stderr writes between frames
	// corrupt the cursed renderer's diff — the §D panes rendered
	// "SENDING MESSAGE:" dumps through their own field tables. The TUI
	// owns its own output (the §D Describe tables and the h hex view),
	// so the side channel is switched off for the whole session.
	silenceServiceDebug(model.App())
	// Warm the persisted counters before the program (and its stderr
	// capture) exists: each prints a one-time init line, and landing
	// those lines in the scrollback beats a mid-send write inside the
	// alt screen — the RRN init line used to smash the §F pane borders
	// on the first scenario step that auto-filled field 37 .
	// They must NOT be touched after installServerLogSink: its writer
	// forwards synchronously through program.Send, which blocks until
	// Run is consuming.
	utils.GetCounter()
	utils.GetRRNInstance()

	program := tea.NewProgram(model, append([]tea.ProgramOption{tea.WithContext(ctx)}, opts...)...)

	// Live-operation senders: every goroutine leg (send stages,
	// connect attempts, scenario runs) reports through
	// program.Send; tests replace the seams via the Set*Sender setters.
	wireSenders(model, program.Send)

	// Internal/server's mock-server output is captured into the §4
	// page's LOG ring for the session and restored on exit — raw
	// stderr writes used to corrupt the alt screen mid-frame. Other
	// system output (internal/connection, utils, transactions) is not
	// captured: an alt-screen TUI must not surface stdout noise, so
	// those lines fall to the terminal's scrollback instead.
	restoreServerLog := installServerLogSink(program.Send)
	defer restoreServerLog()

	if application := model.App(); application != nil {
		if bus := application.Events(); bus != nil {
			src, unsubscribe := bus.Subscribe()
			model.setEventWiring(ctx, program.Send)
			model.SetEventSource(src)

			defer unsubscribe()
		}
	}

	_, err := program.Run()

	// Shutdown ordering: stop the pump before returning on all
	// exit paths. program.Run has by now cancelled v2's internal ctx and
	// joined its handler goroutines; the pump goroutine itself is detached
	// in v2's command runner (tea.go:727-740), so this Stop is the join
	// point's signal and tests poll for the goroutine's exit.
	model.stopBridge()

	if stopProfile != nil {
		stopProfile()
	}

	dbg.logf("program exit reason=%s", exitReason(err))

	return err
}

// silenceServiceDebug switches off the service debug side channel for
// the session: the connection manager's SENDING/RECEIVED hex dumps and
// trace prints go to os.Stderr and would smash the alt-screen frames.
// A nil application or service (test roots) is a no-op.
func silenceServiceDebug(application *app.App) {
	if application == nil {
		return
	}
	if svc := application.Service(); svc != nil {
		svc.SetDebugMode(false)
	}
}
