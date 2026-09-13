package widgets

import (
	"reflect"
	"strings"
	"testing"

	key "charm.land/bubbles/v2/key"
)

// TestNavHelpTracksNavKeys is the §M drift pin for the shared navigation
// source: NavHelp must expose exactly one line per navKeys binding, in
// field order, with keys identical to the binding's own key strings. A
// binding added to (or removed from) navKeys without updating NavHelp — or
// a hand-edited help string — fails here.
func TestNavHelpTracksNavKeys(t *testing.T) {
	t.Parallel()

	nav := newNavKeys()
	lines := NavHelp()

	v := reflect.ValueOf(nav)
	typ := v.Type()

	if typ.NumField() != len(lines) {
		t.Fatalf("NavHelp has %d lines, navKeys has %d bindings", len(lines), typ.NumField())
	}

	for i := range typ.NumField() {
		if ft := typ.Field(i).Type; ft != reflect.TypeOf(key.Binding{}) {
			t.Fatalf("navKeys field %s: %s, want key.Binding", typ.Field(i).Name, ft)
		}

		bi := v.Field(i).Interface()
		b, ok := bi.(key.Binding)
		if !ok {
			t.Fatalf("navKeys field %s = %T, want key.Binding", typ.Field(i).Name, bi)
		}

		if want := strings.Join(b.Keys(), "/"); lines[i].Keys != want {
			t.Errorf("NavHelp line %d (%s): keys %q, want binding keys %q",
				i, typ.Field(i).Name, lines[i].Keys, want)
		}

		if lines[i].Note == "" {
			t.Errorf("NavHelp line %d (%s): empty note", i, typ.Field(i).Name)
		}
	}
}
