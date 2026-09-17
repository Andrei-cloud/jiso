// root_ctf.go owns the §K CTF export truth. The page is
// presentation-only: root queries the App CTF façade (ListCtfSessions/
// PreviewExport/WriteExport — the same db.OpenExisting + base2 path the
// CLI `ctf` shims drive) OFF the UI thread: every leg runs in
// a tea.Cmd and reports back as a seq-tokened msg, so Update never
// blocks and never touches the filesystem. Generate runs the dry
// preview (nothing written) plus an os.Stat leg for the §N3 overwrite
// warning; w re-stats root-side and opens the ConfirmDialog (default
// No) when the target exists — a silent overwrite never happens;
// confirm proceeds with the write, cancel writes nothing. A missing
// or unset DB is a typed façade error folded into the state's Note
// (empty-state text), never a crash and never a created file.
// ctfSrc overrides the app legs for tests (fake façade; no optimistic
// writes — the state changes only when a result msg arrives).
package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/db"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// ctfSource is the §K façade leg: the App methods match it
// structurally, and tests inject a fake (no real DB above the seam).
type ctfSource interface {
	DBPath() string
	ListCtfSessions(ctx context.Context) ([]app.CtfSessionView, error)
	PreviewExport(ctx context.Context, sessionID, cib, binFilter string, batch int) (*app.CtfExportSummary, error)
	WriteExport(ctx context.Context, sessionID, cib, binFilter string, batch int, outPath string) (*app.CtfExportSummary, error)
}

// ctfSource resolves the injectable leg (nil = the App façade; nil
// App = no leg, the page stays in its empty state).
func (m *RootModel) ctfSource() ctfSource {
	if m.ctfSrc != nil {
		return m.ctfSrc
	}
	if m.app == nil {
		return nil
	}

	return m.app
}

// ctfStat resolves the injectable os.Stat leg (nil = os.Stat).
func (m *RootModel) ctfStat() func(string) (os.FileInfo, error) {
	if m.ctfStatFn != nil {
		return m.ctfStatFn
	}

	return os.Stat
}

// Result messages from the tea.Cmd goroutines; seq marks the load
// generation (a stale seq is ignored — the serverTickSeq lifecycle).
type (
	ctfListLoadedMsg struct {
		seq   uint64
		views []app.CtfSessionView
		err   error
	}
	ctfPreviewLoadedMsg struct {
		seq     uint64
		id      string
		summary *app.CtfExportSummary
		exists  bool
		err     error
		// open marks the Enter leg: its result opens the record viewer.
		// The cursor-following select leg (open=false) folds the same
		// summary into the SUMMARY line without touching the overlay.
		open bool
	}
	ctfWriteStatMsg struct {
		seq    uint64
		id     string
		params pages.CtfParams
		exists bool
	}
	ctfWriteLoadedMsg struct {
		seq     uint64
		summary *app.CtfExportSummary
		err     error
	}
)

// armCtf arms the eligible-sessions query while the page is current:
// on entry, after `r`, and after a dirtying event. Returns nil
// otherwise (the query never runs for an unfocused page).
func (m *RootModel) armCtf() tea.Cmd {
	if m.Current() == nil || m.Current().ID() != pages.CtfPageID {
		return nil
	}
	src := m.ctfSource()
	if src == nil {
		return nil
	}
	if m.ctfListWait || (m.ctfListLoaded && !m.ctfListDirty) {
		return nil
	}
	m.ctfListWait, m.ctfListDirty = true, false
	m.ctfSeq++ // new generation: in-flight preview/write msgs turn stale
	seq := m.ctfSeq

	return func() tea.Msg {
		views, err := src.ListCtfSessions(context.Background())

		return ctfListLoadedMsg{seq: seq, views: views, err: err}
	}
}

// applyCtfList folds a list result: typed errors become the
// empty-state Note (the list clears, nothing is fabricated); a
// selection that left the list falls to the newest session. The wait
// flag clears BEFORE the stale check (the
// applySessionsDetail pattern): handleCtfGenerate bumps the seq while a
// list load is in flight, and a stale-return that kept ctfListWait true
// permanently froze armCtf for the session.
func (m *RootModel) applyCtfList(msg ctfListLoadedMsg) (tea.Model, tea.Cmd) {
	m.ctfListWait = false
	if msg.seq != m.ctfSeq {
		return m, nil
	}
	if msg.err != nil {
		m.ctfListLoaded = true
		m.ctfNote = ctfErrorText(msg.err)
		m.ctfList, m.ctfSelected = nil, ""
		m.ctfSummary, m.ctfSummaryID = nil, "" // the summary described a gone session

		return m, nil
	}
	m.ctfNote = ""
	m.ctfListLoaded = true
	m.ctfList = msg.views

	found := false
	for _, v := range msg.views {
		if v.SessionID == m.ctfSelected {
			found = true

			break
		}
	}
	if !found {
		m.ctfSelected = ""
		if len(msg.views) > 0 {
			m.ctfSelected = msg.views[0].SessionID
		}
		m.ctfSummary, m.ctfSummaryID = nil, "" // re-home: stale summary of a gone session
	}

	return m, nil
}

// handleCtfRefresh marks the cache dirty (`r`); the Update-wrapper arm
// re-queries on the spot (the page is current by construction).
func (m *RootModel) handleCtfRefresh() (tea.Model, tea.Cmd) {
	m.ctfListDirty = true

	return m, m.armCtf()
}

// handleCtfGenerate is Enter: open the record viewer. A summary the
// cursor-following dry leg already computed for exactly this
// session+params is REUSED without a second query;
// otherwise Enter runs the dry preview leg (PreviewExport
// writes nothing) plus the overwrite stat, and the overlay opens when
// the result arrives.
func (m *RootModel) handleCtfGenerate(msg pages.CtfGenerateMsg) (tea.Model, tea.Cmd) {
	if m.Current().ID() != pages.CtfPageID || m.ctfPreviewWait || m.ctfWriteWait {
		return m, nil
	}
	if m.ctfSummary != nil && m.ctfSummaryID == msg.SessionID && m.ctfParams == msg.Params {
		m.ctfNote = ""
		m.ctfPreviewID++
		m.ctfOverlayOpen = true

		return m, nil
	}

	src := m.ctfSource()
	if src == nil {
		m.ctfNote = "no session database available"

		return m, nil
	}
	batch, ok := m.ctfLegBatch(msg.Params)
	if !ok {
		return m, nil
	}
	m.ctfParams = msg.Params
	m.ctfSelected = msg.SessionID
	m.ctfNote = ""
	m.ctfWriteLine, m.ctfWriteOK = "", false
	m.ctfPreviewWait = true
	m.ctfSeq++
	seq := m.ctfSeq
	id, out := msg.SessionID, strings.TrimSpace(msg.Params.OutPath)
	stat := m.ctfStat()

	return m, func() tea.Msg {
		summary, err := src.PreviewExport(context.Background(), id,
			msg.Params.CIB, msg.Params.Bin, batch)
		exists := false
		if err == nil && out != "" {
			_, serr := stat(filepath.Clean(out))
			exists = serr == nil
		}

		return ctfPreviewLoadedMsg{seq: seq, id: id, summary: summary, exists: exists, err: err, open: true}
	}
}

// handleCtfSelect is the cursor-following dry leg: the
// list cursor moved onto a different session, or a form edit changed
// the batch — root re-runs the dry preview for the row under the
// cursor and folds it into the SUMMARY line WITHOUT opening the
// overlay. A new move bumps the seq, so in-flight results from the
// previous one turn stale (the serverStats lifecycle).
func (m *RootModel) handleCtfSelect(msg pages.CtfSelectMsg) (tea.Model, tea.Cmd) {
	if m.Current().ID() != pages.CtfPageID || m.ctfWriteWait {
		return m, nil
	}

	src := m.ctfSource()
	if src == nil {
		m.ctfNote = "no session database available"

		return m, nil
	}
	batch, ok := m.ctfLegBatch(msg.Params)
	if !ok {
		return m, nil
	}
	m.ctfParams = msg.Params
	m.ctfSelected = msg.SessionID
	m.ctfNote = ""
	m.ctfPreviewWait = true
	m.ctfSeq++
	seq := m.ctfSeq
	id := msg.SessionID

	return m, func() tea.Msg {
		summary, err := src.PreviewExport(context.Background(), id,
			msg.Params.CIB, msg.Params.Bin, batch)

		return ctfPreviewLoadedMsg{seq: seq, id: id, summary: summary, err: err}
	}
}

// ctfLegBatch validates the form's batch root-side and returns its
// engine-int meaning. An explicitly given batch must be positive
// "0"/"-5" were once accepted and forwarded to the façade;
// the empty field keeps its engine-default meaning.
func (m *RootModel) ctfLegBatch(params pages.CtfParams) (int, bool) {
	batch, err := strconv.Atoi(strings.TrimSpace(params.Batch))
	if err != nil && strings.TrimSpace(params.Batch) != "" {
		m.ctfNote = "batch number must be a positive integer"

		return 0, false
	}
	if err == nil && batch <= 0 {
		m.ctfNote = "batch number must be a positive integer"

		return 0, false
	}

	return batch, true
}

// applyCtfPreview folds the dry result: success stores the summary (the
// SUMMARY line + viewer source); an error lands as the Note (no viewer,
// nothing written, no fabricated records). Only the OPEN leg (Enter)
// arms the record viewer — the cursor-following select leg folds the
// SUMMARY line and stops there.
func (m *RootModel) applyCtfPreview(msg ctfPreviewLoadedMsg) (tea.Model, tea.Cmd) {
	m.ctfPreviewWait = false
	if msg.seq != m.ctfSeq || m.Current().ID() != pages.CtfPageID {
		return m, nil
	}
	if msg.err != nil {
		m.ctfNote = ctfErrorText(msg.err)
		m.ctfSummary, m.ctfSummaryID = nil, ""

		return m, nil
	}
	m.ctfNote = ""
	m.ctfSummary = msg.summary
	m.ctfSummaryID = msg.id
	if !msg.open {
		return m, nil
	}
	m.ctfPreviewOverw = msg.exists
	m.ctfPreviewID++
	m.ctfOverlayOpen = true

	return m, nil
}

// handleCtfWrite is w inside the overlay: stat the target root-side
// off the UI thread; the result routes to §N3 confirm or straight to
// the write. The form values are snapshotted so the write runs with
// exactly the arguments that produced the preview.
func (m *RootModel) handleCtfWrite() (tea.Model, tea.Cmd) {
	if m.Current().ID() != pages.CtfPageID || m.ctfSummary == nil ||
		m.ctfPreviewWait || m.ctfWriteWait || m.ctfConfirm != nil {
		return m, nil
	}
	m.ctfWriteWait = true
	m.ctfWriteLine, m.ctfWriteOK = "", false
	seq := m.ctfSeq
	id, params := m.ctfSummaryID, m.ctfParams
	stat := m.ctfStat()

	return m, func() tea.Msg {
		exists := false
		if out := strings.TrimSpace(params.OutPath); out != "" {
			_, serr := stat(filepath.Clean(out))
			exists = serr == nil
		}

		return ctfWriteStatMsg{seq: seq, id: id, params: params, exists: exists}
	}
}

// applyCtfWriteStat routes the stat verdict: an existing target opens
// the §N3 overwrite confirm (default No — never a silent write); a
// fresh target proceeds.
func (m *RootModel) applyCtfWriteStat(msg ctfWriteStatMsg) (tea.Model, tea.Cmd) {
	m.ctfWriteWait = false
	if msg.seq != m.ctfSeq || m.Current().ID() != pages.CtfPageID {
		return m, nil
	}
	if msg.exists {
		m.ctfConfirm = widgets.NewConfirmDialog(m.themeOrNil(),
			"overwrite "+strings.TrimSpace(msg.params.OutPath)+"?")

		return m, nil
	}

	return m.armCtfWrite(msg.id, msg.params)
}

// armCtfWrite launches the write leg (the -o path) with the
// snapshotted arguments; a missing directory surfaces as the write
// error text, never a created tree.
func (m *RootModel) armCtfWrite(id string, params pages.CtfParams) (tea.Model, tea.Cmd) {
	src := m.ctfSource()
	if src == nil {
		return m, nil
	}
	m.ctfWriteWait = true
	batch, _ := strconv.Atoi(strings.TrimSpace(params.Batch))
	seq := m.ctfSeq

	return m, func() tea.Msg {
		summary, err := src.WriteExport(context.Background(), id,
			params.CIB, params.Bin, batch, params.OutPath)

		return ctfWriteLoadedMsg{seq: seq, summary: summary, err: err}
	}
}

// applyCtfWrite folds the write result into the toast-style line and
// refreshes the stored summary to the written one.
func (m *RootModel) applyCtfWrite(msg ctfWriteLoadedMsg) (tea.Model, tea.Cmd) {
	m.ctfWriteWait = false
	if msg.seq != m.ctfSeq || m.Current().ID() != pages.CtfPageID {
		return m, nil
	}
	m.ctfOverlayOpen = false
	if msg.err != nil {
		m.ctfWriteLine = "write failed: " + msg.err.Error()
		m.ctfWriteOK = false

		return m, nil
	}
	m.ctfWriteLine = "wrote " + strconv.Itoa(msg.summary.Records) +
		" records to " + msg.summary.OutputPath
	m.ctfWriteOK = true
	m.ctfSummary = msg.summary
	m.ctfPreviewOverw = msg.summary.Overwrote

	return m, nil
}

// applyCtfConfirmed / applyCtfCancelled drive the §N3 overwrite
// confirm (default No writes nothing). The write runs with the stored
// m.ctfParams — never a path recovered from the question text
// (an OutPath ending "?" lost its suffix to the old
// TrimPrefix/TrimSuffix round-trip).
func (m *RootModel) applyCtfConfirmed() (tea.Model, tea.Cmd) {
	m.ctfConfirm = nil

	return m.armCtfWrite(m.ctfSummaryID, m.ctfParams)
}

func (m *RootModel) applyCtfCancelled() (tea.Model, tea.Cmd) {
	m.ctfConfirm = nil
	m.ctfWriteWait = false

	return m, nil
}

// leaveCtf bumps the seq when navigation replaces/pushes away from
// the §K page (the leave-side cancel pattern).
func (m *RootModel) leaveCtf() {
	if m.Current().ID() == pages.CtfPageID {
		m.ctfSeq++
		m.ctfListWait, m.ctfPreviewWait, m.ctfWriteWait = false, false, false
	}
}

// ctfErrorText renders a façade error as §K empty-state/inline text:
// the two typed DB states name their next action, a
// config-class error already names its session id (never fabricated
// records), anything else stays verbatim.
func ctfErrorText(err error) string {
	switch {
	case errors.Is(err, app.ErrDBNotConfigured):
		return pages.EmptyTextNoSessionDB
	case errors.Is(err, db.ErrDBNotFound):
		var cfgErr *app.ConfigError
		if errors.As(err, &cfgErr) && cfgErr.Path != "" {
			return "no CTF-eligible sessions in " + cfgErr.Path + ". Pass --db to enable logging."
		}

		return "database file does not exist - pass --db to enable session logging"
	default:
		return err.Error()
	}
}
