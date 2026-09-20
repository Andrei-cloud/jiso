// root_server_form_stamp_test.go pins the §G form's routes-file stamp: an
// extract stamped with the capture's own spec fills the start form's spec
// field, so the server starts in the dialect the extract's transactions
// speak (fatal UAT: a visa-stamped extract served on a flex spec - every
// request died in ASCII decoding while the operator watched a "matched"
// log and a silent client). An unstamped file leaves the remembered spec
// alone.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestServerFormRoutesPickAdoptsStampedSpec(t *testing.T) {
	t.Parallel()

	m := footerRootT(t)

	// The stamped spec must be a file that exists (a stale stamp on
	// another machine's path is not a dialect).
	specFile := filepath.Join(t.TempDir(), "dialect.json")
	if err := os.WriteFile(specFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("spec: %v", err)
	}

	routes := fmt.Sprintf(
		`[{"type":"transaction","name":"Tx 0100 DE3=000000 #1","spec":%q},{"type":"mock_route","name":"Mock Route #0001 0110 DE3=000000 RC=00"}]`,
		specFile)
	routesFile := filepath.Join(t.TempDir(), "extract.json")
	if err := os.WriteFile(routesFile, []byte(routes), 0o644); err != nil {
		t.Fatalf("routes: %v", err)
	}

	_, _ = m.openServerForm()
	if m.serverDlg == nil {
		t.Fatal("the start form did not open")
	}
	_, _ = m.pickServerFormField(serverFieldRoutes, routesFile)

	st := m.serverDlg.State()
	if f := st.Field(serverFieldRoutes); f == nil || f.Value != routesFile {
		t.Fatalf("the routes field must carry the extract, got %+v", f)
	}
	if f := st.Field(serverFieldSpecPath); f == nil || f.Value != specFile {
		t.Errorf("the extract's own stamped spec must fill the spec field, got %+v", f)
	}

	// An unstamped file leaves the (now-stamped) spec field untouched -
	// the stamp fills, it never clears.
	plain := filepath.Join(t.TempDir(), "plain.json")
	if err := os.WriteFile(plain, []byte(`[{"type":"mock_route","name":"R"}]`), 0o644); err != nil {
		t.Fatalf("plain: %v", err)
	}
	_, _ = m.pickServerFormField(serverFieldRoutes, plain)
	st = m.serverDlg.State()
	if f := st.Field(serverFieldSpecPath); f == nil || f.Value != specFile {
		t.Errorf("an unstamped routes file must not clear the spec field, got %+v", f)
	}
}
