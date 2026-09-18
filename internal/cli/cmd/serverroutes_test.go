package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/app"
	"jiso/internal/utils"
)

// writeRoutesFixture materialises a file for the precedence table and
// returns its path.
func writeRoutesFixture(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

const routesFileTwo = `[
  {"name": "flag-echo", "match_fields": {"0": "0800"}, "response_mti": "0810"},
  {"name": "flag-purchase", "match_fields": {"0": "0200"}, "response_mti": "0210", "response_fields": {"39": "00"}}
]`

// txFileWithRoute is a tx file whose mock_routes entry the --routes-file
// flag must override; the "type" discriminator mirrors real tx files.
const txFileWithRoute = `[
  {"type": "transaction", "name": "Echo", "fields": {"0": "0800"}},
  {"type": "mock_route", "name": "tx-echo", "match_fields": {"0": "0800"}, "response_mti": "0810"}
]`

// combinedCfgFile is a saved config file (the §J write shape). As a
// --routes-file it must contribute its route and nothing else; with the
// route removed it contributes nothing at all, which is an error.
const combinedCfgFile = `[
  {"type": "transaction", "name": "Echo", "fields": {"0": "0800"}},
  {"type": "dataset", "name": "card_pool", "data": [{"2": "4111111111111111"}]},
  {"type": "scenario", "name": "Purchase", "steps": []},
  {"type": "mock_route", "name": "cfg-echo", "match_fields": {"0": "0800"}, "response_mti": "0810"}
]`

const combinedCfgFileNoRoutes = `[
  {"type": "transaction", "name": "Echo", "fields": {"0": "0800"}},
  {"type": "dataset", "name": "card_pool", "data": [{"2": "4111111111111111"}]}
]`

func TestResolveServeRoutesPrecedence(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	routesFile := writeRoutesFixture(t, "routes.json", routesFileTwo)
	txFile := writeRoutesFixture(t, "tx.json", txFileWithRoute)
	emptyRoutesFile := writeRoutesFixture(t, "empty-routes.json", "[]")
	combinedFile := writeRoutesFixture(t, "combined.json", combinedCfgFile)
	noRouteCfgFile := writeRoutesFixture(t, "no-routes.json", combinedCfgFileNoRoutes)
	malformedFile := writeRoutesFixture(t, "bad-routes.json", `{"name": "not-an-array"}`)
	namelessFile := writeRoutesFixture(t, "nameless-routes.json", `[{"response_mti": "0810"}]`)
	missingFile := filepath.Join(t.TempDir(), "nope.json")

	tests := []struct {
		name        string
		routesFile  string
		txPath      string
		wantNames   []string
		wantRepo    bool
		wantExitErr bool
		wantPathIn  string
	}{
		{
			name:       "routes-file beats tx mock_routes",
			routesFile: routesFile,
			txPath:     txFile,
			wantNames:  []string{"flag-echo", "flag-purchase"},
		},
		{
			name:      "tx mock_routes used when no routes-file",
			txPath:    txFile,
			wantNames: []string{"tx-echo"},
			wantRepo:  true,
		},
		{
			name:       "explicit empty routes-file overrides tx routes with none",
			routesFile: emptyRoutesFile,
			txPath:     txFile,
			wantNames:  []string{},
		},
		{
			name:       "combined config file contributes only its route",
			routesFile: combinedFile,
			wantNames:  []string{"cfg-echo"},
		},
		{
			name:        "config file carrying no route entry is exit 3",
			routesFile:  noRouteCfgFile,
			wantExitErr: true,
			wantPathIn:  noRouteCfgFile,
		},
		{
			name:      "no source yields no routes",
			wantNames: []string{},
		},
		{
			name:        "missing routes-file is exit 3 naming the path",
			routesFile:  missingFile,
			wantExitErr: true,
			wantPathIn:  missingFile,
		},
		{
			name:        "malformed routes-file (object, not array) is exit 3",
			routesFile:  malformedFile,
			wantExitErr: true,
			wantPathIn:  malformedFile,
		},
		{
			name:        "route without a name is exit 3",
			routesFile:  namelessFile,
			wantExitErr: true,
			wantPathIn:  namelessFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes, repo, err := app.ResolveRoutes(tt.routesFile, tt.txPath, spec)

			if tt.wantExitErr {
				err = serveRoutesError(err) // CLI exit-code mapping
				require.Error(t, err)
				assert.Equal(t, ExitConfig, ExitCodeForError(err), "malformed routes file must map to exit 3")
				assert.Contains(t, err.Error(), tt.wantPathIn, "error must name the routes file path")
				assert.Nil(t, routes)

				return
			}

			require.NoError(t, err)
			names := make([]string, 0, len(routes))
			for _, r := range routes {
				names = append(names, r.Name)
			}
			assert.Equal(t, tt.wantNames, names)
			assert.Equal(t, tt.wantRepo, repo != nil)
		})
	}
}

// TestResolveServeRoutesTxFailureStaysSilent pins the legacy fallback:
// an unloadable tx file contributes zero routes and never errors.
func TestResolveServeRoutesTxFailureStaysSilent(t *testing.T) {
	t.Parallel()

	badTx := writeRoutesFixture(t, "bad-tx.json", "not json at all")

	routes, repo, err := app.ResolveRoutes("", badTx, utils.GetDefaultSpec())
	require.NoError(t, err)
	assert.Empty(t, routes)
	assert.Nil(t, repo)
}

// TestServeStartSkipsGlobalSignalWatcher pins the annotation wiring:
// `serve start` owns SIGINT/SIGTERM (clean stop, exit 0), so the fail-fast
// 128+signal watcher must skip it while every other command keeps it.
func TestServeStartSkipsGlobalSignalWatcher(t *testing.T) {
	root := NewRootCmd()

	var serveStart, scenarioRun *cobra.Command
	for _, c := range root.Commands() {
		switch c.Name() {
		case "server":
			for _, sub := range c.Commands() {
				if sub.Name() == "start" {
					serveStart = sub
				}
			}
		case "scenario":
			for _, sub := range c.Commands() {
				if sub.Name() == "run" {
					scenarioRun = sub
				}
			}
		}
	}

	require.NotNil(t, serveStart)
	require.NotNil(t, scenarioRun)
	assert.True(t, globalSignalWatchSkipped(serveStart), "serve start must own its signals")
	assert.False(t, globalSignalWatchSkipped(scenarioRun), "one-shot commands keep the 128+signal watcher")
	assert.False(t, globalSignalWatchSkipped(root))
}

// TestLoadRoutesFileTxShapeLoadsRoutesOnly pins the F1 filter on the CLI
// path: a tx-file-shaped routes array loads its mock_route entry and skips
// the transaction entry, which used to arrive as an empty "route".
func TestLoadRoutesFileTxShapeLoadsRoutesOnly(t *testing.T) {
	t.Parallel()

	path := writeRoutesFixture(t, "tx-shaped.json", txFileWithRoute)

	routes, err := app.LoadRoutesFile(path)
	require.NoError(t, err)

	require.Len(t, routes, 1)
	assert.Equal(t, "tx-echo", routes[0].Name)
}
