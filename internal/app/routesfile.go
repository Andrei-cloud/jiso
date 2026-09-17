// routesfile.go holds the routes-only mock-route loader shared by the
// `serve start` --routes-file path and the TUI. It implements the
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
// routesFile that cannot be read or parsed is always a config error
// naming the path; the tx-file source keeps the silent fallback (a tx
// file that fails to load contributes zero routes).
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

// LoadRoutesFile parses an explicit mock-routes JSON file: an array of
// route objects shaped like the tx file's mock_route entries. Every
// failure is a config-class error naming the path.
func LoadRoutesFile(path string) ([]config.MockRouteConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{Path: path, Err: fmt.Errorf("failed to read mock routes file: %w", err)}
	}

	var routes []config.MockRouteConfig
	if err := json.Unmarshal(raw, &routes); err != nil {
		return nil, &ConfigError{Path: path, Err: fmt.Errorf("malformed mock routes file: %w", err)}
	}

	for i := range routes {
		if strings.TrimSpace(routes[i].Name) == "" {
			return nil, &ConfigError{Path: path, Err: fmt.Errorf("mock route #%d is missing a name", i+1)}
		}
	}

	return routes, nil
}
