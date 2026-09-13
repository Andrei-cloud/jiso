// settings_state.go holds the §L state contract (wireframe §L) and the
// page→router messages (SCR-512). Root owns every App touch: it loads
// app.SettingsView snapshots off the UI thread, runs ApplySettings on
// each committed field, and drives SaveSettings; the page receives
// SettingsState — display rows with source/marker/error strings already
// resolved — and never imports internal/app or reads the clock. The
// page owns presentation state only: the row cursor, the in-field edit
// buffer, and the drafts of committed-but-not-yet-refreshed values
// (an invalid commit keeps showing the attempted text beside its
// inline error until the snapshot catches up or the field is re-edited).
package pages

// SettingsPageID is the router id of the §L settings page (registry
// entry after the 8 hotkey slots, like §F/§K; the palette
// ":settings" jump resolves it).
const SettingsPageID = "settings"

// SettingsRow is one §L grid row: the display Value ("5s", "on",
// "./specs/visa.json"), its provenance Source ("flag|env|config|
// default|session" text, root-decorated), Marker ("●" hex toggle,
// "✓"/"✗" tls existence, ""), and Error — the per-field validation
// text rendered inline red under the row until the next commit.
type SettingsRow struct {
	Key    string
	Label  string
	Value  string
	Source string
	Marker string
	Error  string
	// Pickable marks a file-path field: `f` opens the shared file
	// picker (TUI-406b); root resolves the directory and predicate.
	Pickable bool
}

// SettingsSaveOverlay is the [w] save-confirm content: the XDG target
// path and the changed-keys diff ("key: old -> new" lines, "(unset)"
// for absent file values). Root opens it only when a change exists;
// the page never computes the diff.
type SettingsSaveOverlay struct {
	Path string
	Diff []string
}

// SettingsState is the immutable §L snapshot root pushes. SavedLine/
// SavedOK carry the last save result line; Note carries root-stamped
// page-level text (no changes, malformed config file, ...).
type SettingsState struct {
	ConfigPath string
	Note       string
	Rows       []SettingsRow
	Save       *SettingsSaveOverlay
	SavedLine  string
	SavedOK    bool
}

// SettingsCommitMsg is Enter inside a field: root validates and
// live-applies the single-field patch (ApplySettings) off the UI
// thread; validation errors return as the row's Error.
type SettingsCommitMsg struct {
	Key   string
	Value string
}

// SettingsSaveMsg is w outside the overlay: root opens the save
// confirm overlay when a change exists (a no-change w only sets the
// page Note).
type SettingsSaveMsg struct{}

// SettingsSaveConfirmMsg is w inside the save overlay: persist exactly
// the diff the overlay shows (changed keys only).
type SettingsSaveConfirmMsg struct{}

// SettingsSaveCancelMsg is Esc inside the save overlay: nothing is
// written and the live edits stay session-only (Esc "discards" the
// save, per the wireframe footer).
type SettingsSaveCancelMsg struct{}

// SettingsRefreshMsg asks root to reload the snapshot (r).
type SettingsRefreshMsg struct{}

// SettingsPickFileMsg is f on a Pickable row: root opens the shared
// widgets.FilePicker overlay (TUI-406b); the chosen path returns as a
// SettingsCommitMsg through the same validation seam.
type SettingsPickFileMsg struct{ Key string }

// SettingsPopMsg asks the router to pop the §L page (Esc outside
// editing/overlay; the overlay and the edit buffer own Esc earlier).
type SettingsPopMsg struct{}
