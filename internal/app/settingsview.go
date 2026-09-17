// settingsview.go is the §L settings façade: CurrentSettings
// snapshots the live session config with per-key SOURCES resolved
// through the CLI-104 layers. The CLI cannot be imported here, so the
// flag layer is inferred: a value matching $JISO_* is "env", one
// matching the user config file is "config", one matching the built-in
// default is "default", and anything else (a CLI flag on the running
// session, or a live §L edit) is "session". The loader is the SAME
// internal/cli/userconfig the cobra root uses, so the displayed file
// values are exactly what the next CLI invocation would read.
// ApplySettings/SaveSettings live in settingsapply.go.
package app

import (
	"os"
	"strconv"
	"time"

	"jiso/internal/cli/userconfig"
)

// Settings source labels (row.Source).
const (
	SourceEnv     = "env"
	SourceConfig  = "config"
	SourceDefault = "default"
	SourceSession = "session"
)

// Settings keys (canonical, §L order is settingsKeyOrder).
const (
	SettingReconnectAttempts   = "reconnect-attempts"
	SettingConnectTimeout      = "connect-timeout"
	SettingTotalConnectTimeout = "total-connect-timeout"
	SettingResponseTimeout     = "response-timeout"
	SettingListenTimeout       = "listen-timeout"
	SettingHex                 = "hex"
	SettingVisaStationID       = "visa-station-id"
	SettingTLSConfig           = "tls-config"
	SettingSpec                = "spec"
	SettingTxFile              = "tx-file"
	SettingDB                  = "db"
	SettingOutput              = "output"
)

// The validation kinds a §L setting can declare, and the two values the output
// setting takes. They are here rather than at each use because the settings table
// below and the apply and validate switches in settingsapply.go must agree
// name-for-name: a kind the table spells that no switch handles is a setting that
// silently accepts anything, and nothing fails at build time to say so.
// ucKeyOutput is the userconfig yaml key for the output setting, which is a bool
// in the file and a "text"/"json" value in the session -- two spellings of one
// setting, so the key gets a name rather than sharing the value's literal.
const ucKeyOutput = "json"

const (
	kindInt      = "int"
	kindDuration = "duration"
	kindBool     = "bool"
	kindStation  = "station"
	kindTLS      = "tls"
	kindSpec     = "spec"
	kindTxFile   = "txfile"
	kindDB       = "db"
	kindOutput   = "output"
)

// The output setting's values. "json" is also the userconfig yaml key for the same
// setting, which is exactly why the two are not interchangeable literals: the key
// names where a value is stored, the value says how messages are printed.
const (
	outputValueText = "text"
	outputValueJSON = "json"
)

// settingSpec maps one §L key to its userconfig/env names, default
// text, validation kind, and live-safety. liveSafe=false keys never
// touch config.Config (output has no session-config holder).
type settingSpec struct {
	key      string
	ucKey    string // userconfig.File yaml key
	env      string // $JISO_* name
	def      string // default textual value
	kind     string // int|duration|bool|station|tls|spec|txfile|db|output
	liveSafe bool
}

// settingsKeyOrder is the §L row order.
var settingsKeyOrder = []string{
	SettingReconnectAttempts, SettingConnectTimeout, SettingTotalConnectTimeout,
	SettingResponseTimeout, SettingListenTimeout, SettingHex, SettingVisaStationID,
	SettingTLSConfig, SettingSpec, SettingTxFile, SettingDB, SettingOutput,
}

var settingsSpecs = map[string]settingSpec{
	SettingReconnectAttempts:   {SettingReconnectAttempts, "reconnect_attempts", "JISO_RECONNECT_ATTEMPTS", "3", kindInt, true},
	SettingConnectTimeout:      {SettingConnectTimeout, "connect_timeout", "JISO_CONNECT_TIMEOUT", "5s", kindDuration, true},
	SettingTotalConnectTimeout: {SettingTotalConnectTimeout, "total_connect_timeout", "JISO_TOTAL_CONNECT_TIMEOUT", "10s", kindDuration, true},
	SettingResponseTimeout:     {SettingResponseTimeout, "response_timeout", "JISO_RESPONSE_TIMEOUT", "5s", kindDuration, true},
	SettingListenTimeout:       {SettingListenTimeout, "listen_timeout", "JISO_LISTEN_TIMEOUT", "5m", kindDuration, true},
	SettingHex:                 {SettingHex, "hex", "JISO_HEX", "false", kindBool, true},
	SettingVisaStationID:       {SettingVisaStationID, "visa_station_id", "JISO_VISA_STATION_ID", "", kindStation, true},
	SettingTLSConfig:           {SettingTLSConfig, "tls_config", "JISO_TLS_CONFIG", "", kindTLS, true},
	SettingSpec:                {SettingSpec, "spec", "JISO_SPEC", "", kindSpec, true},
	SettingTxFile:              {SettingTxFile, "file", "JISO_FILE", "", kindTxFile, true},
	SettingDB:                  {SettingDB, "db", "JISO_DB", "", kindDB, true},
	SettingOutput:              {SettingOutput, ucKeyOutput, "JISO_JSON", outputValueText, kindOutput, false},
}

// SettingsRow is one §L grid row: the effective session Value with its
// Source, the raw user-config FileValue (FileSet = key present in the
// file, the save-overlay diff's old side), and Marker — "●" for the
// hex toggle, "✓"/"✗" for a set tls-config path, "" otherwise.
type SettingsRow struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	FileValue string `json:"file_value,omitempty"`
	FileSet   bool   `json:"file_set,omitempty"`
	Marker    string `json:"marker,omitempty"`
	LiveSafe  bool   `json:"live_safe"`
}

// SettingsView is the §L snapshot: the user config file path (save
// target, "~/.config/jiso"-style per XDG) and the rows in
// order. A malformed user config file degrades the config layer to
// "no file" and reports the load error separately (root renders it as
// the page Note; the session rows still render).
type SettingsView struct {
	ConfigPath string        `json:"config_path"`
	Rows       []SettingsRow `json:"rows"`
}

// CurrentSettings snapshots the App-visible config with sources. It
// performs read-only file checks (tls-config ✓/✗ marker) and the same
// userconfig.Load the CLI's PersistentPreRunE runs.
func (a *App) CurrentSettings() (SettingsView, error) {
	uc, path, loadErr := userconfig.Load()

	view := SettingsView{ConfigPath: path}
	for _, key := range settingsKeyOrder {
		spec := settingsSpecs[key]
		row := SettingsRow{Key: key, Value: a.settingValue(spec), LiveSafe: spec.liveSafe}
		var fileVal *string
		if uc != nil {
			fileVal = userconfigValue(uc, spec.ucKey)
		}
		if fileVal != nil {
			row.FileValue, row.FileSet = *fileVal, true
		}
		row.Source = settingSource(row.Value, spec, os.Getenv(spec.env), fileVal)
		switch spec.kind {
		case kindBool:
			row.Marker = "\u25cf"
		case kindTLS:
			if row.Value != "" {
				if _, err := os.Stat(row.Value); err == nil {
					row.Marker = "\u2713"
				} else {
					row.Marker = "\u2717"
				}
			}
		}
		view.Rows = append(view.Rows, row)
	}

	return view, loadErr
}

// settingValue formats the effective session value for the grid.
func (a *App) settingValue(spec settingSpec) string {
	cfg := a.cfg
	switch spec.kind {
	case kindInt:
		return strconv.Itoa(cfg.GetReconnectAttempts())
	case kindDuration:
		var d time.Duration
		switch spec.key {
		case SettingConnectTimeout:
			d = cfg.GetConnectTimeout()
		case SettingTotalConnectTimeout:
			d = cfg.GetTotalConnectTimeout()
		case SettingResponseTimeout:
			d = cfg.GetResponseTimeout()
		default:
			d = cfg.GetListenTimeout()
		}

		return formatDuration(d)
	case kindBool:
		return boolText(cfg.GetHex())
	case kindStation:
		return cfg.GetVisaStationID()
	case kindTLS:
		return cfg.GetTLSConfigPath()
	case kindSpec:
		return cfg.GetSpec()
	case kindTxFile:
		return cfg.GetFile()
	case kindDB:
		return cfg.GetDbPath()
	case kindOutput:
		a.settingsMu.Lock()
		defer a.settingsMu.Unlock()
		if v, ok := a.settingsOverrides[SettingOutput]; ok {
			return v
		}

		return "text"
	}

	return ""
}

// settingSource resolves one row's provenance: env > config > default,
// with a "session" fallback naming values a flag or a live edit set
// (the flag layer is unobservable from internal/app).
func settingSource(effective string, spec settingSpec, envValue string, fileVal *string) string {
	if envValue != "" && spec.matches(effective, envValue) {
		return SourceEnv
	}
	if fileVal != nil && spec.matches(effective, *fileVal) {
		return SourceConfig
	}
	if spec.matches(effective, spec.def) {
		return SourceDefault
	}

	return SourceSession
}

// matches reports whether a textual layer value equals the effective
// value semantically (durations/bools/ints parse-compared).
func (spec settingSpec) matches(effective, layer string) bool {
	switch spec.kind {
	case kindDuration:
		d, err := time.ParseDuration(layer)

		return err == nil && formatDuration(d) == effective
	case kindBool:
		b, ok := parseBoolText(layer)

		return ok && boolText(b) == effective
	case kindOutput:
		b, ok := parseBoolText(layer)
		if !ok {
			return layer == effective
		}

		return outputText(b) == effective
	case kindInt:
		n, err := strconv.Atoi(layer)

		return err == nil && strconv.Itoa(n) == effective
	default:
		return layer == effective
	}
}

// userconfigValue extracts one §L key's pointer from the loaded file.
func userconfigValue(uc *userconfig.File, ucKey string) *string {
	var p *string

	switch ucKey {
	case kindSpec:
		p = uc.Spec
	case "file":
		p = uc.File
	case kindDB:
		p = uc.DB
	case "tls_config":
		p = uc.TLSConfig
	case "visa_station_id":
		p = uc.VisaStationID
	case "connect_timeout":
		p = uc.ConnectTimeout
	case "total_connect_timeout":
		p = uc.TotalConnectTimeout
	case "response_timeout":
		p = uc.ResponseTimeout
	case "listen_timeout":
		p = uc.ListenTimeout
	case "reconnect_attempts":
		if uc.ReconnectAttempts == nil {
			return nil
		}
		s := strconv.Itoa(*uc.ReconnectAttempts)

		return &s
	case "hex":
		if uc.Hex == nil {
			return nil
		}
		s := boolText(*uc.Hex)

		return &s
	case "json":
		if uc.JSON == nil {
			return nil
		}
		s := outputText(*uc.JSON)

		return &s
	}

	return p
}

// formatDuration renders "5s"/"10s"/"5m"/"1h30m0s"-style
// text (whole minutes win over seconds).
func formatDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0s"
	case d >= time.Minute && d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	case d >= time.Second && d%time.Second == 0:
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	default:
		return d.String()
	}
}

// boolText renders §L on/off spellings as canonical true/false text.
func boolText(b bool) string {
	return strconv.FormatBool(b)
}
