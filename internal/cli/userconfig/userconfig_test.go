package userconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathJISOConfigOverride(t *testing.T) {
	t.Setenv(EnvConfigVar, "/tmp/elsewhere/jiso.yaml")

	path, err := Path()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/elsewhere/jiso.yaml", path)
}

func TestPathDefaultsToUserConfigDir(t *testing.T) {
	t.Setenv(EnvConfigVar, "")

	dir, err := os.UserConfigDir()
	require.NoError(t, err)

	path, err := Path()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "jiso", "config.yaml"), path)
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	t.Setenv(EnvConfigVar, filepath.Join(t.TempDir(), "absent.yaml"))

	f, path, err := Load()
	require.NoError(t, err)
	assert.NoFileExists(t, path)
	assert.Nil(t, f.Spec)
	assert.Nil(t, f.Host)
	assert.Nil(t, f.JSON)
}

func TestLoadParsesKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "spec: s.json\nhost: h\nport: \"9\"\njson: true\nvisa_station_id: 00ABCD\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv(EnvConfigVar, path)

	f, gotPath, err := Load()
	require.NoError(t, err)
	assert.Equal(t, path, gotPath)
	require.NotNil(t, f.Spec)
	assert.Equal(t, "s.json", *f.Spec)
	require.NotNil(t, f.Port)
	assert.Equal(t, "9", *f.Port)
	require.NotNil(t, f.JSON)
	assert.True(t, *f.JSON)
	require.NotNil(t, f.VisaStationID)
	assert.Equal(t, "00ABCD", *f.VisaStationID)
}

func TestLoadMalformedNamesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("host: [unclosed\n"), 0o600))
	t.Setenv(EnvConfigVar, path)

	_, gotPath, err := Load()
	require.Error(t, err)
	assert.Equal(t, path, gotPath)
	assert.Contains(t, err.Error(), "failed to parse config file")
}
