package config

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTLSConfig_Success(t *testing.T) {
	// Create temporary directory with test config and certs
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")
	caFile := filepath.Join(tmpDir, "ca.crt")
	configFile := filepath.Join(tmpDir, "tls_config.json")

	require.NoError(t, os.WriteFile(certFile, []byte("dummy cert"), 0644))
	require.NoError(t, os.WriteFile(keyFile, []byte("dummy key"), 0600))
	require.NoError(t, os.WriteFile(caFile, []byte("dummy ca"), 0644))

	jsonContent := `{
		"enabled": true,
		"client_cert": "./client.crt",
		"client_key": "./client.key",
		"ca_cert": "./ca.crt",
		"server_name": "smc.visa.com",
		"min_version": "1.3",
		"insecure_skip_verify": true
	}`
	require.NoError(t, os.WriteFile(configFile, []byte(jsonContent), 0644))

	cfg, err := LoadTLSConfig(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, certFile, cfg.ClientCert)
	assert.Equal(t, keyFile, cfg.ClientKey)
	assert.Equal(t, caFile, cfg.CACert)
	assert.Equal(t, "smc.visa.com", cfg.ServerName)
	assert.Equal(t, "1.3", cfg.MinVersion)
	assert.True(t, cfg.InsecureSkipVerify)
}

func TestLoadTLSConfig_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "tls_disabled.json")
	jsonContent := `{"enabled": false}`
	require.NoError(t, os.WriteFile(configFile, []byte(jsonContent), 0644))

	cfg, err := LoadTLSConfig(configFile)
	require.NoError(t, err)
	assert.False(t, cfg.Enabled)

	cryptoCfg, err := cfg.BuildCryptoTLSConfig()
	require.NoError(t, err)
	assert.Nil(t, cryptoCfg)
}

func TestLoadTLSConfig_MissingCertFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "tls_missing.json")
	jsonContent := `{
		"enabled": true,
		"client_cert": "./nonexistent.crt",
		"client_key": "./nonexistent.key",
		"ca_cert": "./nonexistent.ca"
	}`
	require.NoError(t, os.WriteFile(configFile, []byte(jsonContent), 0644))

	_, err := LoadTLSConfig(configFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestBuildCryptoTLSConfig_WithGeneratedCerts(t *testing.T) {
	// Test against real PEM certs generated in testdata/certs/ if present
	certsDir := filepath.Join("..", "..", "testdata", "certs")
	configFile := filepath.Join(certsDir, "tls_config.json")

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Skip("testdata/certs/tls_config.json not found; skipping real cert test")
	}

	cfg, err := LoadTLSConfig(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	cryptoCfg, err := cfg.BuildCryptoTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cryptoCfg)

	assert.Equal(t, uint16(tls.VersionTLS12), cryptoCfg.MinVersion)
	assert.Len(t, cryptoCfg.Certificates, 1)
	assert.NotNil(t, cryptoCfg.RootCAs)
}
