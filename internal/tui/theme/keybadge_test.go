package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// Key must render exactly the bold-accent HotKey style (not plain Accent),
// so every key glyph looks highlighted identically.
func TestThemeKeyMatchesHotKeyStyle(t *testing.T) {
	th := NewWith(colorprofile.TrueColor, true)
	got := th.Key("f")
	// Key must equal the bold-accent HotKey rendering (not plain Accent).
	if want := th.HotKey.Render("f"); got != want {
		t.Fatalf("Key(f) = %q, want HotKey render %q", got, want)
	}
	// Bold ships in the same SGR as the foreground (ESC[1;…m).
	if !strings.Contains(got, "\x1b[1;") {
		t.Fatalf("Key(f) must carry the bold SGR, got %q", got)
	}
	// Styling never alters the plain text.
	if stripped := stripSGR(got); stripped != "f" {
		t.Errorf("stripped Key(f) = %q, want %q", stripped, "f")
	}
}

// Under the ASCII profile Key is an identity style: token unchanged, no
// escapes (keeps *ascii*.golden 7-bit).
func TestThemeKeyASCIIIsIdentity(t *testing.T) {
	th := NewWith(colorprofile.ASCII, true)
	got := th.Key("f")
	if got != "f" {
		t.Fatalf("ASCII Key(f) = %q, want identity %q", got, "f")
	}
	// Same zero-escape check shape as TestNoColorYieldsZeroEscapes.
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\x9b") {
		t.Errorf("ASCII Key(f) contains escape codes: %q", got)
	}
}
