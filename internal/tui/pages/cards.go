package pages

import (
	"strconv"
	"strings"
	"time"

	"jiso/internal/tui/theme"
)

// The §A card bodies. layout.go owns the grid (cards, the height fitter and the
// box/clip primitives); this file owns what each card says. The split is the
// seam the file itself already had: nothing here knows about columns, gaps or
// the fitter, and nothing here is called by the fitter's internals.
// --- card bodies ---------------------------------------------------------

// connBodyLines renders the link truth: the symbol+word status with the
// target, then the static link config folded into one line (header, TLS, up,
// retries). Unknown parts render the dash.
//
// With no target at all there is nothing to dash, and the old render was a bare
// "-" -- which told the operator neither the state nor what to do about it. The
// design contract says empty states name the next action, so the card now says
// "no link - c connects" (or "reconnects" once a link has been and went away),
// matching "no send yet - s sends" and "no stress run - t starts one".
func (d *Dashboard) connBodyLines() []string {
	c := d.state.Conn

	if c.Target == "" {
		verb := " connects"
		if c.Status == ConnOffline || c.Status == ConnFailed || c.Status == ConnReconnecting {
			verb = " reconnects"
		}

		action := d.muted("no link"+d.th.Separator()) + keyGlyph(d.th, hotkeyConnection) + d.muted(verb)
		if status := d.connStatusValue(c); status != "" {
			return []string{status, action}
		}

		return []string{action}
	}

	head := d.connStatusValue(c)
	line1 := d.value(c.Target)
	if head != "" {
		line1 = head + " " + line1
	}

	return []string{
		line1,
		d.muted(strings.Join([]string{
			dashIf(d.th, c.Header),
			"TLS " + dashIf(d.th, c.TLS),
			"up " + dashIf(d.th, shortUptime(c.Uptime)),
			"retries " + dashIf(d.th, formatInt(c.Retries)),
		}, d.th.Separator())),
	}
}

// connStatusValue renders the symbol+word status (never color alone).
func (d *Dashboard) connStatusValue(c ConnectionCard) string {
	word := c.Status.Label()
	if word == "" {
		return ""
	}
	if c.Role != "" {
		word += " (" + c.Role + ")"
	}
	switch c.Status {
	case ConnOnline:
		return d.th.Status(theme.KindOK, word)
	case ConnReconnecting:
		return d.th.Status(theme.KindWarn, word)
	case ConnFailed:
		return d.th.Status(theme.KindError, word)
	default:
		return d.th.Deemphasized.Render(word)
	}
}

// serverCardBodyLines renders the MOCK SERVER card: running → the live
// line plus the stats line; stopped → the wireframe's empty state that
// teaches the 4 hotkey.
func (d *Dashboard) serverCardBodyLines() []string {
	sc := d.state.Server
	if !sc.Running {
		dot := pickGlyph(d.th, glyphDotOff, asciiDotOff)

		return []string{
			// UAT round 5 honesty: the 4 key jumps to the server page;
			// the form starts there (Enter), not from the dashboard.
			d.muted(dot+" stopped · ") +
				keyGlyph(d.th, hotkeyMockServer) +
				d.muted(" opens the server page"),
		}
	}

	conns, served, matched, fallback, reqErr := "", "", "", "", ""
	if sc.StatsKnown {
		st := sc.Stats
		conns = formatCount(int64(st.LiveConns))
		served = formatCount(int64(st.Served))
		matched = dashIf(d.th, st.MatchPct)
		fallback = formatCount(int64(st.Fallback))
		reqErr = formatCount(int64(st.ReqErr))
	}
	up := sc.Uptime

	line1 := d.th.StatusOK.Render(pickGlyph(d.th, glyphDotOn, asciiDotOn)+" running") +
		d.th.TextPrimary.Render(" :"+dashIf(d.th, sc.Port)) +
		d.th.Deemphasized.Render(" ("+dashIf(d.th, sc.Header)+")") +
		d.deem(" · up "+shortDur(up)+" · conns "+dashIf(d.th, conns))
	line2 := d.deem("served " + dashIf(d.th, served) +
		" · matched " + dashIf(d.th, matched) +
		" · fallback " + dashIf(d.th, fallback) +
		" · req err " + dashIf(d.th, reqErr))

	return []string{line1, line2}
}

// sendCardBodyLines renders the LAST SEND card: time + tx, the MTI turn
// with the RC badge, the elapsed/validation line, and the reopen
// affordance as body copy (the actual Enter is the "View last send"
// quick-action row — no card-focus system). Empty state teaches the s.
func (d *Dashboard) sendCardBodyLines() []string {
	s := d.state.LastSend
	if s == nil {
		return []string{
			d.muted("no send yet · ") +
				keyGlyph(d.th, hotkeyLastSend) +
				d.muted(" sends"),
		}
	}

	rc := "RC " + dashIf(d.th, s.RC)
	if s.RCNote != "" {
		rc += " " + s.RCNote
	}
	arrow := pickGlyph(d.th, serverArrow, asciiArrow)
	line2 := d.value(s.ReqMTI) + " " +
		d.th.Deemphasized.Render(plainDecor(d.th, arrow)) + " " +
		d.value(s.RespMTI) + d.deem(d.th.Separator()+rc)
	validated := d.th.Status(theme.KindError, "failed")
	if s.Validated {
		validated = d.th.Status(theme.KindOK, "validated")
	}
	correlation := d.th.Status(theme.KindError, "correlation")
	if s.Correlation {
		correlation = d.th.Status(theme.KindOK, "correlation")
	}
	line3 := d.value(formatElapsed(s.Elapsed)) + d.sep() + validated + d.sep() + correlation

	return []string{
		d.th.Dim.Render(dashIf(d.th, s.Time)) + " " + d.value(s.TxName),
		line2,
		line3,
		keyGlyph(d.th, "enter") + d.muted(" open · ") +
			keyGlyph(d.th, "h") + d.muted(" hexdump"),
	}
}

// stressCardBodyLines renders the LAST STRESS card (the most recent
// completed stress run, root-derived) and the summary-reopen affordance
// as body copy; empty state teaches the t.
func (d *Dashboard) stressCardBodyLines() []string {
	s := d.state.LastStress
	if s == nil {
		return []string{
			d.muted("no stress run · ") +
				keyGlyph(d.th, "t") +
				d.muted(" starts one"),
		}
	}

	done := d.th.Status(theme.KindWarn, "stopped")
	if s.Done {
		done = d.th.Status(theme.KindOK, "done")
	}

	return []string{
		d.th.Dim.Render(dashIf(d.th, s.Time)) + " " + d.value(s.ID) + " " +
			done + d.deem(d.th.Separator()+dashIf(d.th, s.OkPct)+" ok"),
		d.deem(dashIf(d.th, s.Workers) + d.th.Separator() + dashIf(d.th, s.TPS) +
			d.th.Separator() + "p99 " + dashIf(d.th, s.P99)),
		keyGlyph(d.th, "enter") + d.muted(" summary"),
	}
}

// logBodyLines compacts the root-stamped raw ring copy with the §G
// page's renderer (oldest first; the card renders the tail, newest at
// the bottom). Unrecognized lines pass through verbatim — the compact
// form is an optimization, never a filter.
func (d *Dashboard) logBodyLines() []string {
	if len(d.state.ServerLog) == 0 {
		return []string{d.th.TextMuted.Render("no server output yet")}
	}
	lines := make([]string, 0, len(d.state.ServerLog))
	for _, raw := range d.state.ServerLog {
		lines = append(lines, d.th.TextMuted.Render(CompactServerLog(d.th, raw)))
	}

	return lines
}

// sessionBodyLines renders the SESSION card: the live session id and
// the async App.SessionStats counters (dash until the first snapshot
// lands — unknown ≠ zero), then the average response and the db path.
func (d *Dashboard) sessionBodyLines() []string {
	s := d.state.Session
	id, db := "", ""
	tx, ok, fail, avg := "", "", "", ""
	if s != nil {
		id, db = s.ID, s.DBPath
		if s.Known {
			tx = strconv.Itoa(s.TxSent)
			ok = strconv.Itoa(s.OK)
			fail = strconv.Itoa(s.Fail)
			avg = strconv.FormatFloat(float64(s.AvgResponse)/float64(time.Millisecond), 'f', 1, 64) + "ms"
		}
	}

	return []string{
		d.kv("ID", id) + d.sep() + d.kv("tx", tx) + d.sep() + d.kv("ok", ok) + d.sep() + d.kv("fail", fail),
		d.kv("avg", avg) + d.sep() + d.kv("db", db),
	}
}

// actionsBodyLines renders the quick-actions list sized into the card;
// the widget owns its empty state and selection marker.
func (d *Dashboard) actionsBodyLines(w, kept int) []string {
	d.actions.SetSize(max(w-4, 2), max(kept, 1))

	return strings.Split(d.actions.View(), "\n")
}
