package frame

import (
	"strings"
	"testing"

	"jiso/internal/tui/theme"
)

// TestHeaderChip pins: a configured header framing renders
// as an "hdr <type>" chip right after the connection chip.
func TestHeaderChip(t *testing.T) {
	t.Parallel()

	th := theme.Default()
	view := Render(Props{
		Theme: th, Width: 120, Height: 12, Version: "t",
		Target: "127.0.0.1:9999",
		Conn:   Segment{Kind: 0, Text: "connected"},
		Header: "binary2",
	})
	if !strings.Contains(view, "hdr binary2") {
		t.Fatalf("header chip missing\n%s", strings.SplitN(view, "\n", 2)[0])
	}
	first := strings.SplitN(view, "\n", 2)[0]
	if strings.Index(first, "hdr binary2") < strings.Index(first, "target") {
		t.Error("hdr chip should follow the target/conn chip")
	}

	// Unset header renders no chip.
	view = Render(Props{Theme: th, Width: 120, Height: 12, Version: "t"})
	if strings.Contains(view, "hdr") {
		t.Error("empty header must render no chip")
	}
}
