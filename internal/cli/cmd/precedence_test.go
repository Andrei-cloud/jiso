package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/cli/userconfig"
	cfg "jiso/internal/config"
)

// jisoEnvVars is the full standard env set (00-overhaul-plan); every
// precedence test clears it so the developer's shell can never leak in.
var jisoEnvVars = []string{
	"JISO_SPEC", "JISO_FILE", "JISO_DB", "JISO_HOST", "JISO_PORT",
	"JISO_HEADER", "JISO_TLS_CONFIG", "JISO_VISA_STATION_ID",
	"JISO_JSON", "JISO_QUIET", "JISO_DEBUG", "JISO_UNSECURE", "JISO_CONFIG",
	// SCR-512 §L settings keys.
	"JISO_RECONNECT_ATTEMPTS", "JISO_CONNECT_TIMEOUT", "JISO_TOTAL_CONNECT_TIMEOUT",
	"JISO_RESPONSE_TIMEOUT", "JISO_LISTEN_TIMEOUT", "JISO_HEX",
}

// isolateConfig clears $JISO_* and points $JISO_CONFIG at a nonexistent
// temp path, so the real user config never leaks into a test.
//
// Accepted debt (M1 review #21, deferred by orchestrator): clearing with
// t.Setenv(k, "") means "env present but empty" is still not distinguished
// from "env absent", and most cases here run the exempt version command.
func isolateConfig(t *testing.T) {
	t.Helper()

	for _, k := range jisoEnvVars {
		t.Setenv(k, "")
	}
	t.Setenv("JISO_CONFIG", filepath.Join(t.TempDir(), "absent-config.yaml"))
}

// writeUserConfig plants a user config file and points $JISO_CONFIG at it.
func writeUserConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv("JISO_CONFIG", path)

	return path
}

// runPrecedence executes the root command (version: exempt from spec/tx
// file validation, so bogus spec values are safe) with isolated config.
func runPrecedence(t *testing.T, args ...string) (rootCmd *cobra.Command, stdout, stderr string, err error) {
	t.Helper()

	resetConfig(t)

	rootCmd = NewRootCmd()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(args)

	err = rootCmd.Execute()

	return rootCmd, out.String(), errBuf.String(), err
}

func TestPrecedenceStringFlags(t *testing.T) {
	tests := []struct {
		name     string
		envHost  string
		envSpec  string
		cfgBody  string
		args     []string
		wantHost string
		wantSpec string
	}{
		{
			name:     "default when nothing set",
			wantHost: "",
			wantSpec: "",
		},
		{
			name:     "config beats default",
			cfgBody:  "host: config-host\nspec: config-spec.json\n",
			wantHost: "config-host",
			wantSpec: "config-spec.json",
		},
		{
			name:     "env beats config",
			envHost:  "env-host",
			envSpec:  "env-spec.json",
			cfgBody:  "host: config-host\nspec: config-spec.json\n",
			wantHost: "env-host",
			wantSpec: "env-spec.json",
		},
		{
			name:     "env beats default",
			envHost:  "env-host",
			envSpec:  "env-spec.json",
			wantHost: "env-host",
			wantSpec: "env-spec.json",
		},
		{
			name:     "flag beats env",
			envHost:  "env-host",
			envSpec:  "env-spec.json",
			cfgBody:  "host: config-host\nspec: config-spec.json\n",
			args:     []string{"--host", "flag-host", "--spec", "flag-spec.json"},
			wantHost: "flag-host",
			wantSpec: "flag-spec.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("JISO_HOST", tt.envHost)
			t.Setenv("JISO_SPEC", tt.envSpec)
			writeUserConfigIf(t, tt.cfgBody)

			_, stdout, stderr, err := runPrecedence(t, append(tt.args, "version")...)
			require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)

			c := cfg.GetConfig()
			assert.Equal(t, tt.wantHost, c.GetHost())
			assert.Equal(t, tt.wantSpec, c.GetSpec())
		})
	}
}

func TestPrecedenceOutputBoolFlags(t *testing.T) {
	tests := []struct {
		name      string
		envJSON   string
		envQuiet  string
		cfgBody   string
		args      []string
		wantJSON  bool
		wantQuiet bool
	}{
		{
			name: "default off",
		},
		{
			name:      "config beats default",
			cfgBody:   "json: true\nquiet: true\n",
			wantJSON:  true,
			wantQuiet: true,
		},
		{
			name:      "truthy env beats config",
			envJSON:   "1",
			envQuiet:  "true",
			cfgBody:   "json: false\nquiet: false\n",
			wantJSON:  true,
			wantQuiet: true,
		},
		{
			name:      "falsy env beats config true",
			envJSON:   "0",
			cfgBody:   "json: true\n",
			wantJSON:  false,
			wantQuiet: false,
		},
		{
			name:      "flag beats env",
			envJSON:   "0",
			envQuiet:  "0",
			cfgBody:   "json: true\nquiet: true\n",
			args:      []string{"--json", "-q"},
			wantJSON:  true,
			wantQuiet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("JISO_JSON", tt.envJSON)
			t.Setenv("JISO_QUIET", tt.envQuiet)
			writeUserConfigIf(t, tt.cfgBody)

			rootCmd, stdout, stderr, err := runPrecedence(t, append(tt.args, "version")...)
			require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)

			gotJSON, _ := rootCmd.Flags().GetBool("json")
			gotQuiet, _ := rootCmd.Flags().GetBool("quiet")
			assert.Equal(t, tt.wantJSON, gotJSON)
			assert.Equal(t, tt.wantQuiet, gotQuiet)
		})
	}
}

func TestPrecedenceUnsetEnvLeavesConfigLayer(t *testing.T) {
	isolateConfig(t)
	writeUserConfig(t, "host: config-host\njson: true\n")

	rootCmd, stdout, stderr, err := runPrecedence(t, "version")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)

	assert.Equal(t, "config-host", cfg.GetConfig().GetHost())
	got, _ := rootCmd.Flags().GetBool("json")
	assert.True(t, got)
}

func TestDebugGoesToStderrOnly(t *testing.T) {
	tests := []struct {
		name    string
		debug   string
		envHost string
		cfgBody string
		args    []string
		want    []string
		wantNot []string
	}{
		{
			name:    "env layer wins",
			debug:   "1",
			envHost: "env-host",
			want: []string{
				"debug: spec =  (from default)",
				"debug: host = env-host (from env)",
				"debug: port =  (from default)",
			},
		},
		{
			name:  "flag layer wins",
			debug: "1",
			args:  []string{"--host", "flag-host", "--port", "9999"},
			want: []string{
				"debug: host = flag-host (from flag)",
				"debug: port = 9999 (from flag)",
			},
		},
		{
			name:    "config layer wins",
			debug:   "1",
			cfgBody: "host: config-host\n",
			want:    []string{"debug: host = config-host (from config)"},
		},
		{
			name:    "config debug: true enables notices",
			cfgBody: "debug: true\nhost: config-host\n",
			want:    []string{"debug: host = config-host (from config)"},
		},
		{
			name:    "env debug=0 beats config debug: true",
			debug:   "0",
			cfgBody: "debug: true\n",
			wantNot: []string{"debug:"},
		},
		{
			name:    "silent without JISO_DEBUG",
			cfgBody: "host: config-host\n",
			wantNot: []string{"debug:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("JISO_DEBUG", tt.debug)
			t.Setenv("JISO_HOST", tt.envHost)
			writeUserConfigIf(t, tt.cfgBody)

			_, stdout, stderr, err := runPrecedence(t, append(tt.args, "version")...)
			require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)

			assert.NotContains(t, stdout, "debug:", "debug notices must never hit stdout")

			for _, want := range tt.want {
				assert.Contains(t, stderr, want)
			}
			for _, not := range tt.wantNot {
				assert.NotContains(t, stderr, not)
			}
		})
	}
}

func TestMalformedUserConfigExitsConfig(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"broken yaml", "host: [unclosed\n", "failed to parse config file"},
		{"wrong type", "json: not-a-bool\n", "failed to parse config file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			path := writeUserConfig(t, tt.body)

			_, stdout, stderr, err := runPrecedence(t, "version")

			require.Error(t, err)
			assert.Equal(t, ExitConfig, ExitCodeForError(err), "exit code for %v", err)
			assert.Contains(t, err.Error(), path, "error must name the config file")
			assert.Contains(t, err.Error(), tt.want)
			assert.Empty(t, stdout, stderr)
		})
	}
}

// writeUserConfigIf writes a user config only when body is non-empty,
// leaving the isolated nonexistent path in place otherwise.
func writeUserConfigIf(t *testing.T, body string) {
	t.Helper()

	if body == "" {
		return
	}
	writeUserConfig(t, body)
}

func TestPrecedenceBindsRemainingEnvVars(t *testing.T) {
	isolateConfig(t)
	t.Setenv("JISO_FILE", "env-tx.json")
	t.Setenv("JISO_DB", "env.db")
	t.Setenv("JISO_HEADER", "ascii4")
	t.Setenv("JISO_TLS_CONFIG", "")
	t.Setenv("JISO_VISA_STATION_ID", "00ABCD")

	_, stdout, stderr, err := runPrecedence(t, "version")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)

	c := cfg.GetConfig()
	assert.Equal(t, "env-tx.json", c.GetFile())
	assert.Equal(t, "env.db", c.GetDbPath())
	assert.Equal(t, "ascii4", c.GetHeader())
	assert.Equal(t, "00ABCD", c.GetVisaStationID())
}

// TestResolveDBReportsWinningPath asserts the debug resolution names the
// winning database path — including the --db-path alias value — instead of
// an empty string from the flag layer (M1 review #7).
func TestResolveDBReportsWinningPath(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "x"}
		c.Flags().String("db", "", "")
		c.Flags().String("db-path", "", "")

		return c
	}

	t.Run("db-path alias is the flag layer with its value", func(t *testing.T) {
		isolateConfig(t)
		c := newCmd()
		require.NoError(t, c.Flags().Set("db-path", "alias.db"))

		r := resolveDB(c, nil)
		assert.Equal(t, "alias.db", r.value)
		assert.Equal(t, layerFlag, r.from)
	})

	t.Run("db beats db-path", func(t *testing.T) {
		isolateConfig(t)
		c := newCmd()
		require.NoError(t, c.Flags().Set("db-path", "alias.db"))
		require.NoError(t, c.Flags().Set("db", "real.db"))

		r := resolveDB(c, nil)
		assert.Equal(t, "real.db", r.value)
		assert.Equal(t, layerFlag, r.from)
	})

	t.Run("empty explicit flags fall through to env", func(t *testing.T) {
		isolateConfig(t)
		t.Setenv("JISO_DB", "env.db")
		c := newCmd()
		require.NoError(t, c.Flags().Set("db", ""))

		r := resolveDB(c, nil)
		assert.Equal(t, "env.db", r.value)
		assert.Equal(t, layerEnv, r.from)
	})
}

func TestFlagDbPathAliasBeatsEnvDb(t *testing.T) {
	isolateConfig(t)
	t.Setenv("JISO_DB", "env.db")

	_, stdout, stderr, err := runPrecedence(t, "--db-path", "alias.db", "version")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "alias.db", cfg.GetConfig().GetDbPath())
}

// TestUnsecureEnvBindsAnalyzeFlag: JISO_UNSECURE maps to the existing
// analyze -u/--unsecure (mask-off) option (00-overhaul-plan flag policy).
// Running analyze for real needs a capture file, so resolution is exercised
// directly on the located analyze command.
func TestUnsecureEnvBindsAnalyzeFlag(t *testing.T) {
	isolateConfig(t)
	t.Setenv("JISO_UNSECURE", "1")

	rootCmd, _, _, err := runPrecedence(t, "version")
	require.NoError(t, err)

	analyzeCmd, _, findErr := rootCmd.Find([]string{"analyze"})
	require.NoError(t, findErr)
	require.NotNil(t, analyzeCmd.Flags().Lookup("unsecure"))

	uc, _, loadErr := userconfig.Load()
	require.NoError(t, loadErr)

	resolvePrecedence(analyzeCmd, uc)
	got, _ := analyzeCmd.Flags().GetBool("unsecure")
	assert.True(t, got, "$JISO_UNSECURE=1 must enable analyze --unsecure")
}
