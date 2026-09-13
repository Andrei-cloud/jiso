// root_connect_form.go owns the §E form DATA (SCR-505): the root builds
// the initial form from config (or the session-prefill of the last
// successful connect), recomputes every field's Enabled flag from the form
// values on every relevant change, and stamps the TLS note after its own
// filesystem check — the pages dialog renders these flags verbatim and
// never touches config, the filesystem, or internal/app.
package tui

import (
	"os"
	"strings"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// connectFieldSpec is the static shape of one §E row (wireframe §E order).
type connectFieldSpec struct {
	key      string
	label    string
	kind     pages.FieldKind
	options  []string
	def      string // default option/value when nothing prefills it
	note     string
	noteKind pages.NoteKind
}

func connectFieldSpecs() []connectFieldSpec {
	return []connectFieldSpec{
		{
			key: pages.ConnectFieldMode, label: "Mode", kind: pages.FieldRadio,
			options: pages.ConnectModeOptions, def: "Caller",
		},
		{
			key: pages.ConnectFieldIP, label: "IP", kind: pages.FieldText,
			note: "(caller only)", noteKind: pages.NoteInfo,
		},
		{
			key: pages.ConnectFieldPort, label: "Port", kind: pages.FieldText,
			def: defaultListenPort,
		},
		{
			key: pages.ConnectFieldHeader, label: "Header", kind: pages.FieldPicker,
			options: pages.ConnectHeaderOptions, def: "binary2",
		},
		{
			key: pages.ConnectFieldStation, label: "Station ID", kind: pages.FieldText,
			note: "(visa header only)", noteKind: pages.NoteInfo,
		},
		{
			key: pages.ConnectFieldUnsolicited, label: "Unsolicited", kind: pages.FieldRadio,
			options: pages.ConnectYesNoOptions, def: "No",
		},
		{key: pages.ConnectFieldTLS, label: "TLS", kind: pages.FieldText},
	}
}

// defaultListenPort is the bind-port fallback when neither the session
// prefill nor the config carries one (REPL connect prompt default).
const defaultListenPort = "9999"

// buildConnectForm assembles the §E snapshot: every value prefills from the
// last successful connect (session-only, set by applyConnectResult) and
// falls back to the config; radio Selected indices are re-derived from the
// resulting values so prefill and rendering agree.
func (m *RootModel) buildConnectForm() pages.ConnectFormState {
	cfg := m.configOrNil()
	prev := m.connectSession
	last := m.ensureLastConn()

	st := pages.ConnectFormState{}
	for _, sp := range connectFieldSpecs() {
		f := pages.FormField{
			Key: sp.key, Label: sp.label, Kind: sp.kind,
			Options: append([]string(nil), sp.options...),
			Note:    sp.note, NoteKind: sp.noteKind,
		}
		f.Value = connectPrefill(prev, sp, cfg, last)
		if f.Kind == pages.FieldRadio || f.Kind == pages.FieldPicker {
			f.Selected = connectOptionIndex(f.Options, f.Value, sp.def)
			f.Value = f.Options[f.Selected]
		}
		st.Fields = append(st.Fields, f)
	}

	return st
}

// connectPrefill resolves one field's initial value: session value, then
// the config-derived fallback, then the last-used connection details
// (state dir, UAT), then the spec default.
func connectPrefill(prev *pages.ConnectFormState, sp connectFieldSpec, cfg *config.Config, last *app.LastConnection) string {
	if prev != nil {
		if f := prev.Field(sp.key); f != nil && f.Value != "" {
			return f.Value
		}
	}
	if v, ok := connectConfigValue(sp.key, cfg); ok {
		return v
	}
	if v, ok := lastConnValue(sp.key, last); ok {
		return v
	}

	return sp.def
}

// lastConnValue maps a field key to the remembered last-successful
// connect value ("" + false when nothing was remembered).
func lastConnValue(key string, last *app.LastConnection) (string, bool) {
	if last == nil {
		return "", false
	}
	switch key {
	case pages.ConnectFieldIP:
		if last.Host != "" {
			return last.Host, true
		}
	case pages.ConnectFieldPort:
		if last.Port != "" {
			return last.Port, true
		}
	case pages.ConnectFieldHeader:
		if last.Header != "" {
			return last.Header, true
		}
	case pages.ConnectFieldTLS:
		if last.TLS != "" {
			return last.TLS, true
		}
	}

	return "", false
}

// ensureLastConn reads the state-dir last-used connection once per
// session (a malformed file is treated as absent; the debug log keeps
// it visible in JISO_DEBUG runs).
func (m *RootModel) ensureLastConn() *app.LastConnection {
	if m.lastConnLoaded {
		return m.lastConn
	}
	m.lastConnLoaded = true
	if lc, err := app.LoadLastConnection(); err == nil && lc != nil {
		m.lastConn = lc
	}

	return m.lastConn
}

// connectConfigValue maps a field key to its config prefill (target and
// bind both derive from host:port; the header keeps the raw configured
// string so an exotic-but-valid value still round-trips).
func connectConfigValue(key string, cfg *config.Config) (string, bool) {
	if cfg == nil {
		return "", false
	}
	switch key {
	case pages.ConnectFieldIP:
		if h := cfg.GetHost(); h != "" {
			return h, true
		}
	case pages.ConnectFieldPort:
		if p := cfg.GetPort(); p != "" {
			return p, true
		}
	case pages.ConnectFieldHeader:
		if h := cfg.GetHeader(); h != "" {
			return h, true
		}
	case pages.ConnectFieldStation:
		if s := cfg.GetVisaStationID(); s != "" {
			return s, true
		}
	case pages.ConnectFieldTLS:
		if p := cfg.GetTLSConfigPath(); p != "" {
			return p, true
		}
	}

	return "", false
}

// connectOptionIndex finds the option matching want (case-insensitive),
// defaulting to the option named def (or 0).
func connectOptionIndex(options []string, want, def string) int {
	for i, o := range options {
		if strings.EqualFold(o, strings.TrimSpace(want)) {
			return i
		}
	}
	for i, o := range options {
		if strings.EqualFold(o, def) {
			return i
		}
	}

	return 0
}

// applyConnectRules recomputes every field's Enabled flag from the form
// DATA (wireframe §E: "Fields enable/disable by mode & header type"):
// target belongs to caller mode, bind port to listener mode, and the
// station ID to the visa header only — the same case-insensitive visa
// identification internal/utils/length.go SelectLength uses. Everything
// else stays enabled.
func applyConnectRules(st *pages.ConnectFormState) {
	mode := connectFormValue(st, pages.ConnectFieldMode)
	header := connectFormValue(st, pages.ConnectFieldHeader)

	for i := range st.Fields {
		f := &st.Fields[i]
		switch f.Key {
		case pages.ConnectFieldIP:
			f.Enabled = mode != pages.ConnectModeListener
			if f.Enabled {
				f.Note, f.NoteKind = "(caller only)", pages.NoteInfo
			} else {
				f.Note, f.NoteKind = "(n/a: bind all)", pages.NoteInfo
			}
		case pages.ConnectFieldPort:
			f.Enabled = true
		case pages.ConnectFieldStation:
			f.Enabled = pages.ConnectHeaderIsVisa(header)
		default:
			f.Enabled = true
		}
	}

	// The §E dialog names its Enter verb after the mode (connect vs
	// listen); reused forms (SERVER/STRESS/BGSEND) keep their own label.
	if st.Title == "" {
		if mode == pages.ConnectModeListener {
			st.EnterLabel = "listen"
		} else {
			st.EnterLabel = "connect"
		}
	}
}

// stampTLSNote is the root's filesystem check (the page never sees fs):
// "loaded" with the pass kind when the path exists, "missing" with the
// fail kind otherwise, no note when the field is empty (TLS off).
func (m *RootModel) stampTLSNote(st *pages.ConnectFormState) {
	f := st.Field(pages.ConnectFieldTLS)
	if f == nil {
		return
	}
	path := strings.TrimSpace(f.Value)
	switch {
	case path == "":
		f.Note, f.NoteKind = "", pages.NoteNone
	case connectFileExists(path):
		f.Note, f.NoteKind = "loaded", pages.NotePass
	default:
		f.Note, f.NoteKind = "missing", pages.NoteFail
	}
}

// connectFileExists is the dialog's only filesystem touch (root side). It
// stays a var so tests could pin it without creating files.
var connectFileExists = func(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && !fi.IsDir()
}

// syncConnect pushes the root-side data into the open dialog after every
// Update (placement mirrors syncDashboard/syncSend): Enabled flags and the
// TLS note are re-derived from the current values, so a mode or header
// change takes effect before the next View. While in flight the form is
// frozen (the progress line is the only truth).
func (m *RootModel) syncConnect() {
	if m.dlg != nil {
		syncConnectDialog(m.dlg, m.stampTLSNote)
	}
	// The wizard's step-0 form is the same dialog machinery and needs the
	// same re-derivation (proposal 04 §B).
	if m.wizard != nil {
		syncConnectDialog(m.wizard.ConnectForm(), m.stampTLSNote)
	}
}

// syncConnectDialog re-derives one open connect form (Enabled flags, mode
// note and Enter verb) unless it is mid-attempt.
func syncConnectDialog(d *pages.ConnectDialog, stamp func(*pages.ConnectFormState)) {
	if d == nil {
		return
	}
	st := d.State()
	if st.InFlight {
		return
	}
	applyConnectRules(&st)
	stamp(&st)
	d.SetState(st)
}

// connectFormValue reads one field's canonical value ("" when absent).
func connectFormValue(st *pages.ConnectFormState, key string) string {
	if f := st.Field(key); f != nil {
		return f.Value
	}

	return ""
}

// configOrNil returns the app config or nil (no-panic prefill).
func (m *RootModel) configOrNil() *config.Config {
	if m.app == nil {
		return nil
	}

	return m.app.Config()
}

// rememberLastConn stamps the successful connect form into the state-dir
// last-used file (best-effort; failures only show under JISO_DEBUG) and
// refreshes the in-session copy so a reopened form prefills immediately.
func (m *RootModel) rememberLastConn(st *pages.ConnectFormState) {
	lc := app.LastConnection{}
	str := func(key string) string {
		if f := st.Field(key); f != nil {
			return f.Value
		}

		return ""
	}
	lc.Host = str(pages.ConnectFieldIP)
	lc.Port = str(pages.ConnectFieldPort)
	lc.Header = str(pages.ConnectFieldHeader)
	lc.TLS = str(pages.ConnectFieldTLS)
	if lc.Host == "" || lc.Port == "" {
		return
	}
	m.lastConn, m.lastConnLoaded = &lc, true
	if err := app.SaveLastConnection(lc); err != nil {
		m.debug.logf("last-connection save: %v", err)
	}
}
