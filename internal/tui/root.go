package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/palette"
)

// NewRootModel wires the page registry — the 1..8 hotkey slots in
// Order, then the non-hotkey pages (§C inspector, §D send
// exchange, §L settings). Boot lands on the dashboard. Application may
// be nil in tests; the list pages refresh via their sync* every Update.
func NewRootModel(application *app.App) *RootModel {
	m := &RootModel{
		app: application, keys: newGlobalKeyMap(),
		resizeWindow: defaultResizeCoalesceWindow,
		now:          time.Now,
		dashActions:  palette.DashboardActions(),
		// Mouse defaults ON; Go's zero value would silently
		// disable the whole mouse leg (F9 toggles, see hitmap_mouse.go).
		mouseEnabled: true,
	}
	m.newPages()
	m.registerPages()
	m.syncAll()

	return m
}

// App returns the internal/app façade this frontend drives.
func (m *RootModel) App() *app.App { return m.app }

// setDebug installs the lifecycle logger (nil disables). run calls it
// once per session from $JISO_DEBUG; unit tests inject a manual logger.
func (m *RootModel) setDebug(d *debugLogger) { m.debug = d }

// Init implements tea.Model; the program pushes no startup commands yet.
func (m *RootModel) Init() tea.Cmd { return nil }

// Update implements tea.Model: pure state transitions plus tea.Cmd
// returns, never I/O; the one side effect is arming the event bridge
// (itself a Cmd) when SetEventSource installed a source.
func (m *RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	// A finished send run wrote the session DB: dirty the §I cache and
	// the §A SESSION card's stats read so the wrapper's arms re-query.
	if _, ok := msg.(SendStageMsg); ok && m.sendRun != nil && m.sendRun.state.Done {
		m.sessionsDirty = true
		m.sessionStatsDirty = true
	}
	m.syncPages()
	m.updateOverlays(msg)
	cmd = m.armBatches(cmd)

	return next, cmd
}
