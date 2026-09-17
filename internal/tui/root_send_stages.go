// root_send_stages.go holds the §D live-op seam and its production
// implementation, plus the parse stage: request/response row building,
// correlation notes, and the RC badge mapping — all computed in root so
// the §D page just renders.
package tui

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"

	app "jiso/internal/app"
	iconn "jiso/internal/connection"
	"jiso/internal/tui/pages"
	"jiso/internal/utils"
)

// liveExchange is the raw result of the network leg: the composed request
// (always set once composition succeeded), whether the write was
// dispatched (Wrote → the Send stage lights even on a later timeout), and
// the response (nil on timeout/transport failure). Tests inject a fake leg
// returning canned exchanges.
type liveExchange struct {
	Request  *iso8583.Message
	Response *iso8583.Message
	Wrote    bool
	Elapsed  time.Duration
}

// parsedExchange is the parse stage output: pre-derived pane rows with
// correlation notes, the RC badge values, and the STAN correlation verdict.
type parsedExchange struct {
	request, response []pages.ExchangeRow
	requestHex        []string // standard hexdump of the packed request
	responseHex       []string // standard hexdump of the packed response
	rc, rcLabel       string
	rcOK              bool
	correlationOK     bool
}

// messageHexDump packs msg and renders the standard hexdump (offset,
// 16 byte pairs, ASCII gutter) the h toggle shows for whole panes.
// A pack failure honestly yields no lines.
func messageHexDump(msg *iso8583.Message) []string {
	if msg == nil {
		return nil
	}
	packed, err := msg.Pack()
	if err != nil {
		return nil
	}

	return utils.StandardHexDump(packed)
}

// appConnect is the production Connect stage: App.Connect only when the
// service is not already connected (the §A card stays the connect truth).
func (m *RootModel) appConnect(_ context.Context) error {
	if m.app.IsConnected() {
		return nil
	}

	return m.app.Connect()
}

// appSend is the production Send/Receive leg, composed from the same app
// primitives App.Send runs (Compose → app.ValidateMessage gate → Pack →
// service async send), because App.Send itself takes no context and hides
// the request message the §D request pane must show. The ctx carries the
// config response-timeout budget — the same source App.New passes to the
// service and the CLI send --wait blocks on — so a stalled response
// returns context.DeadlineExceeded (also mapping the service's own
// nil-response timeout to it, honestly). Repo/DB logging mirrors
// App.Send's side effects so the TUI send counts like the CLI send.
func (m *RootModel) appSend(ctx context.Context, txName string) (*liveExchange, error) {
	tc := m.app.Transactions()

	msg, err := tc.Compose(txName)
	if err != nil {
		return nil, err
	}
	if err := app.ValidateMessage(msg); err != nil {
		return nil, fmt.Errorf("message validation failed: %w", err)
	}
	if _, err := msg.Pack(); err != nil {
		return nil, err
	}

	respCh, err := m.app.Service().SendAsync(msg, txName)
	if err != nil {
		tc.LogTransaction(txName, false)

		return nil, err
	}

	ex := &liveExchange{Request: msg, Wrote: true}
	start := time.Now()

	var resp *iso8583.Message

	select {
	case <-ctx.Done():
		err = ctx.Err()
	case resp = <-respCh:
		ex.Elapsed = time.Since(start)
		if resp == nil {
			err = fmt.Errorf("response timeout for transaction %s: %w", txName, context.DeadlineExceeded)
		}
	}

	success := err == nil
	tc.LogTransaction(txName, success)
	app.LogTransactionToDB(m.app.Config().GetSessionID(), txName, msg, resp,
		int(ex.Elapsed.Milliseconds()), success)

	if err != nil {
		return ex, err
	}
	ex.Response = resp

	return ex, nil
}

// exchangeRows renders one message through utils.Describe — the same
// human-readable description "message describe" prints (spec header,
// MTI, Bitmap HEX, annotated bitmap bits, then one F<n> description /
// value line per set field) — and mirrors every Describe line into a
// pane row. Sensitive values arrive already masked by Describe's
// DefaultFilters, and Hex is derived from the MASKED text, so the
// pure-display `h` toggle never leaks a raw value.
func exchangeRows(msg *iso8583.Message) ([]pages.ExchangeRow, error) {
	var buf bytes.Buffer
	if err := utils.Describe(msg, &buf); err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	rows := make([]pages.ExchangeRow, 0, len(lines))
	for _, ln := range lines {
		r := pages.ExchangeRow{Text: ln}
		if n, value, ok := describeFieldLine(ln); ok {
			r.Num = strconv.Itoa(n)
			r.Display = value
			r.Hex = hex.EncodeToString([]byte(value))
		}
		rows = append(rows, r)
	}

	return rows, nil
}

// describeFieldRE matches a Describe field line at zero indent
// ("F7   Transmission Date & Time.............: 0910184709"); indented
// nested composite lines are subfields, not message fields.
var describeFieldRE = regexp.MustCompile(`^F(\d+)\s.*?: (.*)$`)

// describeFieldLine splits a Describe field line into number and value.
func describeFieldLine(line string) (int, string, bool) {
	m := describeFieldRE.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, "", false
	}

	return n, m[2], true
}

// fieldName is the spec description of a field ("" when unspecced).
func fieldName(f field.Field) string {
	if spec := f.Spec(); spec != nil {
		return spec.Description
	}

	return ""
}

// parseExchange builds both panes, the correlation notes, and the RC
// badge. It fails only when a message carries a field whose value cannot
// be described (honest: nothing else in a post-unpack message can fail).
func parseExchange(ex *liveExchange) (*parsedExchange, error) {
	if ex.Request == nil {
		return nil, errors.New("request message missing")
	}
	if ex.Response == nil {
		return nil, errors.New("response message missing")
	}

	out := &parsedExchange{}

	var err error
	if out.request, err = exchangeRows(ex.Request); err != nil {
		return nil, fmt.Errorf("describe request: %w", err)
	}
	if out.response, err = exchangeRows(ex.Response); err != nil {
		return nil, fmt.Errorf("describe response: %w", err)
	}

	out.requestHex = messageHexDump(ex.Request)
	out.responseHex = messageHexDump(ex.Response)

	out.correlationOK = annotateCorrelation(ex, out.response)

	out.rc = strings.TrimSpace(fieldString(ex.Response, 39))
	out.rcLabel, out.rcOK = rcBadge(out.rc)

	return out, nil
}

// annotateCorrelation compares request/response STAN (field 11, via the
// shared iconn.NormalizeStan) and every other field both messages carry
// (echo check). It writes the per-row notes and returns the correlation
// verdict (both STANs present and equal). Field 0 (MTI legitimately
// differs) and field 39 (the RC badge owns it) stay unannotated; field 38
// gets the "auth code" info note when nothing else claimed it.
func annotateCorrelation(ex *liveExchange, rows []pages.ExchangeRow) bool {
	reqVals := fieldValueMap(ex.Request)
	respVals := fieldValueMap(ex.Response)

	reqSTAN := iconn.NormalizeStan(strings.TrimSpace(fieldString(ex.Request, 11)))
	respSTAN := iconn.NormalizeStan(strings.TrimSpace(fieldString(ex.Response, 11)))
	stanMatch := reqSTAN != "" && reqSTAN == respSTAN

	for i := range rows {
		r := &rows[i]
		n, err := strconv.Atoi(r.Num)
		if err != nil {
			continue
		}
		switch n {
		case 11:
			switch {
			case respVals[11] == "":
			case stanMatch:
				r.Note, r.NoteKind = "STAN", pages.NotePass
			default:
				r.Note, r.NoteKind = "STAN", pages.NoteFail
			}
		case 0, 39:
			// MTI and RC carry their own chrome (pane titles / badge).
		default:
			rv, ok := reqVals[n]
			if ok {
				if rv == respVals[n] {
					r.Note, r.NoteKind = "echo", pages.NotePass
				} else {
					r.Note, r.NoteKind = "echo", pages.NoteFail
				}
			} else if n == 38 {
				r.Note, r.NoteKind = "auth code", pages.NoteInfo
			}
		}
	}

	return stanMatch
}

// fieldValueMap indexes a message's field values by number (raw, unmasked:
// correlation is a root-side check; masked values only ever ship in rows).
func fieldValueMap(msg *iso8583.Message) map[int]string {
	out := map[int]string{}
	for n, f := range msg.GetFields() {
		v, err := f.String()
		if err != nil {
			continue
		}
		out[n] = v
	}

	return out
}

// fieldString is the raw value of field n ("" when absent/unreadable).
func fieldString(msg *iso8583.Message, n int) string {
	f := msg.GetField(n)
	if f == nil {
		return ""
	}
	v, err := f.String()
	if err != nil {
		return ""
	}

	return v
}

// rcBadge maps a response code to the §D badge label. The CLI send path
// describes RCs by code only — no label source exists in internal/app or
// internal/command (verified by grep) — so §D maps the two
// codes and renders every other code code-only: 00 → APPROVED (ok),
// 96 → DECLINED (error), else no label and no ok/error emphasis.
func rcBadge(rc string) (label string, ok bool) {
	switch rc {
	case "00":
		return "APPROVED", true
	case "96":
		return "DECLINED", false
	default:
		return "", false
	}
}
