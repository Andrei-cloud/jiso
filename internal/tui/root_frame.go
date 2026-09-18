package tui

import (
	"path/filepath"

	"jiso/internal/config"
	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/version"
)

// frameProps assembles the frame inputs from live router state: the
// top-rule chips and the footer hint list (page legend, always-visible
// trio, then the current page's context keys). A nil app degrades to offline.
func (m *RootModel) frameProps(content string) frame.Props {
	th := m.theme
	if th == nil {
		th = theme.Default()
	}

	props := frame.Props{
		Theme:   th,
		Width:   m.width,
		Height:  m.height,
		Version: version.Version,
		// Every connection state carries symbol+word.
		Conn:    frame.Segment{Kind: theme.KindError, Text: "offline"},
		Content: content,
	}
	// Footer order: page legend, the always-visible trio, then the
	// page's context keys (dropped first under width pressure).
	props.Hints = m.footerHints()

	if m.app != nil {
		m.applyAppFrameProps(&props)
	}
	// Bridge-carried connection events are the freshest truth and win over
	// the app snapshot (they arrive after the last state change).
	if m.conn != nil {
		props.Conn = connSegment(*m.conn)
	}

	return props
}

// footerHints is the footer strip's full entry list — the packer's INPUT:
// page legend, always-visible trio, then the current page's context keys.
// frameProps renders and buildHitMap packs exactly this list, so the
// cells the user sees and the cells that fire actions can never drift.
//
// One hotkey surface per state: a pending confirm swaps its badged
// decision keys into the strip (the box only asks); the form dialogs,
// wizards, and picker list their keys in-body, and the §M box documents
// every key — while any of them is open the strip keeps only the global
// group, never the frozen page's keys.
func (m *RootModel) footerHints() []frame.KeyHint {
	hints := globalFooterHints(&m.keys)
	if m.errModal != nil {
		// While the error screen is open it owns the strip: the global
		// legend plus the screen's own keys, page hints suppressed.
		return append(hints, m.errModal.footerHints()...)
	}
	if c := m.pendingConfirm(); c != nil {
		// The confirm's question is the box; the decision keys are the
		// footer's — badged by the frame's Theme.Key path.
		for _, dk := range c.DecisionKeys() {
			hints = append(hints, frame.KeyHint{Key: dk.Key, Desc: dk.Desc, Primary: true})
		}

		return hints
	}
	if m.dlg != nil || m.wizard != nil || m.serverDlg != nil ||
		m.workerWiz != nil || m.filePick != nil || m.help != nil {
		// These overlays carry their own badged hint line in-body (the
		// §M box is the keymap itself): the global legend alone keeps
		// the strip honest without duplicating their keys.
		return hints
	}

	return append(hints, m.Current().Hints()...)
}

// applyAppFrameProps fills the frame props derived from the live app: config
// spec/header/file/target, worker count and the connection chip.
func (m *RootModel) applyAppFrameProps(props *frame.Props) {
	if cfg := m.app.Config(); cfg != nil {
		m.applyConfigFrameProps(props, cfg)
	}
	props.Workers = len(m.app.Workers())
	if m.app.IsConnected() {
		props.Conn = frame.Segment{Kind: theme.KindOK, Text: "connected"}
	}
}

// applyConfigFrameProps fills the spec/header/txfile/target chips from the config.
func (m *RootModel) applyConfigFrameProps(props *frame.Props, cfg *config.Config) {
	if spec := cfg.GetSpec(); spec != "" {
		props.Spec = filepath.Base(spec)
	}
	// The chip shows the EFFECTIVE header framing
	// (the live link's framing once known, the fallback before).
	props.Header = m.effectiveHeader()
	if file := cfg.GetFile(); file != "" {
		props.TxFile = filepath.Base(file)
		if repo := m.app.Transactions(); repo != nil {
			props.TxCount = len(repo.ListNames())
		}
	}
	if host := cfg.GetHost(); host != "" {
		props.Target = host + ":" + cfg.GetPort()
	}
}
