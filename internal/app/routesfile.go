// routesfile.go holds the mock-route loader that keeps route entries only,
// shared by the `serve start` --routes-file path and the TUI. It implements the
// precedence — routesFile > the tx-file's mock_routes > no routes — and
// returns the app-package ConfigError for config-class failures naming
// the path; frontends translate it into the exit-code taxonomy.
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/transactions"
)

// ResolveRoutes implements the documented precedence
// --routes-file > the tx file's mock_routes > no routes. An explicit
// routesFile that cannot be read, parsed, or that carries no route entry is
// always a config error naming the path; the tx-file source keeps the
// silent fallback (a tx file that fails to load contributes zero routes).
func ResolveRoutes(routesFile, txPath string, spec *iso8583.MessageSpec) ([]config.MockRouteConfig, transactions.Repository, error) {
	if routesFile = strings.TrimSpace(routesFile); routesFile != "" {
		routes, err := LoadRoutesFile(routesFile)
		if err != nil {
			return nil, nil, err
		}

		return routes, nil, nil
	}

	if txPath != "" && spec != nil {
		if tc, err := transactions.NewTransactionCollection(txPath, spec); err == nil {
			return tc.GetMockRoutes(), tc, nil
		}
	}

	return nil, nil, nil
}

// LoadRoutesFile parses an explicit mock-routes JSON file: an array whose
// route-shaped entries load and whose other entries (the transactions,
// datasets and scenarios of a saved config file) are skipped. Route-shaped
// means tagged mock_route, or — a hand-written routes file carries no type
// keys — carrying match fields or a response MTI. Every failure is a
// config-class error naming the path: a file of entries with no route among
// them is named entry by entry rather than starting a routeless server.
func LoadRoutesFile(path string) ([]config.MockRouteConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{Path: path, Err: fmt.Errorf("failed to read mock routes file: %w", err)}
	}

	var entries []config.Item
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, &ConfigError{Path: path, Err: fmt.Errorf("malformed mock routes file: %w", err)}
	}

	var routes []config.MockRouteConfig
	var skipped []string
	for i, e := range entries {
		if !isRouteEntry(e) {
			skipped = append(skipped, entryLabel(e))
			continue
		}
		if strings.TrimSpace(e.Name) == "" {
			return nil, &ConfigError{Path: path, Err: fmt.Errorf("mock route #%d is missing a name", i+1)}
		}

		routes = append(routes, routeFromItem(e))
	}

	if len(routes) == 0 && len(entries) > 0 {
		return nil, &ConfigError{Path: path, Err: fmt.Errorf(
			"routes file %s: %d entries, none are mock routes (%s)", path, len(entries), skipList(skipped))}
	}

	return routes, nil
}

// isRouteEntry reports whether one config entry is a mock route: tagged as
// one, or type-less (routes-only files have no discriminator) and carrying
// a match field or a response MTI.
func isRouteEntry(e config.Item) bool {
	if e.Type == config.TypeMockRoute {
		return true
	}

	return e.Type == "" && (len(e.MatchFields) > 0 || e.ResponseMTI != "")
}

// routeFromItem narrows a config entry to the route fields. The two types
// carry them under the same JSON names, so the copy is lossless; the entry's
// transaction/dataset/scenario keys are foreign to a route and stay behind.
func routeFromItem(e config.Item) config.MockRouteConfig {
	return config.MockRouteConfig{
		Name:           e.Name,
		Description:    e.Description,
		MatchFields:    e.MatchFields,
		RequiredFields: e.RequiredFields,
		EchoFields:     e.EchoFields,
		ResponseMTI:    e.ResponseMTI,
		ResponseFields: e.ResponseFields,
		DelayMs:        e.DelayMs,
		LatencyMs:      e.LatencyMs,
		JitterMs:       e.JitterMs,
		DropConnection: e.DropConnection,
	}
}

// entryLabel names one skipped entry as name:type, with angle-bracket
// placeholders where the entry has no name or no type, so the operator can
// find it in the file.
func entryLabel(e config.Item) string {
	name := strings.TrimSpace(e.Name)
	if name == "" {
		name = "<unnamed>"
	}
	typ := string(e.Type)
	if typ == "" {
		typ = "<no type>"
	}

	return name + ":" + typ
}

// skipList joins the skipped-entry names, capped at five with a count of
// the rest so a whole config file cannot turn the error into a paragraph.
func skipList(skipped []string) string {
	const shown = 5
	if len(skipped) <= shown {
		return strings.Join(skipped, ", ")
	}

	return strings.Join(skipped[:shown], ", ") + fmt.Sprintf(", +%d more", len(skipped)-shown)
}
