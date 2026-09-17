// root_ctf_state.go derives the §K CtfState snapshot (the
// data-flow contract): the page receives display data only — list rows
// with root-derived short ids, relative times, and approved cells; the
// SUMMARY line with root-formatted money; the preview overlay content;
// the toast-style write line. syncCtf runs in the Update wrapper, so
// every folded message is reflected in the next View.
package tui

import (
	"strconv"
	"strings"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
)

// syncCtf prefills the form defaults once (the §K values:
// CIB 400129, batch 1, output ./out/CTF_001.dat — "last used" lives in
// root state, the page only renders it) and pushes the snapshot.
func (m *RootModel) syncCtf() {
	if m.ctf == nil {
		return
	}
	if !m.ctfPrefilled {
		m.ctfParams = pages.CtfParams{
			CIB:     app.DefaultCtfCIB,
			Batch:   "1",
			OutPath: "./out/CTF_001.dat",
		}
		m.ctfPrefilled = true
	}
	m.ctf.SetState(m.ctfState())
}

// ctfState builds the immutable snapshot the page renders.
func (m *RootModel) ctfState() pages.CtfState {
	st := pages.CtfState{
		DBPath:      m.ctfDBPath(),
		Note:        m.ctfNote,
		SelectedID:  m.ctfSelected,
		Params:      m.ctfParams,
		SummaryLine: m.ctfSummaryLine(),
		SummaryWait: m.ctfPreviewWait,
		WriteLine:   m.ctfWriteLine,
		WriteOK:     m.ctfWriteOK,
		PreviewID:   m.ctfPreviewID,
	}
	now := m.now()
	for _, v := range m.ctfList {
		st.Sessions = append(st.Sessions, pages.CtfSessionRow{
			ID:       v.SessionID,
			ShortID:  m.shortSessionID(v.SessionID),
			When:     app.FormatRelativeTime(now, v.LastActiveTime),
			Approved: strconv.Itoa(v.ApprovedCount) + " approved",
		})
	}
	if m.ctfSummary != nil && m.ctfOverlayOpen {
		st.Preview = m.ctfPreviewState()
	}

	return st
}

// ctfDBPath reports the path shown in the §K header ("" = not
// configured).
func (m *RootModel) ctfDBPath() string {
	src := m.ctfSource()
	if src == nil {
		return ""
	}

	return src.DBPath()
}

// ctfSummaryLine is the SUMMARY line: "148 tx · $ 12,450.00
// total · header/trailer dates auto" from the last preview (the dates
// are base2's, root renders the fixed tail).
func (m *RootModel) ctfSummaryLine() string {
	if m.ctfSummary == nil {
		return ""
	}
	sep := m.themeOrNil().Separator()

	return strconv.Itoa(m.ctfSummary.MonetaryTransactions) + " tx" + sep +
		"$ " + formatMoney(m.ctfSummary.DestinationAmountSum) + " total" + sep +
		"header/trailer dates auto"
}

// ctfPreviewState builds the overlay content from the stored preview.
func (m *RootModel) ctfPreviewState() *pages.CtfPreview {
	s := m.ctfSummary
	sep := m.themeOrNil().Separator()
	out := m.ctfParams.OutPath
	if strings.TrimSpace(out) == "" {
		out = s.OutputPath
	}

	return &pages.CtfPreview{
		Headline: []string{
			m.shortSessionID(m.ctfSummaryID) + sep +
				strconv.Itoa(s.Records) + " records" + sep +
				strconv.Itoa(s.MonetaryTransactions) + " monetary tx" + sep +
				"$ " + formatMoney(s.DestinationAmountSum) + " total",
		},
		Records:   s.RecordLines,
		OutPath:   out,
		Overwrite: m.ctfPreviewOverw,
	}
}

// formatMoney renders raw minor units as "12,450.00" (grouped integer
// part, sign preserved); the §K SUMMARY and overlay totals.
func formatMoney(raw int64) string {
	neg := raw < 0
	if neg {
		raw = -raw
	}
	whole, cents := raw/100, raw%100
	digits := strconv.FormatInt(whole, 10)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	sign := ""
	if neg {
		sign = "-"
	}

	return sign + b.String() + "." + padCents(cents)
}

// padCents zero-pads a 0..99 cent value.
func padCents(c int64) string {
	if c < 10 {
		return "0" + strconv.FormatInt(c, 10)
	}

	return strconv.FormatInt(c, 10)
}
