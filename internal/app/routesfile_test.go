package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jiso/internal/config"
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

// A type-less entry that carries match fields is route-shaped, so the
// unnamed rule reaches it too.
func TestLoadRoutesFileRejectsUnnamedMatchFieldsRoute(t *testing.T) {
	p := writeTemp(t, "routes.json", `[{"name":"Echo","match_fields":{"0":"0800"}},{"match_fields":{"0":"0200"}}]`)
	if _, err := LoadRoutesFile(p); err == nil {
		t.Fatal("want error for a match_fields route with no name, got nil")
	} else if !strings.Contains(err.Error(), p) {
		t.Fatalf("err = %v, want it to name %s", err, p)
	}
}

// combinedSaveFile is a config file in the shape SaveItems writes
// (transaction + dataset + scenario + mock_route). Pointing the routes
// file at one must load the route and nothing else.
const combinedSaveFile = `[
  {
    "type": "transaction",
    "name": "Echo",
    "spec": "specs/flex.json",
    "fields": {"0": "0800"}
  },
  {
    "type": "dataset",
    "name": "card_pool",
    "data": [{"2": "4111111111111111"}]
  },
  {
    "type": "scenario",
    "name": "E2E Purchase and Reversal",
    "steps": [{"name": "Purchase", "transaction": "Echo"}]
  },
  {
    "type": "mock_route",
    "name": "Echo Response",
    "description": "answer every echo",
    "match_fields": {"0": "0800", "3": "000000"},
    "required_fields": ["0", "3"],
    "echo_fields": [0, 3],
    "response_mti": "0810",
    "response_fields": {"39": "00"},
    "delay_ms": 40,
    "jitter_ms": 10,
    "drop_connection": true
  }
]`

func TestLoadRoutesFileCombinedKeepsOnlyRoutes(t *testing.T) {
	p := writeTemp(t, "config.json", combinedSaveFile)

	routes, err := LoadRoutesFile(p)
	if err != nil {
		t.Fatalf("LoadRoutesFile: %v", err)
	}

	want := config.MockRouteConfig{
		Name: "Echo Response", Description: "answer every echo",
		MatchFields:    map[string]any{"0": "0800", "3": "000000"},
		RequiredFields: []string{"0", "3"}, EchoFields: []int{0, 3},
		ResponseMTI: "0810", ResponseFields: map[string]any{"39": "00"},
		DelayMs: 40, JitterMs: 10, DropConnection: true,
	}
	if len(routes) != 1 {
		t.Fatalf("got %+v, want only the mock_route entry", routes)
	}
	if !reflect.DeepEqual(want, routes[0]) {
		t.Errorf("route mapping lost or changed data:\n got %+v\nwant %+v", routes[0], want)
	}
}

// A route-shaped entry needs a type or a criterion: match fields alone (no
// response_mti) and response_mti alone both load, so a hand-written
// routes-only file is never rejected for the keys the tx file does not use.
func TestLoadRoutesFileTypelessRouteShapes(t *testing.T) {
	p := writeTemp(t, "routes.json",
		`[{"name":"match only","match_fields":{"3":"000000"}},
		  {"name":"mti only","response_mti":"0810"}]`)

	routes, err := LoadRoutesFile(p)
	if err != nil {
		t.Fatalf("LoadRoutesFile: %v", err)
	}
	if len(routes) != 2 || routes[0].Name != "match only" || routes[1].Name != "mti only" {
		t.Fatalf("got %+v, want both route-shaped entries", routes)
	}
}

// A file of nothing but foreign entries is a hard error naming the file and
// every skipped entry, never a silent zero-route start.
func TestLoadRoutesFileAllForeignNamesPathAndSkips(t *testing.T) {
	const foreign = `[
	  {"type": "transaction", "name": "Echo", "fields": {"0": "0800"}},
	  {"type": "dataset", "name": "card_pool", "data": [{"2": "4111111111111111"}]},
	  {"type": "scenario", "name": "Purchase", "steps": []},
	  {"name": "loose", "description": "no criterion at all"},
	  {"type": "dataset", "data": [{"2": "4111111111111111"}]}
	]`
	p := writeTemp(t, "config.json", foreign)

	_, err := LoadRoutesFile(p)
	if err == nil {
		t.Fatal("want an error for a routes file with no route entries, got nil")
	}
	for _, want := range []string{
		p, "5 entries", "none are mock routes",
		"Echo:transaction", "card_pool:dataset", "Purchase:scenario",
		"loose:<no type>", "<unnamed>:dataset",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

// The skipped list is capped so a whole config file does not turn the error
// into a paragraph; the cap marker says there is more.
func TestLoadRoutesFileSkipListCapped(t *testing.T) {
	var items []string
	for i := range 8 {
		items = append(items, fmt.Sprintf(`{"type":"transaction","name":"Tx%d","fields":{}}`, i))
	}
	p := writeTemp(t, "config.json", "["+strings.Join(items, ",")+"]")

	_, err := LoadRoutesFile(p)
	if err == nil {
		t.Fatal("want an error for an all-foreign routes file, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"Tx0:transaction", "Tx4:transaction", "+3 more"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err = %v, want it to contain %q", msg, want)
		}
	}
	for _, hidden := range []string{"Tx5:transaction", "Tx7:transaction"} {
		if strings.Contains(msg, hidden) {
			t.Errorf("err = %v must cap the skipped list, shows %q", msg, hidden)
		}
	}
}

// An explicit empty array is "no routes on purpose" (the precedence table's
// documented override), so it stays loadable; the all-foreign error is
// about entries that are not routes.
func TestLoadRoutesFileEmptyArrayIsNoRoutes(t *testing.T) {
	p := writeTemp(t, "routes.json", `[]`)

	routes, err := LoadRoutesFile(p)
	if err != nil {
		t.Fatalf("LoadRoutesFile on an empty array: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("got %+v, want no routes", routes)
	}
}

// A file that parses as JSON but is not an array keeps the malformed-class
// error naming the path.
func TestLoadRoutesFileObjectShapeStillMalformed(t *testing.T) {
	p := writeTemp(t, "routes.json", `{"name":"Echo","response_mti":"0810"}`)
	if _, err := LoadRoutesFile(p); err == nil {
		t.Fatal("want error for a routes file that is an object, got nil")
	} else if !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("err = %v, want the malformed class", err)
	}
}

// Every load failure is a config-class error naming the path.
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

// Precedence: routesFile > tx-file mock_routes > none.
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

	// An unloadable tx file contributes zero routes and never errors.
	badTx := writeTemp(t, "bad-tx.json", "not json at all")
	if routes, repo, err = ResolveRoutes("", badTx, spec); err != nil || len(routes) != 0 || repo != nil {
		t.Fatalf("unloadable tx file: routes = %+v, repo = %v, err = %v", routes, repo, err)
	}
}
