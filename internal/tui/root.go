package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/tui/palette"
)

// NewRootModel wires the page registry — the 1..8 hotkey slots are the
// wireframe's pages in wireframe order (§A dashboard, §B transactions,
// §F scenarios, §G mock server, §H workers, §I sessions, §J analyze,
// §K CTF) and the registry continues with the non-hotkey pages: the §C
// inspector and §D send exchange (drill-downs via Enter / s) and the §L
// settings page (palette-only). Boot lands on the dashboard. Application
// may be nil in tests; screens read it through App(). The real pages
// start from an empty snapshot; the list pages are refreshed by their
// sync* methods on every Update, the inspector on open (its composed
// values must stay stable while the user scrolls).
func NewRootModel(application *app.App) *RootModel {
	m := &RootModel{
		app: application, keys: newGlobalKeyMap(),
		resizeWindow: defaultResizeCoalesceWindow,
		now:          time.Now,
		dashActions:  palette.DashboardActions(),
	}
	m.newPages()
	m.registerPages()
	m.syncAll()

	return m
}

// App returns the internal/app façade this frontend drives.
func (m *RootModel) App() *app.App { return m.app }

// setDebug installs the lifecycle logger (nil disables). run() calls it
// once per session from $JISO_DEBUG; unit tests inject a manual logger.
func (m *RootModel) setDebug(d *debugLogger) { m.debug = d }

// Init implements tea.Model; the program pushes no startup commands yet.
func (m *RootModel) Init() tea.Cmd { return nil }

// Update implements tea.Model. It is pure: state transitions plus tea.Cmd
// returns, never I/O, never os.Exit. The one side effect is arming the
// event bridge (a tea.Cmd — the goroutine lives in the bridge, not here)
// when SetEventSource installed a source since the last Update.
func (m *RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	// A finished send run wrote the session DB (App.Send logs it):
	// mark the §I cache dirty so the wrapper's arm re-queries, and dirty
	// the §A SESSION card's async stats read (proposal 05 §3).
	if _, ok := msg.(SendStageMsg); ok && m.sendRun != nil && m.sendRun.state.Done {
		m.sessionsDirty = true
		m.sessionStatsDirty = true
	}
	m.syncPages()
	m.updateOverlays(msg)
	cmd = m.armBatches(cmd)

	return next, cmd
}
