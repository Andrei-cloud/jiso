// settingsview_test.go pins the §L façade contract (SCR-512) with a
// fixture user config file in t.TempDir() (JISO_CONFIG override — the
// real user file is never touched): CurrentSettings sources resolve
// env > config > default with a "session" fallback; ApplySettings is
// per-field atomic (invalid field reports an error, valid siblings
// still mutate App-visible config); SaveSettings writes ONLY the
// changed keys through the same loader and aborts entirely on one
// invalid value.
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jiso/internal/cli/userconfig"
)

// settingsFixtureApp builds an App over the shared singleton with a
// fixture user config file (body may be empty = no file yet).
func settingsFixtureApp(t *testing.T, body string) (*App, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	t.Setenv(userconfig.EnvConfigVar, path)

	cfg := testConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	return a, path
}

func rowByView(view SettingsView, key string) (SettingsRow, bool) {
	for _, r := range view.Rows {
		if r.Key == key {
			return r, true
		}
	}

	return SettingsRow{}, false
}

func TestCurrentSettingsSources(t *testing.T) {
	a, path := settingsFixtureApp(t, "host: h\nreconnect_attempts: 7\nconnect_timeout: 8s\nhex: true\n")
	// Mirror what the CLI's PersistentPreRunE does with the resolved
	// file values before the App starts (root.go cfg mapping).
	a.cfg.SetReconnectAttempts(7)
	a.cfg.SetConnectTimeout(8 * time.Second)
	a.cfg.SetHex(true)

	view, err := a.CurrentSettings()
	if err != nil {
		t.Fatalf("CurrentSettings: %v", err)
	}
	if view.ConfigPath != path {
		t.Fatalf("ConfigPath = %q, want %q", view.ConfigPath, path)
	}

	for key, want := range map[string]string{
		"reconnect-attempts": SourceConfig,
		"connect-timeout":    SourceConfig,
		"hex":                SourceConfig,
		"response-timeout":   SourceDefault,
		"listen-timeout":     SourceDefault,
	} {
		row, ok := rowByView(view, key)
		if !ok {
			t.Fatalf("row %s missing", key)
		}
		if row.Source != want {
			t.Errorf("%s source = %s, want %s", key, row.Source, want)
		}
	}

	if row, _ := rowByView(view, "connect-timeout"); row.Value != "8s" || !row.FileSet || row.FileValue != "8s" {
		t.Errorf("connect-timeout row = %+v", row)
	}
	if row, _ := rowByView(view, "hex"); row.Marker != "\u25cf" {
		t.Errorf("hex marker = %q, want bullet", row.Marker)
	}
	if _, ok := rowByView(view, "host"); ok {
		t.Error("host is not a §L key")
	}
}

func TestCurrentSettingsEnvSource(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	t.Setenv("JISO_RESPONSE_TIMEOUT", "9s")
	a.cfg.SetResponseTimeout(9 * time.Second)

	view, err := a.CurrentSettings()
	if err != nil {
		t.Fatalf("CurrentSettings: %v", err)
	}
	row, _ := rowByView(view, "response-timeout")
	if row.Source != SourceEnv {
		t.Fatalf("source = %s, want env (row %+v)", row.Source, row)
	}
}

func TestCurrentSettingsSessionSourceAndTLSMarker(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	missing := filepath.Join(t.TempDir(), "absent.json")
	if err := a.cfg.SetTLSConfigPath(""); err != nil {
		t.Fatalf("SetTLSConfigPath: %v", err)
	}
	_ = missing

	// Point the path at a missing file via the raw field: the ✓/✗
	// marker data must reflect existence, not the setter's success.
	view, err := a.CurrentSettings()
	if err != nil {
		t.Fatalf("CurrentSettings: %v", err)
	}
	if row, _ := rowByView(view, "tls-config"); row.Value != "" || row.Marker != "" {
		t.Errorf("unset tls row = %+v, want empty value no marker", row)
	}

	// A live edit (config value absent from every layer) reads as
	// "session".
	a.cfg.SetReconnectAttempts(42)
	view, _ = a.CurrentSettings()
	if row, _ := rowByView(view, "reconnect-attempts"); row.Source != SourceSession {
		t.Errorf("live-edited value source = %s, want session", row.Source)
	}
}

func TestCurrentSettingsTLSMarkerExists(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	tlsPath := filepath.Join(t.TempDir(), "tls.json")
	if err := os.WriteFile(tlsPath, []byte(`{"enabled":false}`), 0o600); err != nil {
		t.Fatalf("write tls: %v", err)
	}
	if err := a.cfg.SetTLSConfigPath(tlsPath); err != nil {
		t.Fatalf("SetTLSConfigPath: %v", err)
	}

	view, _ := a.CurrentSettings()
	row, _ := rowByView(view, "tls-config")
	if row.Marker != "\u2713" {
		t.Fatalf("tls marker = %q, want check", row.Marker)
	}

	// A path that exists in config but not on disk must marker ✗ via
	// a direct mutation of the session path (bypassing the loading
	// setter, mirroring a file deleted mid-session).
	a.cfg.SetTLSConfig(nil)
	if errs := a.ApplySettings(t.Context(), map[string]string{}); errs != nil {
		t.Fatalf("empty patch: %v", errs)
	}
	missing := filepath.Join(t.TempDir(), "gone.json")
	if err := os.WriteFile(missing, []byte(`{"enabled":false}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := a.cfg.SetTLSConfigPath(missing); err != nil {
		t.Fatalf("SetTLSConfigPath: %v", err)
	}
	if err := os.Remove(missing); err != nil {
		t.Fatalf("remove: %v", err)
	}
	view, _ = a.CurrentSettings()
	if row, _ := rowByView(view, "tls-config"); row.Marker != "\u2717" {
		t.Fatalf("tls marker after delete = %q, want cross", row.Marker)
	}
}

func TestApplySettingsValidPatchVisibleViaConfig(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")

	errs := a.ApplySettings(t.Context(), map[string]string{
		"reconnect-attempts":    "5",
		"connect-timeout":       "7s",
		"total-connect-timeout": "20s",
		"response-timeout":      "3s",
		"listen-timeout":        "90s",
		"hex":                   "on",
		"visa-station-id":       "00ABCD",
	})
	if errs != nil {
		t.Fatalf("valid patch errors: %v", errs)
	}

	cfg := a.Config()
	if cfg.GetReconnectAttempts() != 5 || cfg.GetConnectTimeout() != 7*time.Second ||
		cfg.GetTotalConnectTimeout() != 20*time.Second || cfg.GetResponseTimeout() != 3*time.Second ||
		cfg.GetListenTimeout() != 90*time.Second || !cfg.GetHex() || cfg.GetVisaStationID() != "00ABCD" {
		t.Fatalf("config not updated: %+v", cfg)
	}
}

func TestApplySettingsInvalidDurationOthersApplied(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")

	errs := a.ApplySettings(t.Context(), map[string]string{
		"connect-timeout":    "bogus",
		"response-timeout":   "6s",
		"reconnect-attempts": "9",
	})
	if len(errs) != 1 || errs["connect-timeout"] == "" {
		t.Fatalf("errors = %v, want only connect-timeout", errs)
	}
	if a.Config().GetResponseTimeout() != 6*time.Second || a.Config().GetReconnectAttempts() != 9 {
		t.Fatal("valid siblings must still apply")
	}
	if a.Config().GetConnectTimeout() != 5*time.Second {
		t.Fatal("invalid field must not apply")
	}
}

func TestApplySettingsTimeoutPairViolation(t *testing.T) {
	a, _ := settingsFixtureApp(t, "") // total stays 10s

	errs := a.ApplySettings(t.Context(), map[string]string{
		"connect-timeout":  "20s",
		"response-timeout": "4s",
	})
	if len(errs) != 1 || errs["connect-timeout"] == "" {
		t.Fatalf("errors = %v, want connect-timeout pair violation", errs)
	}
	if a.Config().GetConnectTimeout() != 5*time.Second {
		t.Fatal("rejected connect timeout must not apply")
	}
	if a.Config().GetResponseTimeout() != 4*time.Second {
		t.Fatal("independent field must still apply")
	}
}

func TestApplySettingsTLSMissingAndStationFormat(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	missing := filepath.Join(t.TempDir(), "absent.json")

	errs := a.ApplySettings(t.Context(), map[string]string{
		"tls-config":      missing,
		"visa-station-id": "12",
		"unknown-key":     "x",
	})
	if len(errs) != 3 {
		t.Fatalf("errors = %v, want three per-field messages", errs)
	}
	if !strings.Contains(errs["tls-config"], "does not exist") {
		t.Errorf("tls error = %q", errs["tls-config"])
	}
	if !strings.Contains(errs["visa-station-id"], "6 hex") {
		t.Errorf("station error = %q", errs["visa-station-id"])
	}
	if !strings.Contains(errs["unknown-key"], "unknown setting") {
		t.Errorf("unknown error = %q", errs["unknown-key"])
	}
	if a.Config().GetVisaStationID() != "" {
		t.Error("invalid station id must not apply")
	}
}

func TestApplySettingsOutputOverrideSessionSource(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")

	if errs := a.ApplySettings(t.Context(), map[string]string{"output": "json"}); errs != nil {
		t.Fatalf("output patch: %v", errs)
	}
	view, _ := a.CurrentSettings()
	row, _ := rowByView(view, "output")
	if row.Value != "json" || row.Source != SourceSession || row.LiveSafe {
		t.Fatalf("output row = %+v, want session-sourced non-live json", row)
	}
	if errs := a.ApplySettings(t.Context(), map[string]string{"output": "bogus"}); len(errs) != 1 {
		t.Fatalf("bogus output errors = %v", errs)
	}
}

func TestApplySettingsPathChecks(t *testing.T) {
	a, _ := settingsFixtureApp(t, "")
	dir := t.TempDir()

	if errs := a.ApplySettings(t.Context(), map[string]string{
		"spec":    filepath.Join(dir, "absent.json"),
		"tx-file": filepath.Join(dir, "absent.json"),
		"db":      filepath.Join(dir, "nope", "sub.db"),
	}); len(errs) != 3 {
		t.Fatalf("errors = %v, want three file-existence errors", errs)
	}
	// A db path whose parent exists is legal (the file may be new).
	if errs := a.ApplySettings(t.Context(), map[string]string{
		"db": filepath.Join(dir, "fresh.db"),
	}); errs != nil {
		t.Fatalf("fresh db path: %v", errs)
	}
	if a.Config().GetDbPath() == "" {
		t.Error("db path must apply")
	}
}

func TestSaveSettingsWritesOnlyChangedKeys(t *testing.T) {
	body := "host: h\nspec: ./specs/visa.json\nquiet: true\n"
	a, path := settingsFixtureApp(t, body)

	savedPath, err := a.SaveSettings(t.Context(), map[string]string{
		"connect-timeout":    "8s",
		"hex":                "on",
		"output":             "json",
		"reconnect-attempts": "4",
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if savedPath != path {
		t.Fatalf("saved path = %q, want %q", savedPath, path)
	}

	f, _, err := userconfig.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if f.ConnectTimeout == nil || *f.ConnectTimeout != "8s" {
		t.Errorf("connect_timeout = %v", f.ConnectTimeout)
	}
	if f.Hex == nil || !*f.Hex {
		t.Errorf("hex = %v", f.Hex)
	}
	if f.JSON == nil || !*f.JSON {
		t.Errorf("json = %v (output json must persist as json: true)", f.JSON)
	}
	if f.ReconnectAttempts == nil || *f.ReconnectAttempts != 4 {
		t.Errorf("reconnect_attempts = %v", f.ReconnectAttempts)
	}
	if f.Host == nil || *f.Host != "h" || f.Spec == nil || *f.Spec != "./specs/visa.json" || f.Quiet == nil || !*f.Quiet {
		t.Fatalf("untouched keys lost: %+v", f)
	}
	if f.DB != nil || f.ResponseTimeout != nil {
		t.Errorf("unset defaults must not be written: %+v", f)
	}
}

func TestSaveSettingsInvalidAbortsWholeWrite(t *testing.T) {
	body := "host: h\n"
	a, path := settingsFixtureApp(t, body)
	before, _ := os.ReadFile(path)

	_, err := a.SaveSettings(t.Context(), map[string]string{
		"connect-timeout":  "8s",
		"response-timeout": "nonsense",
	})
	if err == nil || !strings.Contains(err.Error(), "response-timeout") {
		t.Fatalf("err = %v, want per-key validation failure", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("file must be untouched:\n%s", after)
	}
}

func TestSaveSettingsEmptyPatchAndMissingFileCreate(t *testing.T) {
	a, path := settingsFixtureApp(t, "")
	if _, err := a.SaveSettings(t.Context(), nil); err == nil {
		t.Fatal("empty patch must error")
	}
	if _, err := a.SaveSettings(t.Context(), map[string]string{"db": "./sessions.db"}); err != nil {
		t.Fatalf("save to missing file: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("save must create %s: %v", path, err)
	}
	f, _, err := userconfig.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if f.DB == nil || *f.DB != "./sessions.db" {
		t.Fatalf("db = %v", f.DB)
	}
}
