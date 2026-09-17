// transactions_state.go holds the §B state contract and the page→router
// messages. Root builds TransactionsState from internal/app and pushes it
// via SetState; the page never touches the app or the clock.
package pages

import "strings"

// TransactionsPageID is the router slot name of the transactions page:
// hotkey 2, footer label "tx". The §D send exchange is a drill-down
// reached from here (see SendPageID), not a hotkey slot.
const TransactionsPageID = "transactions"

// TxRow is one table row as pre-derived display strings. Empty fields
// render as the dash (unknown ≠ zero); ID is the stable row identity that
// survives filter and sort recomposition.
type TxRow struct {
	ID          string
	Name        string
	MTI         string
	Description string
	Dataset     string
	Spec        string
}

// TransactionsState is the immutable snapshot root pushes into the tx
// page. FileName is the loaded file's base name ("" → empty state); Error
// carries a failed tx-file load — the page must say WHY it did not load,
// never fall back to silence.
type TransactionsState struct {
	FileName string
	TxCount  int
	Rows     []TxRow
	Error    string
}

// TxPopMsg is Esc outside the filter on §B: unwinds to the dashboard
// (the root's popPage contract).
type TxPopMsg struct{}

// TxDetailMsg asks the router to open the message inspector (§C) for the
// row with ID; root builds the inspector state and pushes the page.
type TxDetailMsg struct {
	ID string
}

// TxSendMsg asks the router to start the §D send flow for the row with ID.
// A send while an op is in flight is ignored (no queue, no retry).
type TxSendMsg struct {
	ID string
}

// TxPickFileMsg asks the router to open the tx-file picker. Today it is a
// logged no-op.
type TxPickFileMsg struct{}

// matchText is the filter haystack: the lowercased concatenation of every
// display field, so one substring hit on any column keeps the row.
func (r TxRow) matchText() string {
	return strings.ToLower(r.Name + " " + r.MTI + " " + r.Description + " " +
		r.Dataset + " " + r.Spec)
}
