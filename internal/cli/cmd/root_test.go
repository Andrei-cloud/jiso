package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	cfg "jiso/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "jiso version v1.5.0")
}

func TestVersionAliasCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"v"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "jiso version v1.5.0")
}

func TestSpecInitCommand(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "test_spec.json")

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"spec", "init", outPath})

	err := rootCmd.Execute()
	require.NoError(t, err)

	_, err = os.Stat(outPath)
	assert.NoError(t, err)
}

func TestServerAliasCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"serve", "--help"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Manage embedded ISO8583 mock server")
}

func TestAnalyzeAliasCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"pcap", "--help"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Analyze stream/PCAP capture files")
}

func TestPersistentFlagsConfigMapping(t *testing.T) {
	c := cfg.GetConfig()
	c.Reset()

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"--hex", "--host", "127.0.0.1", "--port", "8888", "version"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	assert.True(t, c.GetHex())
	assert.Equal(t, "127.0.0.1", c.GetHost())
	assert.Equal(t, "8888", c.GetPort())
}

func TestDbFlagMapping(t *testing.T) {
	c := cfg.GetConfig()
	c.Reset()

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"--db", "/tmp/test.db", "version"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	assert.Equal(t, "/tmp/test.db", c.GetDbPath())
}

func TestREPLFallback(t *testing.T) {
	replCalled := false
	SetREPLRunner(func(ctx context.Context) error {
		replCalled = true
		return nil
	})

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.True(t, replCalled)
}
