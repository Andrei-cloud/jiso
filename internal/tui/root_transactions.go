package tui

import (
	"encoding/json"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// syncTransactions pushes a fresh TransactionsState snapshot into the
// canonical §B page instance. It runs from NewRootModel and after every
// Update: root owns all App access and derives the display strings, so
// the page never imports internal/app and never reads the clock.
func (m *RootModel) syncTransactions() {
	if m.tx == nil {
		return
	}
	m.tx.SetState(m.transactionsState())
}

// transactionsState derives the §B snapshot from the app's loaded tx
// file: base name, row count, and one row per transaction (name, MTI
// parsed from field 0 of the template, description, and the DATASET/SPEC
// cells pre-derived from the repository's per-transaction info).
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
			Dataset:     pages.DatasetCell(info.Dataset, info.DatasetRows),
			Spec:        specCell(info.Spec),
		})
	}

	return st
}

// specCell is the SPEC display: the base name of the entry's declared spec
// path. The fallback spec is never shown as if the entry had declared one,
// so "" stays "" and the page renders the dash.
func specCell(declared string) string {
	if declared == "" {
		return ""
	}

	return filepath.Base(declared)
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
// opens the §C inspector with state built for that tx. s on §B (and
// Enter on the §D page — same TxSendMsg path) starts the live exchange
// when the connection and the dial target are set: root pushes §D and
// launches the stage goroutine; a send while one is in flight is ignored
// (no queue, no retry). When either is missing the same key opens the
// send wizard instead — its connect-first walk collects the missing
// elements with the chosen transaction pre-selected, and no send
// starts. f on §B (TxPickFileMsg)
// opens the shared file picker, and a selection commits the tx-file
// path through the same settings commit path §L uses — a file whose
// entries declare no spec chains into a spec browse first. The compose-
// with-dataset run stays a logged no-op.
func (m *RootModel) handleTxMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pages.TxDetailMsg:
		m.debug.logf("tx detail id=%s", msg.ID)
		m.openInspector(msg.ID)
	case pages.TxSendMsg:
		m.debug.logf("tx send id=%s", msg.ID)
		if m.app != nil && !m.sendTargetReady() {
			// Offline or no dialable target: the wizard takes the
			// operator through the missing elements (connect first) with
			// this transaction already under the template cursor.
			return m.openSendWizardFor(msg.ID)
		}

		return m, m.startSend(msg.ID)
	case pages.TxPickFileMsg:
		m.debug.logf("tx pick file")

		return m.handleTxPickFile()
	case pages.TxAssignSpecMsg:
		m.debug.logf("tx assign spec id=%s", msg.ID)

		return m.handleTxAssignSpec(msg.ID)
	case pages.TxComposeMsg:
		m.debug.logf("tx compose id=%s", msg.ID)
	}

	return m, nil
}
