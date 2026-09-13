package widgets

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

var toastT0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestToastStackCap3(t *testing.T) {
	t.Parallel()

	tr := NewToast(asciiTheme(t))
	for i := range 5 {
		tr.Push("msg", ToastInfo, toastT0.Add(time.Duration(i)*time.Second))
	}
	if tr.Len() != MaxToastStack {
		t.Fatalf("len = %d, want %d", tr.Len(), MaxToastStack)
	}
	// Oldest two dropped: the survivors are stamped at +2..+4s.
	want := []time.Time{toastT0.Add(2 * time.Second), toastT0.Add(3 * time.Second), toastT0.Add(4 * time.Second)}
	for i, e := range tr.items {
		if !e.at.Equal(want[i]) {
			t.Errorf("survivor %d = %v, want %v", i, e.at, want[i])
		}
	}
}

func TestToastPruneByInjectedAge(t *testing.T) {
	t.Parallel()

	tr := NewToast(tcTheme(t))
	tr.Push("old", ToastInfo, toastT0)
	tr.Push("new", ToastSuccess, toastT0.Add(2*time.Second))
	tr.Prune(toastT0.Add(3*time.Second), 2*time.Second) // age 3s >= ttl drops; age 1s stays
	if tr.Len() != 1 || !strings.Contains(tr.View(), "new") {
		t.Fatalf("prune kept %q", tr.View())
	}
	tr.Prune(toastT0.Add(5*time.Second), 2*time.Second)
	if tr.Len() != 0 || tr.View() != "" {
		t.Fatal("all toasts must expire")
	}
}

func TestToastKindTokens(t *testing.T) {
	t.Parallel()

	tc := tcTheme(t)
	tr := NewToast(tc)
	tr.Push("ok", ToastSuccess, toastT0)
	tr.Push("bad", ToastError, toastT0)
	tr.Push("fyi", ToastInfo, toastT0)
	lines := tr.Lines()
	if !strings.HasPrefix(ansi.Strip(lines[0]), theme.GlyphOK+" ") {
		t.Errorf("success = %q, want %q prefix", lines[0], theme.GlyphOK)
	}
	if !strings.HasPrefix(ansi.Strip(lines[1]), theme.GlyphError+" ") {
		t.Errorf("error = %q, want %q prefix", lines[1], theme.GlyphError)
	}
	if !strings.HasPrefix(ansi.Strip(lines[2]), GlyphToastInfo+" ") {
		t.Errorf("info = %q, want bullet prefix", lines[2])
	}
	// Kind -> token: success/error/info carry distinct truecolor fg codes.
	fgs := map[string]bool{}
	for _, l := range lines {
		fgs[fgCode(l)] = true
	}
	if len(fgs) != 3 || fgs[""] {
		t.Errorf("kinds must map to three distinct tokens: %v", fgs)
	}

	at := NewToast(asciiTheme(t))
	at.Push("ok", ToastSuccess, toastT0)
	at.Push("bad", ToastError, toastT0)
	at.Push("fyi", ToastInfo, toastT0)
	for i, want := range []string{theme.ASCIIOK, theme.ASCIIError, ASCIIToastInfo} {
		if !strings.HasPrefix(at.Lines()[i], want) {
			t.Errorf("ascii line %d = %q, want %q prefix", i, at.Lines()[i], want)
		}
	}
}

// fgCode extracts the TrueColor foreground of a styled line ("" when
// the line carries none).
func fgCode(s string) string {
	i := strings.Index(s, "38;2;")
	if i < 0 {
		return ""
	}
	code, _, _ := strings.Cut(s[i:], "m")

	return code
}

func TestToastEmptyRendersNothing(t *testing.T) {
	t.Parallel()

	tr := NewToast(tcTheme(t))
	tr.SetSize(40)
	if got := tr.View(); got != "" {
		t.Fatalf("empty = %q, want \"\"", got)
	}
	tr.Push("", ToastInfo, toastT0) // empty text is never pushed
	if tr.Len() != 0 {
		t.Fatal("empty text must not enter the stack")
	}
}

func TestToastRightAligned(t *testing.T) {
	t.Parallel()

	tr := NewToast(asciiTheme(t))
	tr.SetSize(24)
	tr.Push("ab", ToastInfo, toastT0)
	tr.Push("cdef", ToastSuccess, toastT0)
	ls := lines(tr.View())
	if len(ls) != 2 {
		t.Fatalf("lines = %d", len(ls))
	}
	if want := strings.Repeat(" ", 24-4) + "* ab"; ls[0] != want {
		t.Errorf("line 0 = %q, want %q", ls[0], want)
	}
	if want := strings.Repeat(" ", 24-9) + "[ok] cdef"; ls[1] != want {
		t.Errorf("line 1 = %q, want %q", ls[1], want)
	}
}
