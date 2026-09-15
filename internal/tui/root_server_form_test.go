// root_server_form_test.go covers the §G start-form root contract
// (SCR-507): "c" opens the form ONLY on the §G page (the global connect
// dialog keeps every other page), prefill comes from the cobra shim's
// sources (9999/binary2 flag defaults + config spec/routes paths), a
// failed start keeps the modal open with the error and never auto-retry,
// Esc closes, and the edited field values are what reach the serve leg.
package tui

import (
	"errors"
	"strings"
	"testing"

	"jiso/internal/app"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

func TestRootServerFormOpensOnlyOnServerPage(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('c') // status page: the connect dialog keeps its hotkey
	if r.m.dlg == nil || r.m.serverDlg != nil {
		t.Fatalf("c on dashboard: connect=%v serverForm=%v", r.m.dlg != nil, r.m.serverDlg != nil)
	}
	_, _ = r.m.Update(special(tea.KeyEsc))
	r.key('4')
	r.key('c')
	if r.m.serverDlg == nil || r.m.dlg != nil {
		t.Fatalf("c on §G: serverForm=%v connect=%v", r.m.serverDlg != nil, r.m.dlg != nil)
	}
}

func TestRootServerFormPrefillFromShimSources(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	st := r.m.serverDlg.State()
	if st.Title != "SERVER" || st.EnterLabel != "start" {
		t.Errorf("form identity = %q/%q, want SERVER/start", st.Title, st.EnterLabel)
	}
	// UAT round 4: no fabricated defaults. A never-started form carries
	// the config's spec/routes and nothing else; port/header stay empty
	// (the header radio renders unselected) and the shim defaults apply
	// only at Enter.
	want := map[string]string{
		"port": "", "header": "",
		"spec": "../../specs/spec.json", "routes": r.tx,
	}
	for k, w := range want {
		if got := serverFormValue(&st, k); got != w {
			t.Errorf("prefill %s = %q, want %q", k, got, w)
		}
	}
	if f := st.Field("header"); f == nil || f.Selected != -1 {
		t.Errorf("header radio Selected = %+v, want unselected (-1)", f)
	}
	if f := st.Field("routes"); f == nil || !f.Browsable {
		t.Errorf("routes field must be browsable ([f] browse hint)")
	}
}

// TestServerFormPrefillsLastStart: a successful start stamps the
// state-dir memory, and a reopened form prefills exactly those values
// (no config, no shim defaults) (UAT round 4).
func TestServerFormPrefillsLastStart(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	st := r.m.serverDlg.State()
	st.Field(serverFieldPort).Value = "9101"
	st.Field(serverFieldHeader).Value = "ascii4"
	st.Field(serverFieldHeader).Selected = 1
	r.m.serverDlg.SetState(st)
	_, cmd := r.m.Update(special(tea.KeyEnter))
	r.run(cmd)
	if !r.m.serverRunning() {
		t.Fatal("fake start did not flip the running truth")
	}

	lc, err := app.LoadLastServerStart()
	if err != nil || lc == nil {
		t.Fatalf("last-server-start must be saved: %+v, %v", lc, err)
	}
	if lc.Port != "9101" || lc.Header != "ascii4" {
		t.Fatalf("saved %+v, want the started 9101/ascii4", *lc)
	}

	// (The successful start already closed the dialog; esc would now
	// unwind to the dashboard — proposal 05 §4 — so the reopen goes
	// straight through "c" on §4.)
	r.key('c')
	after := r.m.serverDlg.State()
	if got := serverFormValue(&after, serverFieldPort); got != "9101" {
		t.Errorf("reopened port = %q, want the remembered 9101", got)
	}
	if got := serverFormValue(&after, serverFieldHeader); got != "ascii4" {
		t.Errorf("reopened header = %q, want the remembered ascii4", got)
	}
}

func TestRootServerFormEscCloses(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	_, _ = r.m.Update(special(tea.KeyEsc))
	if r.m.serverDlg != nil {
		t.Fatal("esc did not close the start form")
	}
	if r.startCount() != 0 {
		t.Fatalf("closing the form started the server %d times", r.startCount())
	}
}

func TestRootServerStartFailureKeepsFormOpen(t *testing.T) {
	r := newServeTestRoot(t)
	r.mu.Lock()
	r.startErr = errServeBusy
	r.mu.Unlock()
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	_, cmd := r.m.Update(special(tea.KeyEnter))
	r.run(cmd)
	if r.m.serverRunning() {
		t.Fatal("failed start flipped the running truth")
	}
	if r.m.serverDlg == nil {
		t.Fatal("failed start closed the form")
	}
	st := r.m.serverDlg.State()
	if st.InFlight || st.Error == "" || !strings.Contains(st.Error, "busy") {
		t.Fatalf("failure line wrong: inflight=%v err=%q", st.InFlight, st.Error)
	}
	if r.startCount() != 1 {
		t.Fatalf("auto-retry after failure: %d starts", r.startCount())
	}
	// The form stays editable: Esc still closes it.
	_, _ = r.m.Update(special(tea.KeyEsc))
	if r.m.serverDlg != nil {
		t.Fatal("esc did not close the failed form")
	}
}

func TestRootServerEditedValuesReachServeLeg(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	st := r.m.serverDlg.State()
	st.Field("port").Value = "8123"
	st.Field("spec").Value = "/tmp/custom-spec.json"
	st.Field(serverFieldRoutes).Value = "/tmp/routes-only.json"
	r.m.serverDlg.SetState(st)
	_, cmd := r.m.Update(special(tea.KeyEnter))
	r.run(cmd)
	if !r.m.serverRunning() {
		t.Fatal("start did not flip running")
	}
	if r.m.serverPort != "8123" {
		t.Errorf("running port = %q, want 8123", r.m.serverPort)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last.port != "8123" || r.last.spec != "/tmp/custom-spec.json" {
		t.Fatalf("serve leg got %+v", r.last)
	}
	// Finding 1 (D1c): the "Routes file" value reaches the serve leg as the
	// routes-file argument, never as a fabricated tx-path fallback.
	if r.last.routesFile != "/tmp/routes-only.json" || r.last.txPath != "" {
		t.Fatalf("routes wiring = txPath %q routesFile %q, want \"\" and /tmp/routes-only.json",
			r.last.txPath, r.last.routesFile)
	}
}

func TestRootServerViewOverlays(t *testing.T) {
	r := newServeTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	view := r.m.View().Content
	if !strings.Contains(view, "SERVER") || !strings.Contains(view, "Routes file") {
		t.Fatalf("form overlay missing from View:\n%s", view)
	}
	_, _ = r.m.Update(special(tea.KeyEsc))

	// Stop-confirm composition over the running page body.
	r.goPage(t)
	r.key('s')
	if r.m.serverConfirm == nil {
		t.Fatal("no confirm for the view check")
	}
	if view := r.m.View().Content; !strings.Contains(view, "stop mock server :9999 with 3 live connection(s)?") {
		t.Fatalf("confirm line missing from View:\n%s", view)
	}
}

func TestRootServerPageSnapshotWired(t *testing.T) {
	r := newServeTestRoot(t)
	r.goPage(t)
	page, ok := r.m.registry[3].(*pages.Server)
	if !ok {
		t.Fatalf("registry slot 4 = %T", r.m.registry[3])
	}
	if id := page.ID(); id != pages.ServerPageID {
		t.Errorf("page ID = %q, want %q", id, pages.ServerPageID)
	}
}

var errServeBusy = errors.New("listen :9999: address already in use (busy)")

// upd delivers any msg and follows its Cmd chain.
func (r *serveTestRoot) upd(msg tea.Msg) {
	_, cmd := r.m.Update(msg)
	r.run(cmd)
}

// TestServerFormFieldPicker: [f] on the §G form's spec/routes fields
// opens the shared file picker and the pick lands in the focused field
// preserving the other values; [f] on port/header is inert (UAT round
// 3: the routes/tx-file field had no browse).
func TestServerFormFieldPicker(t *testing.T) {
	r := newServeTestRoot(t)
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	if r.m.serverDlg == nil {
		t.Fatal("server form did not open")
	}

	focusKey := func() string {
		st := r.m.serverDlg.State()

		return focusedFormFieldKey(&st, r.m.serverDlg.Focus())
	}

	if focusKey() != serverFieldPort {
		t.Fatalf("focus = %q, want %q", focusKey(), serverFieldPort)
	}

	r.key('f')
	if r.m.filePick != nil {
		t.Fatal("[f] on the port field must be inert")
	}

	// Edit the port so the pick-preservation is observable, then walk
	// down to the routes field and browse.
	st := r.m.serverDlg.State()
	st.Field(serverFieldPort).Value = "9101"
	r.m.serverDlg.SetState(st)

	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	r.upd(tab)
	r.upd(tab)
	r.upd(tab)
	if focusKey() != serverFieldRoutes {
		t.Fatalf("focus = %q, want %q", focusKey(), serverFieldRoutes)
	}

	r.key('f')
	if r.m.filePick == nil {
		t.Fatal("[f] on the routes field must open the picker")
	}

	r.upd(widgets.FilePickedMsg{Path: "/tmp/tx2.json"})
	if r.m.filePick != nil {
		t.Fatal("picker must close after the pick")
	}
	after := r.m.serverDlg.State()
	if got := serverFormValue(&after, serverFieldRoutes); got != "/tmp/tx2.json" {
		t.Errorf("routes field = %q, want /tmp/tx2.json", got)
	}
	if got := serverFormValue(&after, serverFieldPort); got != "9101" {
		t.Errorf("port field = %q, want the preserved 9101", got)
	}
}

// serverFieldIdx resolves one §G field's index in render order (the
// two-mode tests focus a named field without hard-coding its position).
func serverFieldIdx(t *testing.T, d *pages.ConnectDialog, key string) int {
	t.Helper()

	for i, f := range d.State().Fields {
		if f.Key == key {
			return i
		}
	}
	t.Fatalf("no §G field %q", key)

	return -1
}

// TestServerFormTypingLetterFInFieldDoesNotOpenPicker pins the two-mode
// contract (UAT round 8 finding 2 / D3): in NAVIGATE mode `f` is the
// file-pick key for the focused browsable field; typing enters EDIT mode
// (the first printable types itself), and there `f` types literally into
// the field — the picker must NOT open (the confirmed §G leak: `f` could
// never be typed into a form field).
func TestServerFormTypingLetterFInFieldDoesNotOpenPicker(t *testing.T) {
	r := newServeTestRoot(t)
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	if r.m.serverDlg == nil {
		t.Fatal("server form did not open")
	}
	r.m.serverDlg.SetFocus(serverFieldIdx(t, r.m.serverDlg, serverFieldRoutes))
	if r.m.serverDlg.Editing() {
		t.Fatal("the form must open in navigate mode")
	}

	// Navigate mode: f opens the picker (D3, unchanged).
	r.key('f')
	if r.m.filePick == nil {
		t.Fatal("[f] in navigate mode must open the picker")
	}
	r.upd(special(tea.KeyEscape)) // the picker closes; the form stays open
	if r.m.filePick != nil {
		t.Fatal("esc must close the picker")
	}
	if r.m.serverDlg == nil {
		t.Fatal("closing the picker closed the form")
	}

	// Typing enters edit mode (and types the first character itself).
	r.upd(ch('m'))
	if !r.m.serverDlg.Editing() {
		t.Fatal("typing must enter edit mode")
	}

	// Edit mode: f types literally into the field; the picker stays closed.
	r.upd(ch('f'))
	if r.m.filePick != nil {
		t.Fatal("'f' while editing opened the picker; it must type into the field")
	}
	after := r.m.serverDlg.State()
	if got := serverFormValue(&after, serverFieldRoutes); !strings.HasSuffix(got, "mf") {
		t.Fatalf("routes field = %q, want the typed suffix \"mf\"", got)
	}
}

// TestServerFormEscLeavesFieldBeforeScreen pins the esc order of the
// two-mode form (D3): the first esc leaves EDIT mode — the field stays
// focused with its typed value, the form stays open, nothing starts —
// and only the second esc (navigate mode) leaves the screen.
func TestServerFormEscLeavesFieldBeforeScreen(t *testing.T) {
	r := newServeTestRoot(t)
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	r.m.serverDlg.SetFocus(serverFieldIdx(t, r.m.serverDlg, serverFieldRoutes))

	r.upd(ch('x')) // typing enters edit mode
	if !r.m.serverDlg.Editing() {
		t.Fatal("typing must enter edit mode")
	}

	r.upd(special(tea.KeyEscape))
	if r.m.serverDlg == nil {
		t.Fatal("esc from edit mode left the screen; it must leave the field first")
	}
	if r.m.serverDlg.Editing() {
		t.Fatal("esc must leave edit mode")
	}
	stAfter := r.m.serverDlg.State()
	if got := focusedFormFieldKey(&stAfter, r.m.serverDlg.Focus()); got != serverFieldRoutes {
		t.Fatalf("focus after esc = %q, must stay on the routes field", got)
	}
	after := r.m.serverDlg.State()
	if got := serverFormValue(&after, serverFieldRoutes); !strings.HasSuffix(got, "x") {
		t.Fatalf("esc edited the value %q, must keep it", got)
	}
	if r.startCount() != 0 {
		t.Fatalf("leaving the field started the server %d times", r.startCount())
	}

	r.upd(special(tea.KeyEscape))
	if r.m.serverDlg != nil {
		t.Fatal("esc in navigate mode must leave the screen")
	}
}

// TestServerLogStaysOnServerPage: mock-server lines render ONLY inside
// the §4 page's LOG pane — never the global console strip, never the
// dashboard or any other screen (UAT round 3: "[SERVER]" fragments
// smeared across §D).
func TestServerLogStaysOnServerPage(t *testing.T) {
	r := newServeTestRoot(t)
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})

	const line = "[SERVER] Matched Route Echo for MTI 0800 -> Responding 0810 (RC: 00)"
	r.upd(serverLineMsg{text: line})

	if len(r.m.serverLog) != 1 {
		t.Fatalf("serverLog = %d lines, want 1", len(r.m.serverLog))
	}
	if len(r.m.console) != 0 {
		t.Fatalf("console strip must not carry server lines, got %v", r.m.console)
	}

	r.key('4')
	if v := r.m.View().Content; !strings.Contains(v, "[SERVER] Matched Route") || !strings.Contains(v, "LOG") {
		t.Errorf("server page view must show the LOG pane:\n%s", v)
	}

	r.key('1')
	if v := r.m.View().Content; strings.Contains(v, "Responding 0810") {
		t.Errorf("dashboard must not show server output:\n%s", v)
	}
}
