// root_settings_state.go derives the §L SettingsState snapshot (the
// SCR-501 data-flow contract): the page receives display data only —
// wireframe labels, on/off spellings for the hex toggle, the ✓/✗
// existence marker, per-field validation errors, and the save-overlay
// diff — all resolved here from the cached app.SettingsView.
// syncSettings runs in the Update wrapper, so every folded message is
// reflected in the next View.
package tui

import (
	"sort"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
)

// settingsLabels are the §L wireframe row labels in grid order.
var settingsLabels = map[string]string{
	app.SettingReconnectAttempts:   "reconnect-attempts",
	app.SettingConnectTimeout:      "connect-timeout",
	app.SettingTotalConnectTimeout: "total-connect-timeout",
	app.SettingResponseTimeout:     "response-timeout",
	app.SettingListenTimeout:       "listen-timeout",
	app.SettingHex:                 "hex output",
	app.SettingVisaStationID:       "visa-station-id",
	app.SettingTLSConfig:           "tls-config",
	app.SettingSpec:                "spec",
	app.SettingTxFile:              "tx file",
	app.SettingDB:                  "db",
	app.SettingOutput:              "output",
}

// syncSettings pushes the current snapshot into the §L page.
func (m *RootModel) syncSettings() {
	if m.settings == nil {
		return
	}
	m.settings.SetState(m.settingsState())
}

// settingsState builds the immutable snapshot the page renders.
func (m *RootModel) settingsState() pages.SettingsState {
	st := pages.SettingsState{
		Note:      m.settingsNote,
		SavedLine: m.settingsSavedLine,
		SavedOK:   m.settingsSavedOK,
	}
	if m.settingsView == nil {
		return st
	}
	st.ConfigPath = m.settingsView.ConfigPath

	for _, row := range m.settingsView.Rows {
		label, ok := settingsLabels[row.Key]
		if !ok {
			continue
		}
		st.Rows = append(st.Rows, pages.SettingsRow{
			Key:      row.Key,
			Label:    label,
			Value:    settingsDisplay(row.Key, row.Value),
			Source:   row.Source,
			Marker:   row.Marker,
			Error:    m.settingsErrs[row.Key],
			Pickable: settingsPathKeys[row.Key],
		})
	}

	if m.settingsSaveOpen {
		st.Save = m.settingsSaveOverlay()
	}

	return st
}

// settingsSaveOverlay builds the [w] confirm content: the XDG target
// and the changed-keys diff "label: old -> new" (file value, or
// "(unset)" when the key is absent from the user config), sorted by
// §L grid order for deterministic lines.
func (m *RootModel) settingsSaveOverlay() *pages.SettingsSaveOverlay {
	ov := &pages.SettingsSaveOverlay{Path: m.settingsView.ConfigPath}

	order := make([]string, 0, len(m.settingsChanged))
	for key := range m.settingsChanged {
		order = append(order, key)
	}
	sort.Slice(order, func(i, j int) bool {
		return settingsRowOrdinal(order[i]) < settingsRowOrdinal(order[j])
	})

	for _, key := range order {
		old := "(unset)"
		for _, row := range m.settingsView.Rows {
			if row.Key != key {
				continue
			}
			if row.FileSet {
				old = settingsDisplay(key, row.FileValue)
			}

			break
		}
		ov.Diff = append(ov.Diff, settingsLabels[key]+": "+old+" -> "+
			settingsDisplay(key, m.settingsChanged[key]))
	}

	return ov
}

// settingsRowOrdinal orders diff lines by the §L grid position.
func settingsRowOrdinal(key string) int {
	for i, k := range []string{
		app.SettingReconnectAttempts, app.SettingConnectTimeout, app.SettingTotalConnectTimeout,
		app.SettingResponseTimeout, app.SettingListenTimeout, app.SettingHex,
		app.SettingVisaStationID, app.SettingTLSConfig, app.SettingSpec,
		app.SettingTxFile, app.SettingDB, app.SettingOutput,
	} {
		if k == key {
			return i
		}
	}

	return len(settingsLabels)
}

// settingsDisplay maps a canonical textual value (or a raw committed
// spelling) to its §L display text (the hex toggle shows on/off;
// everything else verbatim).
func settingsDisplay(key, value string) string {
	if key == app.SettingHex {
		return map[string]string{"true": "on", "false": "off", "on": "on", "off": "off"}[value]
	}

	return value
}
