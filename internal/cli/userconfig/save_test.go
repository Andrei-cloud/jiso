// save_test.go pins the §L persistence contract: Save merges
// only the changed keys (typed per schema), untouched keys survive,
// the result round-trips through Load, and unparsable values error
// without writing.
package userconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveMergesOnlyChangedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("host: h\nquiet: true\n"), 0o600))
	t.Setenv(EnvConfigVar, path)

	require.NoError(t, Save(path, map[string]string{
		"connect_timeout":       "8s",
		"total_connect_timeout": "30s",
		"reconnect_attempts":    "5",
		"hex":                   "true",
	}))

	f, _, err := Load()
	require.NoError(t, err)
	require.NotNil(t, f.ConnectTimeout)
	assert.Equal(t, "8s", *f.ConnectTimeout)
	require.NotNil(t, f.TotalConnectTimeout)
	assert.Equal(t, "30s", *f.TotalConnectTimeout)
	require.NotNil(t, f.ReconnectAttempts)
	assert.Equal(t, 5, *f.ReconnectAttempts)
	require.NotNil(t, f.Hex)
	assert.True(t, *f.Hex)
	// Untouched keys keep their values; unset keys stay absent.
	require.NotNil(t, f.Host)
	assert.Equal(t, "h", *f.Host)
	require.NotNil(t, f.Quiet)
	assert.True(t, *f.Quiet)
	assert.Nil(t, f.DB)
	assert.Nil(t, f.ListenTimeout)
}

func TestSaveCreatesMissingFileAndPreservesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("future_key: keep\n"), 0o600))
	t.Setenv(EnvConfigVar, path)

	require.NoError(t, Save(path, map[string]string{"listen_timeout": "90s"}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "future_key: keep")

	f, _, err := Load()
	require.NoError(t, err)
	require.NotNil(t, f.ListenTimeout)
	assert.Equal(t, "90s", *f.ListenTimeout)
}

func TestSaveRejectsUnparsableValuesWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("host: h\n"), 0o600))
	before, _ := os.ReadFile(path)

	err := Save(path, map[string]string{"reconnect_attempts": "many"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconnect_attempts")

	after, _ := os.ReadFile(path)
	assert.Equal(t, string(before), string(after))

	err = Save(path, map[string]string{"hex": "maybe"})
	require.Error(t, err)

	after, _ = os.ReadFile(path)
	assert.Equal(t, string(before), string(after))
}

func TestSaveEmptyUpdatesIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	require.NoError(t, Save(path, nil))
	assert.NoFileExists(t, path)
}
