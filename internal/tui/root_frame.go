package tui

import (
	"path/filepath"

	"jiso/internal/config"
	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/version"
)

// frameProps assembles the TUI-403 frame inputs from live router state:
// the top-rule chip set (connection target+state, spec, tx file+count,
// worker count) and the footer hint list = wireframe page legend, the
// always-visible trio, then the current page's context keys. A nil app
// (tests, pre-wire) degrades the chips to the offline/no-spec truth.
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
		// Wireframe A3: every connection state carries symbol+word.
		Conn:    frame.Segment{Kind: theme.KindError, Text: "offline"},
		Content: content,
	}
	// Wireframe footer order: page legend, the always-visible
	// ": cmd ? help q quit" trio, then the page's context keys
	// (dropped first under width pressure, see frame.fitHints).
	props.Hints = m.footerHints()

	if m.app != nil {
		m.applyAppFrameProps(&props)
	}
	// Bridge-carried connection events are the freshest truth and win over
	// the app snapshot (they arrive after the last state change).
	if m.conn != nil {
		props.Conn = connSegment(*m.conn)
	}
	// UAT: the newest system output line (connection manager) renders in
	// the bottom console strip instead of corrupting the frame.
	if line, isErr := m.consoleLine(); line != "" {
		props.Console, props.ConsoleErr = line, isErr
	}

	return props
}

// footerHints is the footer strip's full entry list — the packer's INPUT
// before any width filtering: the wireframe page legend, the always-visible
// trio, then the current page's context keys. frameProps renders exactly
// this list and buildHitMap packs exactly this list into click rects, so
// the cells the user sees and the cells that fire actions can never drift.
func (m *RootModel) footerHints() []frame.KeyHint {
	return append(globalFooterHints(&m.keys), m.Current().Hints()...)
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
	// UAT: the connection chip shows the EFFECTIVE header
	// framing next to the target (the live link's framing
	// once known; the config value or the fallback before).
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
