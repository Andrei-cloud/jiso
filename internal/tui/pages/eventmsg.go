// eventmsg.go carries the page-side event envelope. Proposal 05 §3
// removed the §A EVENT FEED pane (its connect/disconnect content lives in
// the CONNECTION card, the timestamped status strip and the live SERVER
// LOG card), so the Feed ring and its severity taxonomy are gone with
// it; the EventMsg envelope stays the root→page delivery contract (root
// stamps Time with its injectable clock at delivery, so pages never call
// time.Now and goldens stay fake-clock-deterministic). Pages that do not
// care about an event simply ignore the message.
package pages

import (
	"time"

	"jiso/internal/app/events"
)

// EventMsg carries one internal/app bus event into a page.
type EventMsg struct {
	Event events.Event
	Time  time.Time
}

// GlyphInfo is the informational bullet (theme owns the ok/warn/error
// glyphs); ASCII fallback below. Design contract: every severity is
// symbol + text, never color alone.
const (
	GlyphInfo = "·"
	ASCIIInfo = "."
)
