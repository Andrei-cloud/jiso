// Package palette implements the command palette (TUI-405): an action
// registry, a hand-rolled fuzzy matcher, and the input+list overlay widget
// the root model opens on ":" / Ctrl+P.
//
// Contract (mirrors internal/tui/widgets):
//   - Pure: Update consumes a tea.Msg and returns state plus a tea.Cmd;
//     no I/O, no os.Exit.
//   - Theme-driven: rendering uses only internal/tui/theme token styles.
//   - Leaf-level: never imports internal/tui/frame, internal/cli (the
//     frontend may consume ONLY internal/app — enforced by
//     imports_guard_test.go), internal/command, or cobra. Palette input
//     tokenisation therefore calls github.com/kballard/go-shellquote
//     directly: the same tokenizer internal/cli/lexer wraps, imported
//     without crossing the package boundary.
package palette

import (
	tea "charm.land/bubbletea/v2"
)

// GoToPageMsg asks the router to jump to the page slot named ID
// (replace-stack semantics, same as the 1..8 hotkeys). An action's Run
// returns it; the palette delivers it through a tea.Cmd.
type GoToPageMsg struct {
	ID string
}

// PushPageMsg asks the router to push the page named ID on top of the
// current stack (same as "?"). Kept distinct from GoToPageMsg so "show
// help" can preserve context while "go to help" replaces the stack.
type PushPageMsg struct {
	ID string
}

// OpenConnectMsg asks the router to open the §E connect dialog as an
// overlay on the current page (SCR-505): the page stack is untouched, Esc
// returns to the same page. The palette action and the dashboard quick
// action both emit it, so every entry point lands on one router handler.
type OpenConnectMsg struct{}

// DisconnectMsg asks the router to close the live connection (TUI-514,
// closes REGRESSION-1): the palette action and the §A quick key both emit
// it, so every entry point lands on one router handler. The router owns
// the semantics — with a connection it disconnects (a §N3 confirm first
// while workers are active or the serve engine runs), without one it is a
// sane no-op with an info toast.
type DisconnectMsg struct{}

// OpenSendWizardMsg asks the router to open the send wizard (proposal 04
// §B): one modal that walks spec ▸ tx file ▸ template, prefixed with a
// connect step when no connection is live ("send selected from the menu
// with connection settings undefined opens connection settings first").
// The palette action, the dashboard quick action and the global "s" key
// all emit it, so every entry point lands on one router handler.
type OpenSendWizardMsg struct{}

// Shared action keywords (several send actions carry them; goconst).
const (
	kwPrevious = "previous"
	kwHistory  = "history"
)

// registerSendActions registers the palette's send family: the wizard
// (proposal 04 §B, always available — offline it opens with the connect
// step first; the §A quick action hides it until a connection exists),
// the last-send view (toasts when nothing ran), and the send history
// overlay (UAT round 5).
func registerSendActions(r *Registry) {
	r.Register(Action{
		ID:       "send-wizard",
		Title:    "Send transaction",
		Keywords: []string{"send", "wizard", "compose", "template"},
		Hints:    []string{"s"},
		Run: func([]string) tea.Msg {
			return OpenSendWizardMsg{}
		},
	})
	r.Register(Action{
		ID:       "last-send",
		Title:    "View last send",
		Keywords: []string{"last", kwPrevious, "result", "again", "resend"},
		Run: func([]string) tea.Msg {
			return LastSendViewMsg{}
		},
	})
	r.Register(Action{
		ID:       "send-history",
		Title:    "Send history",
		Keywords: []string{kwHistory, kwPrevious, "sends", "log"},
		Run: func([]string) tea.Msg {
			return SendHistoryMsg{}
		},
	})
}

// DirectSendMsg asks the router for the one-keystroke send (UAT round
// 5): with a live connection and a loaded spec + tx file it starts the
// send immediately — from the dashboard the operator stays put and the
// LAST SEND tile carries the outcome — falling back to the send wizard
// only when something is unresolved. The dashboard quick action and the
// "s" key emit it; ":send" in the palette behaves the same, since the
// wizard opens itself exactly when the session config is incomplete.
type DirectSendMsg struct{}

// LastSendViewMsg asks the router to show the last completed send result
// (§D snapshot) without starting a new op; Enter there resends (UAT:
// returning to a previously sent transaction).
type LastSendViewMsg struct{}

// SendHistoryMsg asks the router to open the send-history overlay: the
// scrollable list of this session's completed sends; Enter on a row
// freezes §D on it, whose h toggle switches detail ↔ hex (UAT round 5:
// the send menu should keep a history, not just the single LAST SEND).
type SendHistoryMsg struct{}

// LastStressSummaryMsg asks the router to reopen the stress summary
// overlay for the last completed stress run (proposal 05 §3 LAST STRESS
// card): the §A quick-action row and the LAST STRESS "enter summary"
// affordance both emit it, and the router lands on the existing §H
// overlay path. The router toasts when no stress run completed.
type LastStressSummaryMsg struct{}

// QuitRequestMsg asks the router to quit; the router confirms first
// (UAT: exit confirmation before the application quits). Ctrl+C stays
// the immediate escape hatch.
type QuitRequestMsg struct{}

// Action is one palette entry. Run is pure: it maps parsed args to the
// Msg that expresses the intent; the router interprets the Msg. args are
// the tokens after the command word of the palette input
// (":send --flag value" → args ["--flag", "value"]).
type Action struct {
	ID       string
	Title    string
	Keywords []string // extra searchable text (aliases, hotkey digits)
	Hints    []string // short right-aligned hints shown in the list
	Run      func(args []string) tea.Msg
}

// Registry is the ordered action list; registration order is the
// matcher's stable tie-break.
type Registry struct {
	actions []Action
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register appends an action; duplicate IDs are a wiring bug and ignored.
func (r *Registry) Register(a Action) {
	if a.Run == nil {
		return
	}
	for _, e := range r.actions {
		if e.ID == a.ID {
			return
		}
	}
	r.actions = append(r.actions, a)
}

// Actions returns the registered actions in registration order.
func (r *Registry) Actions() []Action { return r.actions }

// jumpAction builds the palette action for page slot i (0-based): the
// title is the wireframe's "go to <page>" label, the hint is the digit.
func jumpAction(i int, id, title string, keywords []string) Action {
	kw := append([]string{id, "page", itoa(i + 1)}, keywords...)

	return Action{
		ID:       "goto." + id,
		Title:    title,
		Keywords: kw,
		Hints:    []string{itoa(i + 1)},
		Run: func([]string) tea.Msg {
			return GoToPageMsg{ID: id}
		},
	}
}

// DashboardActions returns the §A quick-action list exactly as the
// wireframe draws it: Connect / reconnect, then the five page-entry
// verbs whose dim badge is the target page's hotkey digit. Enter runs
// the row; the Run closures emit the same GoToPageMsg/OpenConnectMsg the
// digit hotkeys emit, so every entry point lands on one router handler.
// The Disconnect row (TUI-514) rides along — the page hides it while no
// connection is live.
func DashboardActions() []Action {
	out := []Action{
		{
			ID:       "connect",
			Title:    "Connect / reconnect",
			Keywords: []string{"connect", "conn", "reconnect", "dial", "listen"},
			Hints:    []string{"c"},
			Run:      func([]string) tea.Msg { return OpenConnectMsg{} },
		},
		{
			ID:       "disconnect",
			Title:    "Disconnect",
			Keywords: []string{"disconnect", "drop", "close", "hangup", "offline"},
			Hints:    []string{"D"},
			Run:      func([]string) tea.Msg { return DisconnectMsg{} },
		},
	}
	// Proposal 04 §B: the wizard row sits right under the connect pair;
	// the page hides it until a connection exists (and swaps the
	// connect/disconnect rows by connection state). UAT round 5: the
	// row sends directly when the session config is complete, falling
	// back to the wizard otherwise.
	out = append(out, Action{
		ID:       "send-wizard",
		Title:    "Send transaction",
		Keywords: []string{"send", "wizard", "compose", "template"},
		Hints:    []string{"s"},
		Run:      func([]string) tea.Msg { return DirectSendMsg{} },
	})
	// UAT: return to the last send result without re-sending; the root
	// toasts "no previous send yet" when nothing ran.
	out = append(out, Action{
		ID:       "last-send",
		Title:    "View last send",
		Keywords: []string{"last", kwPrevious, "result", "again", "resend"},
		Run:      func([]string) tea.Msg { return LastSendViewMsg{} },
	})
	// UAT round 5: the scrollable send history (Enter freezes §D on a
	// row; h there toggles detail ↔ hex).
	out = append(out, Action{
		ID:       "send-history",
		Title:    "Send history",
		Keywords: []string{kwHistory, kwPrevious, "sends", "log"},
		Run:      func([]string) tea.Msg { return SendHistoryMsg{} },
	})
	// Proposal 05 §3: reopen the last completed stress run's summary
	// overlay; the page hides the row until a stress run completed (and
	// the root toasts when none exists).
	out = append(out, Action{
		ID:       "last-stress",
		Title:    "Stress summary",
		Keywords: []string{"stress", "summary", "percentiles", "tps", "p99"},
		Run:      func([]string) tea.Msg { return LastStressSummaryMsg{} },
	})
	// Wireframe §A rows: verb + the hotkey badge of the page it enters.
	// ("Send transaction" left this list for the wizard row above.)
	verbs := []struct {
		slot  int // 0-based pageJumps slot; the badge is slot+1
		title string
	}{
		{2, "Run scenario"},         // (3) scenarios
		{3, "Start mock server"},    // (4) server
		{4, "Start stress test"},    // (5) workers
		{6, "Analyze PCAP capture"}, // (7) analyze
	}
	for _, v := range verbs {
		a := jumpAction(v.slot, pageJumps[v.slot].id, v.title, pageJumps[v.slot].keywords)
		out = append(out, a)
	}

	return out
}

// pageJumpSpec is one hotkey jump slot: the registry id (mirroring
// internal/tui.PageIDs, hard-coded here so palette stays leaf-level), the
// wireframe footer label, the palette title, and searchable keywords
// (legacy ids stay findable so muscle memory like ":send" keeps working).
type pageJumpSpec struct {
	id       string
	label    string
	title    string
	keywords []string
}

// pageJumps is the wireframe's 1..8 page order:
// "1 dash 2 tx 3 scenarios 4 server 5 workers 6 sessions 7 analyze 8 ctf".
var pageJumps = []pageJumpSpec{
	{"dashboard", "dash", "go to dashboard", []string{"dash", "status", "home"}},
	{"transactions", "tx", "go to transactions", []string{"tx", "send", "transaction", "message"}},
	{"scenarios", "scenarios", "go to scenarios", []string{"scenario", "flow", "flows", "suite", "steps"}},
	{"server", "server", "go to mock server", []string{"mock", "listener", "serve"}},
	{"workers", "workers", "go to workers", []string{"stress", "load", "bgsend", "worker"}},
	{"sessions", "sessions", "go to sessions", []string{"db", "database", "store", kwHistory}},
	{"analyze", "analyze", "go to analyze", []string{"pcap", "capture", "trace"}},
	{"ctf", "ctf", "go to CTF export", []string{"clearing", "export", "base2", "visa"}},
}

// Seed builds the palette registry: the 8 wireframe page jumps (with the
// hotkey digit as their hint) plus show-help (push) and quit. The Run
// closures stay router Msgs.
func Seed() *Registry {
	r := NewRegistry()

	// SCR-505: the §E connect dialog, first in the registry so the §A
	// quick-actions list shows "Connect / reconnect" on top (wireframe §A
	// / §E). Run emits OpenConnectMsg; the router opens the overlay.
	r.Register(Action{
		ID:       "connect",
		Title:    "Connect / reconnect",
		Keywords: []string{"connect", "conn", "reconnect", "dial", "listen"},
		Hints:    []string{"c"},
		Run: func([]string) tea.Msg {
			return OpenConnectMsg{}
		},
	})

	// TUI-514 (closes REGRESSION-1): drop the live connection without
	// quitting or reconnecting — the REPL `disconnect` parity entry.
	// Second in the registry so the §A quick-actions list shows it right
	// under Connect; the page hides the row while no connection exists
	// (DashboardState.HasConnection). Run emits DisconnectMsg; the router
	// decides (direct leg, §N3 confirm, or sane info no-op).
	r.Register(Action{
		ID:       "disconnect",
		Title:    "Disconnect",
		Keywords: []string{"disconnect", "drop", "close", "hangup", "offline"},
		Hints:    []string{"D"},
		Run: func([]string) tea.Msg {
			return DisconnectMsg{}
		},
	})

	// Proposal 04 §B + UAT round 5: the send family (wizard, last-send
	// view, send history).
	registerSendActions(r)

	for i, j := range pageJumps {
		r.Register(jumpAction(i, j.id, j.title, j.keywords))
	}

	// The §L settings page has no hotkey slot (the wireframe's 8 slots are
	// taken), so it carries no digit hint; the router resolves
	// GoToPageMsg{"settings"} against the registry.
	r.Register(Action{
		ID:       "goto.settings",
		Title:    "go to settings",
		Keywords: []string{"settings", "config", "preferences", "timeouts"},
		Run: func([]string) tea.Msg {
			return GoToPageMsg{ID: "settings"}
		},
	})

	r.Register(Action{
		ID:       "help",
		Title:    "show help",
		Keywords: []string{"help", "keys", "?"},
		Hints:    []string{"?"},
		Run: func([]string) tea.Msg {
			return PushPageMsg{ID: "help"}
		},
	})

	r.Register(Action{
		ID:       "quit",
		Title:    "quit jiso",
		Keywords: []string{"quit", "exit", "q"},
		Hints:    []string{"q"},
		Run: func([]string) tea.Msg {
			return QuitRequestMsg{}
		},
	})

	return r
}

// itoa formats a small non-negative int without pulling in strconv at
// every call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 && i > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	return string(buf[i:])
}
