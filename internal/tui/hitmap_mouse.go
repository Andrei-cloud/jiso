// hitmap_mouse.go is the arming leg of the hit map: View hands the freshly
// built map to installMouse, which decides whether the frame talks to the
// terminal's mouse and wires the cell→action resolver. Registration itself
// lives in hitmap.go (buildHitMap).
package tui

import (
	tea "charm.land/bubbletea/v2"
)

// installMouse arms the view: CellMotion makes the terminal report mouse
// events, and OnMouse resolves cells against the map View just built —
// captured by value, so stale hits cannot outlive their frame. Only the LEFT
// button replays a hit; only vertical wheel steps emit a scrollMsg, with
// wheel-DOWN as delta +1 (the content direction shared with
// pages.Analyze.ScrollPreview, no negation). Releases and motion stay inert.
func (m *RootModel) installMouse(out *tea.View, hm hitMap) {
	// mouse off (F9): never arm DECSET 1002/1006, so the terminal keeps
	// native text selection; clearing OnMouse keeps raw SGR reports inert.
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
