package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// TestConnectPrefillLastConn: values the config leaves unset prefill
// from the remembered last-successful connect; explicit config values
// win over the memory (UAT: "make connection details as last used").
func TestConnectPrefillLastConn(t *testing.T) {
	t.Setenv("JISO_STATE_DIR", t.TempDir())
	if err := app.SaveLastConnection(app.LastConnection{Host: "10.1.2.3", Port: "45455", Header: "binary2"}); err != nil {
		t.Fatalf("seed last-connection: %v", err)
	}

	st := connectPrefill(nil, connectSpecFor(t, pages.ConnectFieldIP), nil, mustLastConn(t))
	if st != "10.1.2.3" {
		t.Fatalf("prefill host = %q, want remembered 10.1.2.3", st)
	}

	cfg := config.GetConfig()
	cfg.Reset()
	defer cfg.Reset()
	cfg.SetHost("127.0.0.1")
	if got := connectPrefill(nil, connectSpecFor(t, pages.ConnectFieldIP), cfg, mustLastConn(t)); got != "127.0.0.1" {
		t.Fatalf("config value must win over last-used, got %q", got)
	}
}

// TestConnectSuccessRemembersLastConn: a successful connect stamps the
// state-dir file, and a rebuilt form prefills from it in-session.
func TestConnectSuccessRemembersLastConn(t *testing.T) {
	t.Setenv("JISO_STATE_DIR", t.TempDir())
	r := newConnectTestRoot(t)
	r.m.dialConnect = func(_ context.Context, _ app.ConnectOptions) error { return nil }
	r.openHotkey(t)
	_, _ = r.m.Update(special(tea.KeyEnter))
	r.drainToResult(t)

	lc, err := app.LoadLastConnection()
	if err != nil || lc == nil {
		t.Fatalf("last-connection must be saved: %+v, %v", lc, err)
	}
	if lc.Host != "127.0.0.1" || lc.Port != "65535" {
		t.Fatalf("saved %+v, want 127.0.0.1:65535", *lc)
	}
	if ds := r.m.dashboardState(); ds.Conn.Header != "binary2" {
		t.Fatalf("dashboard card Header = %q, want the form-selected binary2", ds.Conn.Header)
	}
}

// TestEffectiveHeaderTracksLiveLink: the card and the top-rule chip show
// the framing the LIVE link actually uses (form selection wins), not
// the app fallback (UAT: form binary2, screen lied with ascii4).
func TestEffectiveHeaderTracksLiveLink(t *testing.T) {
	t.Setenv("JISO_STATE_DIR", t.TempDir())
	r := newConnectTestRoot(t)
	var dialed app.ConnectOptions
	r.m.dialConnect = func(_ context.Context, opts app.ConnectOptions) error {
		dialed = opts

		return nil
	}
	r.openHotkey(t)
	st := r.m.dlg.State()
	st.Field(pages.ConnectFieldHeader).Value = "bcd2"
	r.m.dlg.SetState(st)
	_, _ = r.m.Update(special(tea.KeyEnter))
	r.drainToResult(t)

	if dialed.LengthType != "bcd2" {
		t.Fatalf("dial LengthType = %q, want bcd2 (the form selection dials)", dialed.LengthType)
	}
	if ds := r.m.dashboardState(); ds.Conn.Header != "bcd2" {
		t.Errorf("dashboard card Header = %q, want bcd2", ds.Conn.Header)
	}
	if p := r.m.frameProps(""); p.Header != "bcd2" {
		t.Errorf("header chip = %q, want bcd2", p.Header)
	}

	// The last-live framing survives a disconnect: it describes the
	// link the operator last had until the next connect restamps it.
	r.m.conn = nil
	if ds := r.m.dashboardState(); ds.Conn.Header != "bcd2" {
		t.Errorf("after disconnect card Header = %q, want the last-live bcd2", ds.Conn.Header)
	}
}

// TestEffectiveHeaderFallsBack: without a live connect the display
// reads the configured header, and the app fallback only as the last
// resort.
func TestEffectiveHeaderFallsBack(t *testing.T) {
	t.Setenv("JISO_STATE_DIR", t.TempDir())
	r := newConnectTestRoot(t)
	if p := r.m.frameProps(""); p.Header != app.DefaultLengthType {
		t.Fatalf("header chip = %q, want the fallback %q", p.Header, app.DefaultLengthType)
	}
	cfg := r.m.app.Config()
	cfg.SetHeader("naps")
	if p := r.m.frameProps(""); p.Header != "naps" {
		t.Fatalf("header chip = %q, want the configured naps", p.Header)
	}
	cfg.SetHeader("")
}

func connectSpecFor(t *testing.T, key string) connectFieldSpec {
	t.Helper()
	for _, sp := range connectFieldSpecs() {
		if sp.key == key {
			return sp
		}
	}
	t.Fatalf("no connect field spec %q", key)

	return connectFieldSpec{}
}

func mustLastConn(t *testing.T) *app.LastConnection {
	t.Helper()
	lc, err := app.LoadLastConnection()
	if err != nil || lc == nil {
		t.Fatalf("load last-connection: %v", err)
	}

	return lc
}
