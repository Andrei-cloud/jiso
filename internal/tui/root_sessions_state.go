// root_sessions_state.go derives the §I SessionsState snapshot: list
// rows with root-derived short ids and relative times, the stats card
// lines, the tx history cells and the reconstructed review sections —
// all finished display data from the injectable clock; the only math
// is the ok-percentage and the dash substitution.
package tui

import (
	"strconv"
	"strings"
	"time"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// syncSessions pushes a fresh §I snapshot into the canonical page
// instance (Update-wrapper placement mirrors syncWorkers).
func (m *RootModel) syncSessions() {
	if m.sessions == nil {
		return
	}
	m.sessions.SetState(m.sessionsState())
}

// sessionsState assembles the snapshot from the query caches.
func (m *RootModel) sessionsState() pages.SessionsState {
	now := m.now()
	st := pages.SessionsState{
		DBPath:     m.sessionsDBPath(),
		Note:       m.sessionsNote,
		SelectedID: m.sessionsSelected,
		DetailWait: m.sessionsDetailWait,
	}
	for _, s := range m.sessionsList {
		st.Sessions = append(st.Sessions, pages.SessionRow{
			ID:      s.SessionID,
			ShortID: m.shortSessionID(s.SessionID),
			When:    app.FormatRelativeTime(now, s.LastActiveTime),
		})
	}
	if m.sessionsStats != nil {
		st.Stats = sessionsStatsLines(m.sessionsStats)
	}
	for _, tx := range m.sessionsHistory {
		st.History = append(st.History, m.txHistoryRow(tx))
	}
	if m.sessionsReview != nil {
		st.Review = txReviewState(m.themeOrNil(), m.sessionsReview)
	}

	return st
}

// shortSessionID is the §A/§I/§K list cell; the rule and the glyph inside
// it belong to the theme (Theme.ShortID), never a hand-typed value.
func (m *RootModel) shortSessionID(id string) string {
	return m.themeOrNil().ShortID(id)
}

// sessionsStatsLines renders the wireframe's five stats lines.
func sessionsStatsLines(st *app.DbSessionStats) []pages.SummaryKV {
	lines := []pages.SummaryKV{
		{Label: "total", Value: countCell(st.TotalTransactions)},
		{Label: "ok", Value: countCell(st.SuccessfulTransactions) + " (" + okPercent(st) + ")"},
		{Label: "fail", Value: countCell(st.FailedTransactions)},
		{Label: "avg", Value: msCell(st.AverageProcessingTimeMs) + " ms"},
		{Label: "RC dist", Value: rcDistCell(st.ResponseCodeDistribution)},
	}

	return lines
}

// okPercent is the wireframe's "98.7%" success share (dash when the
// total is unknown/zero).
func okPercent(st *app.DbSessionStats) string {
	if st.TotalTransactions <= 0 {
		return "\u2014"
	}
	pct := float64(st.SuccessfulTransactions) * 100 / float64(st.TotalTransactions)

	return msCell(pct) + "%"
}

// rcDistCell renders "00:148 96:2" (codes ascending, the CLI summary's
// stable order); empty renders the dash.
func rcDistCell(codes map[string]int) string {
	keys := sortedRCCodes(codes)
	if len(keys) == 0 {
		return "\u2014"
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+":"+countCell(codes[k]))
	}

	return strings.Join(parts, " ")
}

// txHistoryRow derives one TX HISTORY display row. The canonical
// status token is ok/fail, with the wireframe's "timeout" for a failed
// row that never received a response (no RC stored); the latency cell
// stays empty (the page dashes it).
func (m *RootModel) txHistoryRow(tx app.DbTransactionView) pages.TxHistoryRow {
	status := pages.TxStatusFail
	switch {
	case tx.Success:
		status = pages.TxStatusOK
	case tx.ResponseCode == "":
		status = pages.TxStatusTimeout
	}
	latency := txLatencyCell(tx.ProcessingTime, status)

	return pages.TxHistoryRow{
		ID:      tx.ID,
		Time:    tx.Timestamp.Format("15:04:05"),
		Name:    tx.TxName,
		MTI:     tx.MTI,
		RC:      tx.ResponseCode,
		Status:  status,
		Latency: latency,
	}
}

// txLatencyCell renders "3ms" / "3.4ms" (sub-ms keeps one decimal);
// timeout rows carry no number at all — the page dashes the cell and the
// status token already says "timeout".
func txLatencyCell(d time.Duration, status string) string {
	if status == pages.TxStatusTimeout {
		return ""
	}
	if d <= 0 {
		return ""
	}
	if ms := d.Milliseconds(); d == time.Duration(ms)*time.Millisecond {
		return strconv.FormatInt(ms, 10) + "ms"
	}

	return msCell(float64(d.Microseconds())/1000) + "ms"
}

// txReviewState builds the §I review overlay state from the façade's
// retrospective (APP read path; same sections `jiso db tx <id>` prints):
// headline counters, then the request/response hex + describe sections.
func txReviewState(th *theme.Theme, rev *app.DbTransactionRetrospective) *pages.TxReviewState {
	sep := th.Separator()
	rc := rev.ResponseCode
	if rc == "" {
		rc = "\u2014"
	}
	latency := txLatencyCell(rev.ProcessingTime, txReviewStatus(rev))
	mark := "\u2717"
	if rev.Success {
		mark = "\u2713"
	}
	if th != nil && th.ASCII {
		mark = map[bool]string{true: "[ok]", false: "[x]"}[rev.Success]
	}

	out := &pages.TxReviewState{
		TxID: rev.ID,
		Headline: []string{
			plainDash(th, rev.TxName) + sep + "id " + strconv.FormatInt(rev.ID, 10) +
				sep + "RC " + rc + sep + latency + sep + mark,
		},
		Request:  txReviewMessage(rev.Request),
		Response: txReviewMessage(rev.Response),
	}
	if rev.SessionID != "" {
		out.Headline = append(out.Headline, "session "+rev.SessionID)
	}

	return out
}

// txReviewStatus mirrors txHistoryRow's token mapping for the headline.
func txReviewStatus(rev *app.DbTransactionRetrospective) string {
	switch {
	case rev.Success:
		return pages.TxStatusOK
	case rev.ResponseCode == "":
		return pages.TxStatusTimeout
	default:
		return pages.TxStatusFail
	}
}

// txReviewMessage maps one reconstructed message (nil in, nil out).
func txReviewMessage(m *app.DbMessageReconstruction) *pages.TxReviewMessage {
	if m == nil {
		return nil
	}

	return &pages.TxReviewMessage{
		HEX:         m.HEX,
		Describe:    m.DescribeText,
		RawFallback: m.RawFallback,
		ParseError:  m.ParseError,
	}
}

// plainDash is the package-level theme-dash helper (the RootModel
// method dashIfRoot is the same rule with the model's theme).
func plainDash(th *theme.Theme, s string) string {
	if s != "" {
		return s
	}
	if th != nil && th.ASCII {
		return "-"
	}

	return "\u2014"
}
