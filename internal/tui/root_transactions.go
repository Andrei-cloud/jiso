package tui

import (
	"encoding/json"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// syncTransactions pushes a fresh TransactionsState snapshot into the
// canonical §B page instance. It runs from NewRootModel and after every
// Update (the SCR-501 dashboard pattern): root owns all App access and
// derives every display string here, so the page never imports
// internal/app and never reads the clock. A nil app (tests, pre-wire)
// yields the empty snapshot → the page renders its empty state.
func (m *RootModel) syncTransactions() {
	if m.tx == nil {
		return
	}
	m.tx.SetState(m.transactionsState())
}

// transactionsState derives the §B snapshot from the app's loaded tx
// file: the file's base name, the repository's row count, and one row per
// transaction (name, MTI parsed from field 0 of the template, description).
// Dataset and Spec stay empty — the transactions.Repository does not
// expose them per transaction yet — and render as the dash (unknown ≠
// zero) until a later ticket plumbs them.
func (m *RootModel) transactionsState() pages.TransactionsState {
	if m.app == nil {
		return pages.TransactionsState{}
	}
	cfg, repo := m.app.Config(), m.app.Transactions()
	if cfg == nil || repo == nil {
		return pages.TransactionsState{}
	}

	names := repo.ListNames()
	st := pages.TransactionsState{TxCount: len(names), Error: m.txFileLoadErr}
	if file := cfg.GetFile(); file != "" {
		st.FileName = filepath.Base(file)
	}

	for _, name := range names {
		info, err := repo.Info(name)
		txName, desc, fieldsJSON := info.Name, info.Description, info.FieldsJSON
		if err != nil || txName == "" {
			txName, desc = name, ""
		}
		st.Rows = append(st.Rows, pages.TxRow{
			ID:          name,
			Name:        txName,
			MTI:         mtiFromFields(fieldsJSON),
			Description: desc,
		})
	}

	return st
}

// mtiFromFields extracts the message-type indicator from the transaction
// template's field map ("0" key). A JSON string yields its value; any
// other JSON scalar is shown verbatim; anything unparsable stays empty
// (the page renders the dash).
func mtiFromFields(fieldsJSON string) string {
	if fieldsJSON == "" {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
		return ""
	}
	raw, ok := fields["0"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	return string(raw)
}

// handleTxMsg interprets the §B/§C pages' row messages. Enter on §B
// (TxDetailMsg) opens the §C inspector with state built for that tx
// (SCR-503 — the real transition the wireframe breadcrumb implies). Since
// SCR-504 s on §B (and Enter on the §D page — the same TxSendMsg path)
// starts the live exchange: root pushes the §D page and launches the
// stage goroutine; a send while one is in flight is ignored (no queue, no
// retry). Since E5-FIX/M6 t on §B (TxPickFileMsg — advertised by the §B
// empty state and the §M registry) opens the shared file picker through
// the OpenFilePickerMsg seam (giving that message a real emitter), and a
// selection commits the tx-file path through the same settings commit
// path §L uses. The compose-with-dataset run lands later: it stays a
// logged no-op — the stack never changes and no command runs for it.
func (m *RootModel) handleTxMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.TxDetailMsg:
		m.debug.logf("tx detail id=%s", msg.ID)
		m.openInspector(msg.ID)
	case pages.TxSendMsg:
		m.debug.logf("tx send id=%s", msg.ID)
		return m, m.startSend(msg.ID)
	case pages.TxPickFileMsg:
		m.debug.logf("tx pick file")

		return m.handleTxPickFile()
	case pages.TxComposeMsg:
		m.debug.logf("tx compose id=%s", msg.ID)
	}

	return m, nil
}
