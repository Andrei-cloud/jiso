package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jiso/internal/utils"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestLoadRoutesFileBareArray(t *testing.T) {
	p := writeTemp(t, "routes.json",
		`[{"name":"Echo","match_fields":{"0":"0800"},"response_mti":"0810"}]`)
	routes, err := LoadRoutesFile(p)
	if err != nil {
		t.Fatalf("LoadRoutesFile: %v", err)
	}
	if len(routes) != 1 || routes[0].Name != "Echo" {
		t.Fatalf("got %+v, want one route named Echo", routes)
	}
}

func TestLoadRoutesFileRejectsUnnamed(t *testing.T) {
	p := writeTemp(t, "routes.json", `[{"response_mti":"0810"}]`)
	if _, err := LoadRoutesFile(p); err == nil {
		t.Fatal("want error for a route with no name, got nil")
	}
}

// TestLoadRoutesFileErrorsNameThePath pins the loader contract every failure
// is a config-class error naming the path (exit-3 taxonomy, PAR-309).
func TestLoadRoutesFileErrorsNameThePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")
	if _, err := LoadRoutesFile(missing); err == nil {
		t.Fatal("want error for a missing routes file, got nil")
	} else if !strings.Contains(err.Error(), missing) {
		t.Fatalf("err = %v, want it to name %s", err, missing)
	}

	malformed := writeTemp(t, "bad.json", `{"name":"not-an-array"}`)
	if _, err := LoadRoutesFile(malformed); err == nil {
		t.Fatal("want error for a malformed routes file, got nil")
	} else if !strings.Contains(err.Error(), malformed) {
		t.Fatalf("err = %v, want it to name %s", err, malformed)
	}
}

// TestResolveRoutesPrecedence pins the hoisted precedence
// routesFile > tx-file mock_routes > none, including the silent tx fallback.
func TestResolveRoutesPrecedence(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	routesFile := writeTemp(t, "routes.json", `[
	  {"name": "flag-echo", "match_fields": {"0": "0800"}, "response_mti": "0810"}
	]`)
	txFile := writeTemp(t, "tx.json", `[
	  {"type": "transaction", "name": "Echo", "fields": {"0": "0800"}},
	  {"type": "mock_route", "name": "tx-echo", "match_fields": {"0": "0800"}, "response_mti": "0810"}
	]`)

	// routes-file beats the tx file's mock_routes and yields no repo.
	routes, repo, err := ResolveRoutes(routesFile, txFile, spec)
	if err != nil {
		t.Fatalf("ResolveRoutes: %v", err)
	}
	if len(routes) != 1 || routes[0].Name != "flag-echo" {
		t.Fatalf("routes = %+v, want the flag route", routes)
	}
	if repo != nil {
		t.Fatal("repo must be nil on the routes-file leg")
	}

	// tx-file leg serves its mock_routes and hands back the collection.
	routes, repo, err = ResolveRoutes("", txFile, spec)
	if err != nil {
		t.Fatalf("ResolveRoutes tx leg: %v", err)
	}
	if len(routes) != 1 || routes[0].Name != "tx-echo" || repo == nil {
		t.Fatalf("routes = %+v, repo = %v, want tx-echo and a repository", routes, repo)
	}

	// A loadable-but-routeless source pair yields no routes and never errors.
	if routes, repo, err = ResolveRoutes("", "", spec); err != nil || len(routes) != 0 || repo != nil {
		t.Fatalf("empty sources: routes = %+v, repo = %v, err = %v", routes, repo, err)
	}

	// The pre-PAR-309 silent fallback: an unloadable tx file contributes
	// zero routes and never errors.
	badTx := writeTemp(t, "bad-tx.json", "not json at all")
	if routes, repo, err = ResolveRoutes("", badTx, spec); err != nil || len(routes) != 0 || repo != nil {
		t.Fatalf("unloadable tx file: routes = %+v, repo = %v, err = %v", routes, repo, err)
	}
}
