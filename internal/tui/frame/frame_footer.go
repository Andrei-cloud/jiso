package frame

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// footerGap separates hint entries on the footer strip.
const footerGap = "  "

// hintEntry is one rendered footer entry plus its survival flag and the
// hint it was rendered from (Task 8.4: the packed rect's dispatch source).
type hintEntry struct {
	text    string
	primary bool
	hint    KeyHint
}

// hintSpan is one PACKED (visible) footer entry: the horizontal cell
// range [x0, x1) it occupies inside the footer content area, relative to
// the FooterOrigin x. Entries the width pressure dropped get no span, so
// only drawn hints are clickable.
type hintSpan struct {
	hint   KeyHint
	x0, x1 int
}

// footerLine renders the contextual keymap strip from the hint list the
// router passed in — never hardcoded here. At LevelNarrow only Primary
// hints are kept (contract: the always-visible discoverability layer
// survives to the narrowest column budget); at every level the router
// orders the list legend-first, so overflow drops page keys, then jump
// keys, and the primary trio never.
//
// Whatever the width pressure drops is COUNTED: the line ends with a
// dim "…+N" marker (UAT round 5: dropped entries were invisible, so a
// narrow terminal stranded the user with no hint that keys existed — the
// marker points at the ? help overlay as the discovery path).
func (p Props) footerLine(th *theme.Theme, lv Level, width int) []string {
	line, _ := p.footerSpans(th, lv, width)

	return []string{line}
}

// footerSpans is footerLine's pipeline with the packed entries' cell
// ranges attached — the exact code path Render draws the footer through,
// so the spans always describe the rendered bytes (the hit map is
// metadata; the footer goldens stay byte-identical).
func (p Props) footerSpans(th *theme.Theme, lv Level, width int) (string, []hintSpan) {
	hints := p.Hints
	dropped := 0
	if lv == LevelNarrow {
		primaries := primaryHints(hints)
		dropped = len(hints) - len(primaries)
		hints = primaries
	}

	entries := make([]hintEntry, 0, len(hints))
	for _, h := range hints {
		if h.Key == "" && h.Desc == "" {
			continue
		}
		entry := th.Key(h.Key)
		if h.Desc != "" {
			entry += " " + th.TextMuted.Render(h.Desc)
		}
		entries = append(entries, hintEntry{text: entry, primary: h.Primary, hint: h})
	}
	if len(entries) == 0 {
		return "", nil
	}

	return fitHints(th, entries, width, dropped)
}

// FooterHit is one VISIBLE footer entry in ABSOLUTE terminal cells: the
// horizontal rect [X, X+W) on the footer row that the entry's text
// occupies, plus the key spelling the router should synthesize when a
// click lands there.
type FooterHit struct {
	Dispatch string
	X, Y, W  int
}

// FooterHits replays the exact footer packing Render performs at the given
// terminal size and returns one rect per entry that actually made it into
// the rendered footer row. Entries hidden by the narrow level filter or
// the overflow drop (the "…+N" tail) get NO rect — only visible hints are
// clickable — and a frame that draws no footer row at all (FooterOrigin
// !ok) publishes nothing. Dispatch is the hint's Key spelling (the
// matching vocabulary of the bindings, theme/keys.go); spellings that name
// no key stay in the result so callers can decide their own inertness
// policy.
func FooterHits(th *theme.Theme, hints []KeyHint, width, height int) []FooterHit {
	if th == nil {
		th = theme.Default()
	}
	if width <= 0 {
		width = FallbackWidth
	}
	if height <= 0 {
		height = FallbackHeight
	}
	fx, fy, ok := FooterOrigin(width, height)
	if !ok {
		return nil
	}
	_, spans := Props{Hints: hints}.footerSpans(th, LevelFor(width), width-borderInset)

	out := make([]FooterHit, 0, len(spans))
	for _, s := range spans {
		out = append(out, FooterHit{Dispatch: s.hint.Key, X: fx + s.x0, Y: fy, W: s.x1 - s.x0})
	}

	return out
}

// overflowMarker is the dim "…+N" tail counting hidden entries; the
// ellipsis glyph keeps it 7-bit-safe under theme.ASCII.
func overflowMarker(th *theme.Theme, dropped int) string {
	return th.Deemphasized.Render(th.Ellipsis() + "+" + strconv.Itoa(dropped))
}

// fitHints packs hint entries into the width budget whole-entry-first.
// When the line would overflow, the LAST non-primary entry is dropped
// and packing retries, so primaries survive every width the frame
// renders at; only when the primaries alone still overflow is the
// result truncated (the lone oversized entry case). Dropped entries
// (baseDropped counts what the level filter already hid) are reported
// by the overflowMarker tail, whose width is reserved from the budget.
// The returned spans describe the returned line: entries removed here or
// clipped away by the final truncate get no (full) span.
func fitHints(th *theme.Theme, entries []hintEntry, width, baseDropped int) (string, []hintSpan) {
	dropped := baseDropped
	for {
		budget := width
		marker := ""
		if dropped > 0 {
			marker = overflowMarker(th, dropped)
			budget = width - lipgloss.Width(marker) - lipgloss.Width(footerGap)
		}
		if budget <= 0 {
			// The marker alone cannot share the line with any entry:
			// show just the marker (entries all hidden, all counted).
			if marker != "" {
				return truncate(marker, width), nil
			}

			return truncate(entries[0].text, width), clipSpan(hintSpan{hint: entries[0].hint, x0: 0, x1: lipgloss.Width(entries[0].text)}, width)
		}
		line, spans, overflowed := packHints(entries, budget)
		if !overflowed {
			if marker != "" {
				if line == "" {
					line = marker
				} else {
					line += footerGap + marker
				}
			}

			return line, spans
		}
		tail := -1
		for i := len(entries) - 1; i >= 0; i-- {
			if !entries[i].primary {
				tail = i

				break
			}
		}
		if tail < 0 {
			return truncate(line, width), clipSpans(spans, width)
		}
		entries = removeHint(entries, tail)
		dropped++
	}
}

// packHints packs one attempt, accumulating each accepted entry's cell
// range. overflowed reports that some entry did not fit; the packed
// prefix and its spans are returned either way.
func packHints(entries []hintEntry, width int) (line string, spans []hintSpan, overflowed bool) {
	gapW := lipgloss.Width(footerGap)
	used := 0
	for _, e := range entries {
		ew := lipgloss.Width(e.text)
		need := ew
		x := used
		if line != "" {
			need = gapW + ew
			x = used + gapW
		}
		if used+need > width {
			if line == "" {
				// A lone oversized entry: truncate rather than loop.
				return truncate(e.text, width), clipSpan(hintSpan{hint: e.hint, x0: 0, x1: ew}, width), false
			}

			return line, spans, true
		}
		if line == "" {
			line = e.text
		} else {
			line += footerGap + e.text
		}
		spans = append(spans, hintSpan{hint: e.hint, x0: x, x1: x + ew})
		used += need
	}

	return line, spans, false
}

// clipSpan clamps a packed span to the width the line is truncated at,
// dropping it entirely when the truncate removed the entry's ink.
func clipSpan(s hintSpan, width int) []hintSpan {
	if s.x0 >= width {
		return nil
	}

	return []hintSpan{{hint: s.hint, x0: s.x0, x1: min(s.x1, width)}}
}

// clipSpans clips every span, dropping the ones the truncate removed.
func clipSpans(spans []hintSpan, width int) []hintSpan {
	out := make([]hintSpan, 0, len(spans))
	for _, s := range spans {
		out = append(out, clipSpan(s, width)...)
	}

	return out
}

// removeHint returns entries without index i (copy, no aliasing).
func removeHint(entries []hintEntry, i int) []hintEntry {
	out := make([]hintEntry, 0, len(entries)-1)
	out = append(out, entries[:i]...)

	return append(out, entries[i+1:]...)
}

// primaryHints filters a hint list down to its primary entries.
func primaryHints(hints []KeyHint) []KeyHint {
	out := make([]KeyHint, 0, len(hints))
	for _, h := range hints {
		if h.Primary {
			out = append(out, h)
		}
	}

	return out
}
