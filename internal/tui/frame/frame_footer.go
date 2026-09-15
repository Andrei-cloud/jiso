package frame

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// footerGap separates hint entries on the footer strip.
const footerGap = "  "

// hintEntry is one rendered footer entry plus its survival flag.
type hintEntry struct {
	text    string
	primary bool
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
// narrow terminal stranded the user with no hint that keys existed —
// the marker points at the ? help overlay as the discovery path).
func (p Props) footerLine(th *theme.Theme, lv Level, width int) []string {
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
		entries = append(entries, hintEntry{text: entry, primary: h.Primary})
	}
	if len(entries) == 0 {
		return []string{""}
	}

	return []string{fitHints(th, entries, width, dropped)}
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
func fitHints(th *theme.Theme, entries []hintEntry, width, baseDropped int) string {
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
				return truncate(marker, width)
			}

			return truncate(entries[0].text, width)
		}
		line, overflowed := packHints(entries, budget)
		if !overflowed {
			if marker != "" {
				if line == "" {
					line = marker
				} else {
					line += footerGap + marker
				}
			}

			return line
		}
		tail := -1
		for i := len(entries) - 1; i >= 0; i-- {
			if !entries[i].primary {
				tail = i

				break
			}
		}
		if tail < 0 {
			return truncate(line, width)
		}
		entries = removeHint(entries, tail)
		dropped++
	}
}

// packHints packs one attempt. overflowed reports that some entry did
// not fit; the packed prefix is returned either way.
func packHints(entries []hintEntry, width int) (line string, overflowed bool) {
	gapW := lipgloss.Width(footerGap)
	used := 0
	for _, e := range entries {
		ew := lipgloss.Width(e.text)
		need := ew
		if line != "" {
			need = gapW + ew
		}
		if used+need > width {
			if line == "" {
				// A lone oversized entry: truncate rather than loop.
				return truncate(e.text, width), false
			}

			return line, true
		}
		if line == "" {
			line = e.text
		} else {
			line += footerGap + e.text
		}
		used += need
	}

	return line, false
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
