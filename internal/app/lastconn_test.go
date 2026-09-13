package app

import "testing"

// TestLastConnectionRoundTrip: save then load returns the same details;
// a fresh state dir reports "nothing remembered" (nil, nil).
func TestLastConnectionRoundTrip(t *testing.T) {
	t.Setenv(ServeStateDirEnv, t.TempDir())

	lc, err := LoadLastConnection()
	if err != nil || lc != nil {
		t.Fatalf("fresh dir: %+v, %v; want nil, nil", lc, err)
	}
	want := LastConnection{Host: "10.0.0.9", Port: "9999", Header: "binary2"}
	if err := SaveLastConnection(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadLastConnection()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("load = %+v, want %+v", got, want)
	}
}
