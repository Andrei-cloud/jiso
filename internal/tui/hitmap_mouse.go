// hitmap_mouse.go is the arming leg of the hit map (split out of hitmap.go
// for the repohealth line budget): View hands the freshly built map to
// installMouse, which decides whether the frame talks to the terminal's
// mouse at all (the UAT round 9 F9 toggle) and wires the cell→action
// resolver. Registration itself lives in hitmap.go (buildHitMap); the
// footer/focus satellites add their own rects.
package tui

import (
	tea "charm.land/bubbletea/v2"
)

// installMouse arms the view for mouse input: CellMotion mode makes the
// terminal report clicks/wheel, and OnMouse — which bubbletea invokes with
// the mouse message against the LAST rendered view, on the event-loop
// goroutine — resolves the cell against the map View just built. The
// closure captures that map by value: stale hits cannot outlive their
// frame, and no RootModel field (or lock) is needed. Clicks replay their
// hit; the wheel becomes a scrollMsg for the hit's region; releases and
// motion stay inert so a click never double-fires. Task 8.2b's button
// policy narrows this further: only the LEFT button replays a hit and
// only the vertical wheel steps produce a scrollMsg — middle/right
// clicks and horizontal wheel steps stay inert, so they can neither
// replay key hits nor fake a vertical scroll.
//
// Wheel sign follows the CONTENT-DIRECTION convention shared with
// pages.Analyze.ScrollPreview (Task 7.3): wheel-DOWN is delta +1 (move the
// window down through the content), wheel-UP is -1 — so every scroll
// consumer (8.2–8.5) calls ScrollPreview(msg.delta)-style APIs directly
// with no negation.
func (m *RootModel) installMouse(out *tea.View, hm hitMap) {
	// UAT round 9 (F-9c): with the mouse toggled off (F9) the view must
	// not arm DECSET 1002/1006 at all — the terminal then handles
	// click-drag natively and text (test results, logs) is selectable
	// again. bubbletea v2.0.9 offers only None/CellMotion/AllMotion, so
	// None is the only release. Clearing OnMouse too keeps even a
	// hand-typed SGR report inert; buildHitMap independently stays empty.
	if !m.mouseEnabled {
		out.MouseMode = tea.MouseModeNone
		out.OnMouse = nil

		return
	}
	out.MouseMode = tea.MouseModeCellMotion
	out.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
		mm := msg.Mouse()
		act, ok := hm.resolve(mm.X, mm.Y)
		if !ok {
			return nil
		}
		if wheel, isWheel := msg.(tea.MouseWheelMsg); isWheel {
			var delta int
			switch wheel.Button {
			case tea.MouseWheelUp:
				delta = -1
			case tea.MouseWheelDown:
				delta = 1
			default:
				return nil // horizontal wheel steps scroll nothing vertical
			}
			if act.region == "" {
				return nil // no scroll region under the cursor
			}

			return func() tea.Msg { return scrollMsg{region: act.region, delta: delta} }
		}
		if _, isClick := msg.(tea.MouseClickMsg); !isClick {
			return nil // releases and motion replay nothing
		}
		if mm.Button != tea.MouseLeft {
			return nil // middle/right clicks replay no key hit (Task 8.2b policy)
		}

		return act.cmd()
	}
}
