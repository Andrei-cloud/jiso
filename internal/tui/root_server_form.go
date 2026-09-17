// root_server_form.go owns the §G start-form data: the form reuses the
// §E connect-dialog machinery wholesale (pages.ConnectDialog with
// Title/EnterLabel set) and root prefills it from the SAME sources the
// cobra `serve start` shim reads — port/header are that shim's flag
// defaults, spec and routes file are the config's paths. Enter starts
// the server through the app's in-process serve façade; a failure keeps
// the modal open with the error line, success closes and flips the truth.
package tui

import (
	"path/filepath"
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Field keys of the §G start form, in render order (wireframe: "port ·
// header · spec · routes file").
const (
	serverFieldPort     = "port"
	serverFieldHeader   = "header"
	serverFieldSpecPath = "spec"
	serverFieldRoutes   = "routes"
)

// serverDefaultPort / serverDefaultHeader mirror the cobra `serve start`
// flag defaults — the shim's only port/header source.
const (
	serverDefaultPort   = "9999"
	serverDefaultHeader = "binary2"
)

// serverPickSpecTarget / serverPickRoutesTarget route the §G form's [f]
// browse picks back through applyFilePicked.
const (
	serverPickSpecTarget   = "server:spec"
	serverPickRoutesTarget = "server:routes"
)

// serverFilePickerKey matches f while the server start form is open.
var serverFilePickerKey = key.NewBinding(key.WithKeys("f"))

// serverFormRow is the static shape of one §G row.
type serverFormRow struct {
	key      string
	label    string
	kind     pages.FieldKind
	options  []string
	def      string
	note     string
	noteKind pages.NoteKind
}

func serverFormFieldSpecs() []serverFormRow {
	return []serverFormRow{
		{key: serverFieldPort, label: "Port", kind: pages.FieldText, def: serverDefaultPort},
		{
			key: serverFieldHeader, label: "Header", kind: pages.FieldRadio,
			options: pages.ConnectHeaderOptions, def: serverDefaultHeader,
		},
		{
			key: serverFieldSpecPath, label: "Spec file", kind: pages.FieldText,
			note: "(default spec if empty)", noteKind: pages.NoteInfo,
		},
		{
			key: serverFieldRoutes, label: "Routes file", kind: pages.FieldText,
			note: "(routes-only file, optional)", noteKind: pages.NoteInfo,
		},
	}
}

// openServerForm opens (or re-focuses) the §G start-form overlay on the
// current page — the openConnect modal pattern, page stack untouched.
func (m *RootModel) openServerForm() (tea.Model, tea.Cmd) {
	if m.serverDlg != nil {
		return m, nil
	}
	th := m.theme
	if th == nil {
		th = theme.Default()
	}
	d := pages.NewConnectDialog(th)
	d.SetState(m.buildServerForm())
	if m.width > 0 {
		_, _ = d.Update(m.innerWS())
	}
	m.serverDlg = d
	m.debug.logf("server form open")

	return m, nil
}

// buildServerForm assembles the §G snapshot with no fabricated defaults:
// every field prefills from the last SUCCESSFUL start (state-dir memory),
// the config fills spec/routes when memory leaves them unset, everything
// else stays empty. A header radio with nothing remembered renders
// unselected; Enter then falls back to the shim defaults at start.
func (m *RootModel) buildServerForm() pages.ConnectFormState {
	cfg := m.configOrNil()
	last := m.ensureLastServer()
	st := pages.ConnectFormState{Title: "SERVER", EnterLabel: "start"}

	for _, sp := range serverFormFieldSpecs() {
		f := pages.FormField{
			Key: sp.key, Label: sp.label, Kind: sp.kind,
			Options: append([]string(nil), sp.options...),
			Note:    sp.note, NoteKind: sp.noteKind,
			Enabled: true,
		}
		f.Value = serverPrefill(sp, cfg, last)
		switch {
		case f.Kind == pages.FieldRadio && f.Value == "":
			f.Selected = -1 // nothing remembered: no dot lit
		case f.Kind == pages.FieldRadio:
			f.Selected = connectOptionIndex(f.Options, f.Value, "")
			if f.Selected >= 0 {
				f.Value = f.Options[f.Selected]
			}
		case sp.key == serverFieldSpecPath || sp.key == serverFieldRoutes:
			f.Browsable = true // the dialog footer shows [f] browse
		}
		st.Fields = append(st.Fields, f)
	}

	return st
}

// ensureLastServer reads the state-dir last-start memory once per
// session (a malformed file is treated as absent; the debug log keeps
// the detail).
func (m *RootModel) ensureLastServer() *app.LastServerStart {
	if m.lastServerLoaded {
		return m.lastServer
	}
	m.lastServerLoaded = true

	ls, err := app.LoadLastServerStart()
	if err != nil {
		m.debug.logf("last-server-start load: %v", err)

		return nil
	}
	m.lastServer = ls

	return ls
}

// rememberLastServer stamps the started parameters into the state-dir
// file (best-effort) and refreshes the in-session copy so a reopened
// form prefills immediately.
func (m *RootModel) rememberLastServer(msg serverStartResultMsg) {
	ls := &app.LastServerStart{
		Port: msg.port, Header: msg.header, Spec: msg.spec, Routes: msg.routes,
	}
	m.lastServer, m.lastServerLoaded = ls, true

	if err := app.SaveLastServerStart(*ls); err != nil {
		m.debug.logf("last-server-start save: %v", err)
	}
}

// serverPrefill resolves one field's initial value: last start wins;
// the config fills spec/routes only; port/header are never fabricated
// (see buildServerForm for the contract).
func serverPrefill(sp serverFormRow, cfg *config.Config, last *app.LastServerStart) string {
	if last != nil {
		switch sp.key {
		case serverFieldPort:
			if last.Port != "" {
				return last.Port
			}
		case serverFieldHeader:
			if last.Header != "" {
				return last.Header
			}
		case serverFieldSpecPath:
			if last.Spec != "" {
				return last.Spec
			}
		case serverFieldRoutes:
			if last.Routes != "" {
				return last.Routes
			}
		}
	}

	if cfg != nil {
		switch sp.key {
		case serverFieldSpecPath:
			if v := strings.TrimSpace(cfg.GetSpec()); v != "" {
				return v
			}
		case serverFieldRoutes:
			if v := strings.TrimSpace(cfg.GetFile()); v != "" {
				return v
			}
		}
	}

	return ""
}

// updateServerFormKey routes one key while the start form owns the
// keyboard: Esc closes (ignored while a start is in flight), but while
// a field is being typed into the first Esc leaves the FIELD instead;
// Enter starts; everything else edits the form. [f] browses only in
// navigate mode — while editing, f is a literal and reaches the field.
func (m *RootModel) updateServerFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, connectKeyEsc):
		if m.serverStarting {
			return m, nil
		}
		if m.serverDlg.Editing() {
			_, _ = m.serverDlg.Update(msg) // esc leaves the field before the screen

			return m, nil
		}
		m.serverDlg = nil
		m.debug.logf("server form close")

		return m, nil

	case key.Matches(msg, connectKeyEnter):
		if m.serverStarting {
			return m, nil
		}

		return m.startServer()

	case key.Matches(msg, serverFilePickerKey) && !m.serverDlg.Editing():
		// [f] browses the focused path field (spec or routes/tx file);
		// on port/header it is inert (those are not files). While the
		// form is being typed into this case does not fire and f falls
		// through to the field below.
		st := m.serverDlg.State()
		var target, value string

		switch focusedFormFieldKey(&st, m.serverDlg.Focus()) {
		case serverFieldSpecPath:
			target, value = serverPickSpecTarget, serverFormValue(&st, serverFieldSpecPath)
		case serverFieldRoutes:
			target, value = serverPickRoutesTarget, serverFormValue(&st, serverFieldRoutes)
		default:

			return m, nil
		}

		return m.openFilePicker(OpenFilePickerMsg{
			Target: target, Root: "/", RootLabel: "./",
			Start: formPickStartDir(value), Exts: []string{jsonExt},
		})

	default:
		_, _ = m.serverDlg.Update(msg)

		return m, nil
	}
}

// startServer snapshots the form values, flips the dialog into its
// in-flight line, and returns the start Cmd (the engine leg is
// injectable via serveStartFn — the liveConnect/dialConnect pattern).
func (m *RootModel) startServer() (tea.Model, tea.Cmd) {
	st := m.serverDlg.State()
	port := strings.TrimSpace(serverFormValue(&st, serverFieldPort))
	if port == "" {
		port = serverDefaultPort
	}
	header := strings.TrimSpace(serverFormValue(&st, serverFieldHeader))
	if header == "" {
		header = serverDefaultHeader
	}
	spec := strings.TrimSpace(serverFormValue(&st, serverFieldSpecPath))
	routes := strings.TrimSpace(serverFormValue(&st, serverFieldRoutes))

	start := m.serveStartFn
	if start == nil {
		if m.app == nil {
			st.InFlight = false
			st.Error = errNoAppWired
			m.serverDlg.SetState(st)

			return m, nil
		}
		start = m.app.ServeStart
	}

	st.InFlight = true
	st.Error = ""
	st.Progress = "starting :" + port + " (" + header + ")"
	m.serverDlg.SetState(st)
	m.serverStarting = true
	m.debug.logf("server start requested port=%s header=%s", port, header)

	return m, func() tea.Msg {
		return serverStartResultMsg{
			port: port, header: header, spec: spec, routes: routes,
			// The §G "Routes file" field IS the routes-file leg:
			// app.ServeStart resolves it through ResolveRoutes, and a
			// broken explicit pick fails the start with the path named.
			// An empty field means no routes — nothing is fabricated.
			err: start(port, header, spec, "", routes),
		}
	}
}

// applyServerStartResult closes the run: failure keeps the modal open
// with the error line and the form editable again (no auto-retry),
// success stamps the running truth, takes the first stats snapshot, and
// closes the modal.
func (m *RootModel) applyServerStartResult(msg serverStartResultMsg) (tea.Model, tea.Cmd) {
	m.serverStarting = false

	if msg.err != nil {
		if m.serverDlg != nil {
			st := m.serverDlg.State()
			st.InFlight = false
			st.Progress = ""
			st.Error = msg.err.Error()
			m.serverDlg.SetState(st)
		}
		m.debug.logf("server start failed: %v", msg.err)

		return m, nil
	}

	m.serverStartAt = m.now()
	m.serverPort, m.serverHeader = msg.port, msg.header
	m.serverError = ""
	m.rememberLastServer(msg)
	if snap := m.serveStatsSnapshot(); snap != nil {
		m.serverSnap = snap
	}
	m.serverDlg = nil
	m.debug.logf("server started port=%s header=%s", msg.port, msg.header)

	return m, nil
}

// pickServerFormField commits a §G [f] pick into the named field,
// preserving every other field value and the dialog focus (the
// pickFormTxFile preservation idiom without ApplySettings: the §G
// paths are per-start overrides, not global settings).
func (m *RootModel) pickServerFormField(fieldKey, path string) (tea.Model, tea.Cmd) {
	if m.serverDlg == nil {
		return m, nil
	}
	st := m.serverDlg.State()
	if f := st.Field(fieldKey); f != nil {
		f.Value = path
	}
	m.serverDlg.SetState(st)
	m.pushToast(filepath.Base(path)+" selected", widgets.ToastSuccess)

	return m, nil
}

// focusedFormFieldKey reads the key of the dialog's focused field
// ("" when the index is out of range).
func focusedFormFieldKey(st *pages.ConnectFormState, focus int) string {
	if focus >= 0 && focus < len(st.Fields) {
		return st.Fields[focus].Key
	}

	return ""
}

// formPickStartDir is the picker's open directory for a typed path
// value: its directory when set, else the working directory.
func formPickStartDir(value string) string {
	if value == "" {
		return "."
	}

	return filepath.Dir(value)
}

// serverFormValue reads one field's canonical value ("" when absent).
func serverFormValue(st *pages.ConnectFormState, key string) string {
	if f := st.Field(key); f != nil {
		return f.Value
	}

	return ""
}
