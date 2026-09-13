// send_state.go holds the §D state contract (wireframe
// .opencode/plans/02-tui-wireframes.md §D) and the page→router messages.
// Root owns the live operation: it walks the Connect▸Send▸Receive▸Parse▸
// Validate stages, stamps every time-derived value with its injectable
// clock, computes the correlation notes and the RC badge, and pushes
// SendState snapshots via SetState — the page never touches internal/app,
// never reads the clock, and never ticks itself (SCR-501 data-flow
// contract, extended to the first live screen by SCR-504).
package pages

import "time"

// SendPageID is the router id of the §D live exchange view. The wire-compat
// slot id "send" belongs to the §B transactions page (TransactionsPageID);
// this page is pushed on top of it by TxSendMsg, never a jump target.
const SendPageID = "send-exchange"

// SendStageCount is the number of stages in the segmented indicator
// (Connect ▸ Send ▸ Receive ▸ Parse ▸ Validate).
const SendStageCount = 5

// SendStageNames are the stage labels in walk order; the indicator joins
// them with the ▸ (ascii ">") separator.
var SendStageNames = [SendStageCount]string{"Connect", "Send", "Receive", "Parse", "Validate"}

// NoteKind selects how ExchangeRow.Note renders: pass/fail get the theme
// status symbol (never color alone), info rows stay plain text.
type NoteKind uint8

const (
	// NoteNone is the absence of an annotation, the state a field row is in before a send.
	NoteNone NoteKind = iota
	// NotePass renders " (label ✓)" with the ok symbol.
	NotePass
	// NoteFail renders " (label ✗)" with the error symbol.
	NoteFail
	// NoteInfo renders " (label)" as muted text (e.g. "auth code").
	NoteInfo
)

// ExchangeRow is one request/response pane row as pre-derived display
// strings. Display holds the human value (PAN already masked by root),
// Hex the same row's value hex-encoded: `h` flips the pane between the two
// columns, so the toggle is pure display over identical rows.
type ExchangeRow struct {
	Num     string // field number as shown ("0", "11", …); "" for structural Describe lines
	Text    string // verbatim utils.Describe line; "" = legacy num/value row
	Display string // formatted value
	Hex     string // lowercase hex of the value bytes
	Note    string // annotation label; "" = none
	NoteKind
}

// SendState is the immutable §D snapshot root pushes into the page.
//
// Stage/StageOK encode the segmented-indicator truth: Stage is the index
// of the last resolved stage, StageOK holds one entry per resolved stage
// (walk order), so a stage that never fired (connect failed → the rest
// never run) stays absent and renders dim, never ✗. Elapsed is stamped by
// root on every push (live) and frozen at completion; Budget comes from
// the same config response-timeout source the CLI send --wait honors.
// HexOn is the page-owned `h` toggle: SetState preserves it across pushes
// (root never sets it).
type SendState struct {
	TxID   string // repository key for the Enter re-send
	TxName string // display name
	Target string // host:port

	Request  []ExchangeRow
	Response []ExchangeRow
	// RequestHex/ResponseHex are the standard hexdump lines (offset,
	// 16 byte pairs, ASCII gutter) of the packed messages; the h toggle
	// switches whole panes from the Describe rows to these (UAT).
	RequestHex  []string
	ResponseHex []string

	Stage    int    // 0..4: last resolved stage index
	StageOK  []bool // per resolved stage, walk order
	Elapsed  time.Duration
	Budget   time.Duration
	Attempt  int // send attempt number (1; the flow never auto-retries)
	Done     bool
	TimedOut bool

	RC            string // response field 39, trimmed; "" = absent
	RCLabel       string // "APPROVED"/"DECLINED"; "" = code-only badge
	RCok          bool   // badge renders on the ok style (RC 00)
	Validated     bool   // root's validate stage verdict
	CorrelationOK bool   // request/response STAN correlated

	HexOn bool
}

// sendNavKeys are the §D page-local triggers, documented for the hints.
const (
	sendKeyResend = "enter"
	sendKeyHex    = "h"
	sendKeyPop    = "esc"
)

// SendPopMsg asks the router to pop the §D page (Esc). Root owns the
// stack; at depth 1 the pop is a no-op (InspectorPopMsg pattern).
type SendPopMsg struct{}
