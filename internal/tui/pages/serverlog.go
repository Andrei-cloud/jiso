// serverlog.go compacts the mock server's raw output lines into the
// one-row log entries the §G SERVER LOG pane and the dashboard card
// render. The raw line stays in the root ring; unrecognized lines pass
// through verbatim (the compact form is an optimization, never a filter).
package pages

import (
	"strings"

	"jiso/internal/tui/theme"
)

// Raw-line markers written by internal/server (and the root's receipt
// stamp). They live here as the single parse contract.
const (
	serverLineStamp = "[SERVER]"

	serverMarkOK   = "🟢"
	serverMarkWarn = "⚠️"
	serverMarkDrop = "🔴"
	serverMarkErr  = "❌"
	asciiMarkOK    = "ok"
	asciiMarkWarn  = "warn"
	asciiMarkDrop  = "drop"
	asciiMarkErr   = "err"
	serverArrow    = "→"
	asciiArrow     = "->"
	serverFallback = "Fallback"
)

// CompactServerLog renders one root-stamped raw line as the compact log
// entry. th's glyph mode picks the 7-bit fallbacks; the timestamp (if
// the root stamped one) is always kept.
func CompactServerLog(th *theme.Theme, raw string) string {
	ts, rest := splitReceiptStamp(raw)

	body := strings.TrimSpace(strings.TrimPrefix(rest, serverLineStamp))
	if body == rest && !strings.HasPrefix(rest, serverLineStamp) {
		return raw // not a server line at all: verbatim
	}

	compact, ok := compactServerBody(th, body)
	if !ok {
		return raw
	}

	if ts != "" {
		return ts + " " + compact
	}

	return compact
}

// splitReceiptStamp peels the root's "HH:MM:SS " receipt stamp.
func splitReceiptStamp(raw string) (ts, rest string) {
	if len(raw) > 9 && raw[2] == ':' && raw[5] == ':' && raw[8] == ' ' {
		return raw[:8], raw[9:]
	}

	return "", raw
}

// compactServerBody turns one "[SERVER] "-stripped line into the compact
// form; ok=false when the shape is unrecognized (caller keeps the raw
// line).
func compactServerBody(th *theme.Theme, body string) (string, bool) {
	sep := th.Separator()
	arrow := pickGlyph(th, serverArrow, asciiArrow)

	switch {
	case strings.HasPrefix(body, serverMarkOK+" Matched Route "):
		line, ok := parseResponding(body)
		if !ok {
			return "", false
		}
		mark := pickGlyph(th, serverMarkOK, asciiMarkOK)

		return mark + " " + line.name + sep + line.mti + arrow + line.resp + sep + "RC " + line.rc, true

	case strings.HasPrefix(body, serverMarkWarn+" Fallback"):
		line, ok := parseFallbackResponding(body)
		if !ok {
			return "", false
		}
		mark := pickGlyph(th, serverMarkWarn, asciiMarkWarn)

		return mark + " " + serverFallback + sep + line.mti + arrow + line.resp + sep + "RC " + line.rc, true

	case strings.HasPrefix(body, serverMarkDrop+" Matched Route "):
		line, ok := parseResponding(body)
		if !ok {
			return "", false
		}
		mark := pickGlyph(th, serverMarkDrop, asciiMarkDrop)

		return mark + " " + line.name + sep + line.mti + arrow + " drop_conn", true

	case strings.HasPrefix(body, serverMarkErr+" "):
		mark := pickGlyph(th, serverMarkErr, asciiMarkErr)

		return mark + " " + plainDecor(th, strings.TrimPrefix(body, serverMarkErr+" ")), true
	}

	return "", false
}

// respondingLine is one server-log "… for MTI X -> Responding Y (RC: Z)" line
// parsed into the pieces the compact view renders. name is empty for a fallback
// (no route matched).
type respondingLine struct {
	name string
	mti  string
	resp string
	rc   string
}

// parseResponding extracts (route, MTI, respMTI, RC) from
// "🟢 Matched Route 'Echo' for MTI 0800 -> Responding 0810 (RC: 00)"
// (or the Dropping-connection variant, where resp/rc come back empty).
func parseResponding(body string) (respondingLine, bool) {
	i := strings.IndexByte(body, '\'')
	if i < 0 {
		return respondingLine{}, false
	}
	j := strings.IndexByte(body[i+1:], '\'')
	if j < 0 {
		return respondingLine{}, false
	}
	line := respondingLine{name: body[i+1 : i+1+j]}
	rest := body[i+2+j:]

	const forMTI = " for MTI "
	k := strings.Index(rest, forMTI)
	if k != 0 {
		return respondingLine{}, false
	}
	rest = rest[len(forMTI):]

	if k := strings.Index(rest, " "); k >= 0 {
		line.mti = rest[:k]
		rest = rest[k+1:]
	} else {
		line.mti = rest
		rest = ""
	}

	if r := strings.Index(rest, "Responding "); r >= 0 {
		tail := rest[r+len("Responding "):]
		if k := strings.IndexByte(tail, ' '); k >= 0 {
			line.resp = tail[:k]
			line.rc = extractRC(tail)
		}
	}

	if line.name == "" || line.mti == "" {
		return respondingLine{}, false
	}

	return line, true
}

// extractRC pulls the "(RC: NN)" response-code value out of a responding tail,
// returning "" when the marker or its closing paren is absent.
func extractRC(tail string) string {
	a := strings.Index(tail, "(RC: ")
	if a < 0 {
		return ""
	}

	rest := tail[a+len("(RC: "):]
	if b := strings.IndexByte(rest, ')'); b >= 0 {
		return rest[:b]
	}

	return ""
}

// parseFallbackResponding extracts (MTI, respMTI, RC) from
// "⚠️ Fallback (No Route Match) for MTI 0200 -> Responding 0210 (RC: 12)".
func parseFallbackResponding(body string) (respondingLine, bool) {
	const forMTI = " for MTI "
	k := strings.Index(body, forMTI)
	if k < 0 {
		return respondingLine{}, false
	}
	var line respondingLine
	rest := body[k+len(forMTI):]
	if s := strings.IndexByte(rest, ' '); s >= 0 {
		line.mti = rest[:s]
		rest = rest[s+1:]
	} else {
		line.mti = rest
		rest = ""
	}

	if r := strings.Index(rest, "Responding "); r >= 0 {
		tail := rest[r+len("Responding "):]
		if s := strings.IndexByte(tail, ' '); s >= 0 {
			line.resp = tail[:s]
		} else {
			line.resp = tail
		}
	}
	if a := strings.Index(body, "(RC: "); a >= 0 {
		if b := strings.IndexByte(body[a+len("(RC: "):], ')'); b >= 0 {
			line.rc = body[a+len("(RC: ") : a+len("(RC: ")+b]
		}
	}

	if line.mti == "" {
		return respondingLine{}, false
	}

	return line, true
}
