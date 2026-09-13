// sessions_state.go holds the §I state contract (wireframe §I) and the
// page→router messages. Root owns the DB truth: it queries the app read
// façade off the UI thread (tea.Cmd), derives every display string with
// the injectable clock (relative times, stats math, RC distribution,
// review sections), and pushes SessionsState via SetState — the page
// never imports internal/app and never reads the clock (the SCR-501
// data-flow contract). The page never opens the database and never
// writes: it is a browser over the snapshots root hands it.
package pages

// SessionsPageID is the router id of the §I sessions DB page:
// hotkey 6, footer label "sessions".
const SessionsPageID = "sessions"

// SessionRow is one SESSIONS list row: ShortID is the shortened session
// id (Theme.ShortID: "9f3c…a1", "9f3c~a1" under the ASCII set), When the
// root-derived
// relative stamp ("today 12:01" / "yest 17:30" / "09-07 17:30").
type SessionRow struct {
	ID      string
	ShortID string
	When    string
}

// TxHistoryRow is one TX HISTORY row. Status is one of the canonical
// tokens below (the page maps each to a ✓/✗ symbol+word cell, never
// colour alone); Time and Latency are root-derived display strings.
type TxHistoryRow struct {
	ID      int64
	Time    string
	Name    string
	MTI     string
	RC      string
	Status  string
	Latency string
}

// Tx history Status canonical values.
const (
	TxStatusOK      = "ok"
	TxStatusFail    = "fail"
	TxStatusTimeout = "timeout"
)

// TxReviewMessage is one reconstructed message section of the tx review
// (§C-style: packed hex + parsed fields; the same capability as
// `jiso db tx <id>`). Describe carries the reconstructed field tree as
// stored; RawFallback/ParseError mark the degraded reconstruction.
type TxReviewMessage struct {
	HEX         string
	Describe    string
	RawFallback bool
	ParseError  string
}

// TxReviewState is the reconstructed review of one stored transaction
// (root-built from the app façade's retrospective). nil anywhere in the
// pipeline means "nothing to review": the page never invents one.
type TxReviewState struct {
	TxID     int64
	Headline []string // "Purchase · 0210 RC 96 · 2ms · ✗" lines
	Request  *TxReviewMessage
	Response *TxReviewMessage
}

// SessionsState is the immutable §I snapshot root pushes into the page.
// DBPath is the configured path shown in the title ("" = not
// configured); Note is the root-stamped status line — the typed façade
// error rendered as empty-state text (missing DB etc.), never a crash.
// Stats holds the selected session's overview lines (total/ok/fail/avg/
// RC dist); History the selected session's tx rows (newest first).
type SessionsState struct {
	DBPath     string
	Note       string
	Sessions   []SessionRow
	SelectedID string
	Stats      []SummaryKV
	History    []TxHistoryRow
	Review     *TxReviewState
}

// SessionsSelectMsg asks the router to load stats + tx history for the
// session under the list cursor (Enter in the SESSIONS pane).
type SessionsSelectMsg struct{ ID string }

// SessionsReviewMsg asks the router to reconstruct one stored tx for the
// review overlay (Enter/[t] on a TX HISTORY row).
type SessionsReviewMsg struct{ TxID int64 }

// SessionsRefreshMsg asks the router to re-query the DB (r). The page
// shows stale rows until the next SetState; nothing is cleared
// optimistically.
type SessionsRefreshMsg struct{}

// SessionsPopMsg asks the router to pop the §I page (Esc; no-op at
// depth 1). The review overlay and the narrow drill-down own Esc
// earlier, inside the page.
type SessionsPopMsg struct{}
