// sessions_state.go holds the §I state contract and the page→router
// messages. Root owns the DB truth: it derives every display string off
// the UI thread and pushes SessionsState via SetState; the page never
// imports internal/app, never reads the clock, and never writes.
package pages

// SessionsPageID is the router id of the §I sessions DB page:
// hotkey 6, footer label "sessions".
const SessionsPageID = "sessions"

// SessionRow is one SESSIONS list row: ShortID is the shortened session
// id ("9f3c…a1"), When the root-derived relative stamp
// ("today 12:01" / "yest 17:30" / "09-07 17:30").
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
// (packed hex + parsed fields). Describe carries the field tree as
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
// Note is the root-stamped status line (never a crash dump); DetailWait
// marks an in-flight load — detail panes show loading, never false empty.
type SessionsState struct {
	DBPath     string
	Note       string
	Sessions   []SessionRow
	SelectedID string
	Stats      []SummaryKV
	History    []TxHistoryRow
	Review     *TxReviewState
	DetailWait bool
}

// SessionsSelectMsg asks the router to load stats + tx history for the
// session under the list cursor (Enter in the SESSIONS pane; in the
// narrow fallback it also drills in — that Enter meaning is kept).
type SessionsSelectMsg struct{ ID string }

// SessionsFocusMsg reports the list cursor moved onto a different
// session: root loads its stats + tx history into the detail panes as a
// live preview — the same data Enter loads, minus the narrow drill.
type SessionsFocusMsg struct{ ID string }

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
