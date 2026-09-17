package pages

import (
	"fmt"
	"time"

	"jiso/internal/tui/palette"
	"jiso/internal/tui/theme"
)

// dashIf substitutes the unknown marker for empty display strings: em dash
// normally, "-" under theme.ASCII (ascii goldens must stay 7-bit).
func dashIf(th *theme.Theme, s string) string {
	if s != "" {
		return s
	}
	if th != nil && th.ASCII {
		return ASCIIDash
	}

	return Dash
}

// DashboardPageID is the router slot name of the dashboard: hotkey 1,
// footer label "dash".
const DashboardPageID = "dashboard"

// Dash is the unknown-value marker (design contract: unknown fields render
// as an em dash — ASCII "-" under theme.ASCII — never as zero values).
const (
	Dash      = "—"
	ASCIIDash = "-"
)

// Shared list-page table titles and hint descriptions (several pages
// render the same columns; goconst).
const (
	colTransaction = "TRANSACTION"
	colStatus      = "STATUS"
	hintScroll     = "scroll"
	hintPreview    = "preview"
)

// ConnStatus is the connection-card state, mapped by root from the event
// bus's latest ConnectionEvent (or the app snapshot). Zero value is
// ConnUnknown, which renders as the dash, not as a fake OFFLINE.
type ConnStatus int

const (
	// ConnUnknown is the state before the root reports any connection truth, which the frame renders as its empty state.
	ConnUnknown ConnStatus = iota
	// ConnOnline renders "✓ ONLINE" in status.ok.
	ConnOnline
	// ConnReconnecting renders "⚠ RECONNECTING" in status.warn.
	ConnReconnecting
	// ConnOffline renders plain muted "OFFLINE" (not an error).
	ConnOffline
	// ConnFailed renders "✗ FAILED" in status.error.
	ConnFailed
)

// Label is the word half of the symbol+word pair (never color alone).
func (s ConnStatus) Label() string {
	switch s {
	case ConnOnline:
		return "ONLINE"
	case ConnReconnecting:
		return "RECONNECTING"
	case ConnOffline:
		return "OFFLINE"
	case ConnFailed:
		return "FAILED"
	default:
		return ""
	}
}

// ConnectionCard is the §A connection state snapshot. Root fills it from
// internal/app + the event bridge; every time-derived value is pre-derived
// (Uptime is a Duration, not a timestamp) so the page never reads the clock.
type ConnectionCard struct {
	Status ConnStatus
	Role   string // "caller"/"listener"; "" renders as the dash
	Target string // host:port
	Header string // ISO header type (binary2, ascii4, ...)
	TLS    string // "off", "TLS", or "mTLS"; "" renders as the dash
	// Uptime is how long the current connection has been up; nil = unknown.
	Uptime *time.Duration
	// Retries is the configured reconnect-attempt budget; nil = unknown.
	Retries *int
}

// SessionCard is the §A session snapshot: the counters come from the
// async App.SessionStats read folded root-side; Known is false until the
// first snapshot and every unknown number renders as the dash.
type SessionCard struct {
	ID     string
	TxSent int
	OK     int
	Fail   int
	// AvgResponse is the session's average processing time; meaningful
	// only when Known.
	AvgResponse time.Duration
	DBPath      string
	Known       bool
}

// LastSendCard is the §A LAST SEND snapshot: the completed §D exchange
// pre-derived root-side (Time is the clock stamp formatted at completion;
// the MTIs are the field-0 cells of the exchange rows). The pointer is nil
// until a send has run, which also gates the "View last send" row.
type LastSendCard struct {
	Time    string // "15:04:05" completion time, root-stamped
	TxName  string
	ReqMTI  string
	RespMTI string
	RC      string
	RCNote  string // "APPROVED"/"DECLINED"; "" renders the code alone
	// Elapsed is the frozen completion duration (never live-derived).
	Elapsed     time.Duration
	Validated   bool
	Correlation bool
}

// LastStressCard is the §A LAST STRESS snapshot: the most recent
// COMPLETED stress worker, pre-derived root-side from the App's
// StressSummary at completion (fetched once, never per tick). Nil gates
// the "Stress summary" quick-action row.
type LastStressCard struct {
	Time string // "15:04:05" completion time, root-stamped
	ID   string
	// Done is true for a clean "done" stop.
	Done    bool
	OkPct   string // "100.0%"
	Workers string // "1 worker" / "4 workers"
	TPS     string // "82.3 tps"
	P99     string // "0.3ms"
}

// ServerCard is the §A MOCK SERVER card snapshot: the running truth plus
// the §G stats, all root-derived. StatsKnown is false before the first
// snapshot; the card then renders dash placeholders.
type ServerCard struct {
	Running    bool
	Port       string
	Header     string
	Uptime     time.Duration
	StatsKnown bool
	Stats      StatsCard
}

// DashboardState is the immutable snapshot root pushes into the dashboard
// (root owns the App access). Actions is the palette action registry the
// quick-actions list renders.
type DashboardState struct {
	Conn       ConnectionCard
	Server     ServerCard
	Session    *SessionCard
	LastSend   *LastSendCard
	LastStress *LastStressCard
	// ServerLog is the root-stamped raw mock-server ring copy (oldest
	// first); the SERVER LOG card compacts it and renders the tail.
	ServerLog []string
	Actions   []palette.Action

	// HasConnection marks a live connection: root derives it from the
	// latest ConnectionEvent / app snapshot; the page never touches
	// internal/app. It gates the disconnect row's visibility — the key
	// itself stays bound (a root-side sane no-op without a connection).
	HasConnection bool
}

// FormatUptime renders d as HH:MM:SS (hours may exceed 24); nil becomes ""
// so the caller substitutes the dash.
func FormatUptime(d *time.Duration) string {
	if d == nil {
		return ""
	}
	t := *d
	if t < 0 {
		t = 0
	}
	h := int(t.Hours())
	m := int(t.Minutes()) % 60
	s := int(t.Seconds()) % 60

	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// formatInt renders a counter; nil becomes "".
func formatInt(n *int) string {
	if n == nil {
		return ""
	}

	return fmt.Sprintf("%d", *n)
}
