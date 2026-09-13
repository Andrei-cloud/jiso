// settingsapply.go is the §L write side (SCR-512). ApplySettings
// validates every patched value PER FIELD and mutates the App-visible
// config (config.Config, mutex-guarded) for live-safe keys only: an
// invalid field is reported per-field and skipped, valid siblings
// still apply (all-or-nothing per field, independent across fields).
// Live-apply scope is deliberately conservative — only the session
// config object changes, so already-dialed connections/workers keep
// their values and the NEXT connect/send/serve reads the new ones
// (the page footer states "applies to next operation"). output is not
// live-safe (no session holder): it lands in a session override and
// persists on save. SaveSettings re-validates the whole patch and
// writes ONLY those keys to the user config file via userconfig.Save;
// any invalid key aborts the write with nothing touched.
package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moov-io/iso8583"

	"jiso/internal/cli/userconfig"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// ApplySettings validates patch (canonical §L key -> textual value)
// and applies every valid value to the session config. The returned
// map holds one validation error per invalid field; nil/empty means
// the whole patch applied.
func (a *App) ApplySettings(_ context.Context, patch map[string]string) map[string]string {
	errs := map[string]string{}
	canon := map[string]string{}

	for key, value := range patch {
		spec, ok := settingsSpecs[key]
		if !ok {
			errs[key] = fmt.Sprintf("unknown setting: %s", key)

			continue
		}
		c, err := validateSetting(spec, value)
		if err != "" {
			errs[key] = err

			continue
		}
		canon[key] = c
	}

	// The connect/total pair is checked on the patch result; the
	// patched side carries the violation and is dropped, so unrelated
	// valid fields still apply (per-field atomicity).
	if err := a.checkTimeoutPair(canon); err != "" {
		key := errFieldTimeoutPair(canon)
		errs[key] = err
		delete(canon, key)
	}

	// A patch carrying both spec and tx-file must apply the spec FIRST:
	// the live collection is rebuilt from the CURRENT spec, so the old
	// random map order could compose the new file against the old spec.
	for _, key := range specFirstKeys(canon) {
		if err := a.applySetting(settingsSpecs[key], canon[key]); err != "" {
			errs[key] = err
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errs
}

// SaveSettings persists patch (only the changed keys) to the user
// config file (XDG path or $JISO_CONFIG — tests point JISO_CONFIG at
// t.TempDir()). Validation is all-or-nothing for the save: one invalid
// key returns an error naming it and writes nothing. The resolved
// write path is returned for the success line.
func (a *App) SaveSettings(_ context.Context, patch map[string]string) (string, error) {
	if len(patch) == 0 {
		return "", &ConfigError{Err: fmt.Errorf("no settings to save")}
	}

	ucPatch := make(map[string]string, len(patch))
	for key, value := range patch {
		spec, ok := settingsSpecs[key]
		if !ok {
			return "", &ConfigError{Path: key, Err: fmt.Errorf("unknown setting: %s", key)}
		}
		c, bad := validateSetting(spec, value)
		if bad != "" {
			return "", &ConfigError{Path: key, Err: fmt.Errorf("%s: %s", key, bad)}
		}
		ucPatch[spec.ucKey] = settingFileValue(spec, c)
	}

	path, err := userconfig.Path()
	if err != nil {
		return "", err
	}
	if err := userconfig.Save(path, ucPatch); err != nil {
		return "", fmt.Errorf("failed to write user config %s: %w", path, err)
	}

	return path, nil
}

// checkTimeoutPair rejects a patch whose resulting connect timeout
// exceeds the resulting total connect timeout (the config.Validate
// invariant); "" means the pair is consistent.
func (a *App) checkTimeoutPair(canon map[string]string) string {
	if _, ok := canon[SettingConnectTimeout]; !ok {
		if _, ok := canon[SettingTotalConnectTimeout]; !ok {
			return ""
		}
	}
	connect, err := a.resultingDuration(SettingConnectTimeout, canon)
	if err != nil {
		return ""
	}
	total, err := a.resultingDuration(SettingTotalConnectTimeout, canon)
	if err != nil {
		return ""
	}
	if total < connect {
		return fmt.Sprintf("total connect timeout (%s) must be greater than or equal to connect timeout (%s)",
			formatDuration(total), formatDuration(connect))
	}

	return ""
}

// errFieldTimeoutPair attributes the pair violation to the patched
// side (prefer connect-timeout, the usual offender).
func errFieldTimeoutPair(canon map[string]string) string {
	if _, ok := canon[SettingConnectTimeout]; ok {
		return SettingConnectTimeout
	}

	return SettingTotalConnectTimeout
}

// resultingDuration resolves one timeout across patch-over-current.
func (a *App) resultingDuration(key string, canon map[string]string) (time.Duration, error) {
	if text, ok := canon[key]; ok {
		return time.ParseDuration(text)
	}
	switch key {
	case SettingConnectTimeout:

		return a.cfg.GetConnectTimeout(), nil
	case SettingTotalConnectTimeout:

		return a.cfg.GetTotalConnectTimeout(), nil
	}

	return 0, fmt.Errorf("not a timeout: %s", key)
}

// applySetting mutates the session config for one validated value.
// specFirstKeys orders the apply loop so the spec key (if patched) runs
// before everything else; the tx-file collection is built from the live
// spec, so spec-before-txfile is a correctness requirement.
func specFirstKeys(canon map[string]string) []string {
	keys := make([]string, 0, len(canon))
	for _, key := range []string{SettingSpec, SettingTxFile} {
		if _, ok := canon[key]; ok {
			keys = append(keys, key)
		}
	}
	for key := range canon {
		if key != SettingSpec && key != SettingTxFile {
			keys = append(keys, key)
		}
	}

	return keys
}

func (a *App) applySetting(spec settingSpec, value string) string {
	switch spec.kind {
	case kindInt:
		n, _ := strconv.Atoi(value)
		a.cfg.SetReconnectAttempts(n)
	case kindDuration:
		d, _ := time.ParseDuration(value)

		switch spec.key {
		case SettingConnectTimeout:
			a.cfg.SetConnectTimeout(d)
		case SettingTotalConnectTimeout:
			a.cfg.SetTotalConnectTimeout(d)
		case SettingResponseTimeout:
			a.cfg.SetResponseTimeout(d)
		default:
			a.cfg.SetListenTimeout(d)
		}
	case kindBool:
		b, _ := parseBoolText(value)
		a.cfg.SetHex(b)
	case kindStation:
		a.cfg.SetVisaStationID(value)
	case kindTLS:
		if err := a.cfg.SetTLSConfigPath(value); err != nil {
			return err.Error()
		}
	case kindSpec:
		return a.applySpecSetting(value)
	case kindTxFile:
		return a.applyTxFileSetting(value)
	case kindDB:
		a.cfg.SetDbPath(value)
	case kindOutput:
		a.settingsMu.Lock()
		if a.settingsOverrides == nil {
			a.settingsOverrides = map[string]string{}
		}
		a.settingsOverrides[SettingOutput] = value
		a.settingsMu.Unlock()
	}

	return ""
}

// validateSetting checks one textual value against its kind, returning
// the canonical persisted form or a field error message (config.
// ValidateValues bounds mirrored). Empty values are legal for the
// path-like kinds (clearing a spec/tx/db/station path).
func validateSetting(spec settingSpec, value string) (canonical, fieldErr string) {
	switch spec.kind {
	case kindInt:
		return validateIntSetting(value)
	case kindDuration:
		return validateDurationSetting(spec.key, value)
	case kindBool:
		b, ok := parseBoolText(value)
		if !ok {
			return "", fmt.Sprintf("invalid boolean %q (use on/off)", value)
		}

		return boolText(b), ""
	case kindStation:
		v := strings.TrimSpace(value)
		if v == "" {
			return "", ""
		}
		if !isHexDigits(v) || len(v) != 6 {
			return "", fmt.Sprintf("visa station ID must be 6 hex or decimal digits, got %q", v)
		}

		return v, ""
	case kindTLS:
		v := strings.TrimSpace(value)
		if v == "" {
			return "", "tls-config requires a path (clearing is not supported)"
		}
		if _, err := os.Stat(v); err != nil {
			return "", fmt.Sprintf("TLS config file does not exist: %s", v)
		}

		return v, ""
	case kindSpec, "txfile":
		v := strings.TrimSpace(value)
		if v != "" {
			if _, err := os.Stat(v); err != nil {
				kind := map[string]string{kindSpec: "spec file", kindTxFile: "transaction file"}[spec.kind]

				return "", fmt.Sprintf("%s does not exist: %s", kind, v)
			}
		}

		return v, ""
	case kindDB:
		v := strings.TrimSpace(value)
		if v != "" {
			if _, err := os.Stat(v); err != nil && !existsParentDir(v) {
				return "", fmt.Sprintf("database parent directory does not exist: %s", dirOf(v))
			}
		}

		return v, ""
	case kindOutput:
		return validateOutputValue(value)
	}

	return "", "unknown setting"
}

// durationBound mirrors config.validateValues upper bounds per timeout.
func durationBound(key string, d time.Duration) string {
	switch key {
	case SettingConnectTimeout:
		if d > 5*time.Minute {
			return fmt.Sprintf("connect timeout too high, got %s (max 5m)", formatDuration(d))
		}
	case SettingTotalConnectTimeout, SettingResponseTimeout:
		if d > 10*time.Minute {
			name := strings.TrimSuffix(key, "-timeout")

			return fmt.Sprintf("%s timeout too high, got %s (max 10m)", name, formatDuration(d))
		}
	}

	return ""
}

// settingFileValue maps a canonical §L value to its user config file
// spelling (output text/json persists as the json bool "true"/"false").
func settingFileValue(spec settingSpec, canon string) string {
	if spec.kind == kindOutput {
		// The §L value is "text"/"json"; the file stores a bool under a
		// different key, so this is the one place the two vocabularies meet.
		return strconv.FormatBool(canon == outputValueJSON)
	}

	return canon
}

// parseBoolText accepts the CLI-104 truthy/falsy spellings.
// parseBoolText reads the boolean an operator typed: value is what it means, ok
// says whether the text was a boolean at all, which is the distinction between
// "false" and "not a boolean" that the settings grid reports differently.
func parseBoolText(s string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	}

	return false, false
}

// outputText maps the json bool to the §L output row spelling.
func outputText(json bool) string {
	if json {
		return "json"
	}

	return "text"
}

// isHexDigits reports whether s is only [0-9a-fA-F].
func isHexDigits(s string) bool {
	for _, r := range s {
		if !isHexDigit(r) {
			return false
		}
	}

	return s != ""
}

// isHexDigit reports whether r is one of the sixteen hex digits.
func isHexDigit(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

// existsParentDir reports whether the parent directory of path exists.
func existsParentDir(path string) bool {
	info, err := os.Stat(dirOf(path))

	return err == nil && info.IsDir()
}

// dirOf returns path's parent directory ( "." for bare names).
func dirOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i > 0 {
		return path[:i]
	}

	return "."
}

// applySpecSetting loads (or defaults) the spec for value and installs it on the
// config, the live service, and the transaction collection.
func (a *App) applySpecSetting(value string) string {
	var sp *iso8583.MessageSpec
	if value == "" {
		sp = utils.GetDefaultSpec()
	} else {
		loaded, err := utils.CreateSpecFromFile(value)
		if err != nil {
			return fmt.Sprintf("spec file rejected: %v", err)
		}
		sp = loaded
	}
	a.cfg.SetSpec(value)
	a.svc.SetSpec(sp)
	a.Transactions().SetSpec(sp)

	return ""
}

// applyTxFileSetting builds a transaction collection from value and swaps it in,
// leaving config and the live collection untouched when the file is invalid.
func (a *App) applyTxFileSetting(value string) string {
	coll, err := transactions.NewTransactionCollection(value, a.svc.GetSpec())
	if err != nil {
		return fmt.Sprintf("transaction file rejected: %v", err)
	}
	a.cfg.SetFile(value)
	a.swapTransactions(coll)

	return ""
}

// validateIntSetting bounds the reconnect-attempts setting to 0..100.
func validateIntSetting(value string) (canonical, fieldErr string) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Sprintf("must be an integer, got %q", value)
	}
	if n < 0 || n > 100 {
		return "", fmt.Sprintf("must be 0..100, got %d", n)
	}

	return strconv.Itoa(n), ""
}

// validateDurationSetting parses a timeout and applies its positivity and
// per-key upper bounds.
func validateDurationSetting(key, value string) (canonical, fieldErr string) {
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Sprintf("invalid duration %q", value)
	}
	if d <= 0 {
		return "", fmt.Sprintf("%s must be positive, got %s", key, formatDuration(d))
	}
	if bad := durationBound(key, d); bad != "" {
		return "", bad
	}

	return value, ""
}

// validateOutputValue canonicalizes the output setting to "text" or "json".
func validateOutputValue(value string) (canonical, fieldErr string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case outputValueText:
		return outputValueText, ""
	case outputValueJSON:
		return outputValueJSON, ""
	}
	if b, ok := parseBoolText(value); ok {
		return outputText(b), ""
	}

	return "", fmt.Sprintf("output must be text or json, got %q", value)
}
