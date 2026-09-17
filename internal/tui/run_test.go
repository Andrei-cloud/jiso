package tui

import (
	"testing"

	app "jiso/internal/app"
)

// TestSilenceServiceDebug pins: the TUI session must run
// with the service debug side channel off, because its stderr hex
// dumps corrupt the alt-screen frames (§D panes rendered raw dumps).
func TestSilenceServiceDebug(t *testing.T) {
	a := newTxFileApp(t)
	a.Service().SetDebugMode(true)
	silenceServiceDebug(a)
	if a.Service().GetDebugMode() {
		t.Fatal("service debug mode still on after silencing")
	}
	silenceServiceDebug(nil)
	silenceServiceDebug(&app.App{})
}
