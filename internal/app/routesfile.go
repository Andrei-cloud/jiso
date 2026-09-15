// routesfile.go holds the routes-only mock-route loader hoisted out of the
// CLI (Task 2.1 DRY): the `serve start` --routes-file path and the TUI share
// this one copy. It implements the PAR-309 precedence — routesFile > the
// tx-file's mock_routes > no routes — and returns the app-package
// ConfigError mirror for config-class failures naming the path; frontends
// (internal/cli/cmd) translate it into the process exit-code taxonomy.
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
// --routes-file > the tx file's mock_routes > no routes.
//
// An explicit routesFile that cannot be read or parsed is always a
// config error naming the path — the user asked for exactly that file, so
// silently serving without routes would be a lie. The tx-file source keeps
// the pre-PAR-309 silent fallback: a tx file that fails to load contributes
// zero routes (its parse failures surface in the commands that actually
// consume transactions).
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
// route objects shaped like the tx file's mock_route entries (their "type"
// field is simply ignored). Every failure names the path so the config-class
// message identifies the file the user pointed at.
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
