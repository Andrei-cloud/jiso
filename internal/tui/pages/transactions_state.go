// transactions_state.go holds the §B state contract (wireframe
// .opencode/plans/02-tui-wireframes.md §B) and the page→router messages.
// Root builds TransactionsState from internal/app and pushes it via
// SetState; the page never touches the app or the clock (SCR-501 pattern).
package pages

import "strings"

// TransactionsPageID is the router slot name of the transactions page
// (wireframe §B): hotkey 2, footer label "tx". The §D send exchange is a
// drill-down reached from here (see SendPageID), not a hotkey slot.
const TransactionsPageID = "transactions"

// TxRow is one table row as pre-derived display strings. Empty fields
// render as the dash (unknown ≠ zero); ID is the stable row identity
// (the transaction name) that survives filter and sort recomposition.
// Dataset and Spec stay empty until the repository exposes them per
// transaction (a later ticket plumbs them).
type TxRow struct {
	ID          string
	Name        string
	MTI         string
	Description string
	Dataset     string
	Spec        string
}

// TransactionsState is the immutable snapshot root pushes into the
// transactions page. FileName is the loaded tx file's base name ("" = no
// tx file loaded → empty state); TxCount is the repository's row count.
// Error carries a failed tx-file load (UAT round 7): when picking a file
// the page must say WHY it did not load instead of falling back to silence.
type TransactionsState struct {
	FileName string
	TxCount  int
	Rows     []TxRow
	Error    string
}

// TxPopMsg is Esc outside the filter on §B: proposal 05 §4 unwinds to
// the dashboard (the root's popPage contract).
type TxPopMsg struct{}

// TxDetailMsg asks the router to open the message inspector (§C) for the
// row with ID. Root owns the transition: since SCR-503 it builds the
// InspectorState for that tx and pushes the inspector page.
type TxDetailMsg struct {
	ID string
}

// TxSendMsg asks the router to start the §D send flow for the row with ID.
// Root owns it since SCR-504: it pushes the §D exchange page and walks the
// live stages; the §D page's Enter re-send yields this same message, and
// one while an op is in flight is ignored (no queue, no retry).
type TxSendMsg struct {
	ID string
}

// TxPickFileMsg asks the router to open the tx-file picker (§N1,
// TUI-406b/M5-later). Today it is a logged no-op.
type TxPickFileMsg struct{}

// matchText is the filter haystack: the lowercased concatenation of every
// display field, so one substring hit on any column keeps the row.
func (r TxRow) matchText() string {
	return strings.ToLower(r.Name + " " + r.MTI + " " + r.Description + " " +
		r.Dataset + " " + r.Spec)
}
