package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "jiso/internal/config"
	"jiso/internal/version"
)

func TestRootVersionFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"long flag", []string{"--version"}},
		{"short flag", []string{"-v"}},
	}

	wantLines := []string{
		"jiso version " + version.Version,
		"commit " + version.Commit,
		"built at " + version.BuiltAt,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd := NewRootCmd()
			out := new(bytes.Buffer)
			errBuf := new(bytes.Buffer)
			rootCmd.SetOut(out)
			rootCmd.SetErr(errBuf)
			rootCmd.SetArgs(tt.args)

			err := rootCmd.Execute()
			require.NoError(t, err)

			for _, line := range wantLines {
				assert.Contains(t, out.String(), line)
			}

			assert.Empty(t, errBuf.String())
		})
	}
}

func TestVersionCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "jiso version "+version.Version)
}

func TestVersionAliasCommand(t *testing.T) {
	rootCmd := NewRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"v"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "jiso version "+version.Version)
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

// TestInitDryRunWritesNothing asserts spec init / tx init honor --dry-run:
// the plan is printed and no file (or directory) is written.
func TestInitDryRunWritesNothing(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		topic string
	}{
		{"spec init", []string{"spec", "init"}, "default specification file"},
		{"tx init", []string{"tx", "init"}, "sample transaction configuration file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanRoom := t.TempDir()
			target := filepath.Join(cleanRoom, "out.json")

			rootCmd := NewRootCmd()
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(new(bytes.Buffer))
			rootCmd.SetArgs(append(append([]string{}, tt.args...), target, "--dry-run"))

			err := rootCmd.Execute()
			require.NoError(t, err)

			assert.Contains(t, buf.String(), "dry-run: would write "+tt.topic)
			assert.False(t, fileExists(target), "dry-run must not write the output file")

			entries, err := os.ReadDir(cleanRoom)
			require.NoError(t, err)
			assert.Empty(t, entries, "dry-run must write nothing into the clean room")
		})

		t.Run(tt.name+" --json", func(t *testing.T) {
			cleanRoom := t.TempDir()
			target := filepath.Join(cleanRoom, "out.json")

			rootCmd := NewRootCmd()
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(new(bytes.Buffer))
			rootCmd.SetArgs(append(append([]string{}, tt.args...), target, "--dry-run", "--json"))

			err := rootCmd.Execute()
			require.NoError(t, err)

			var plan map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &plan), "stdout must be pure JSON: %q", buf.String())
			assert.Equal(t, true, plan["dry_run"])
			assert.Equal(t, target, plan["path"])
			assert.False(t, fileExists(target))
		})
	}
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
