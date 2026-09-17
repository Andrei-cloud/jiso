// precedence_settings_test.go pins the precedence extension to
// the §L settings keys: a value persisted by the TUI settings screen
// resolves through $JISO_* > user config > default into the persistent
// flag (marked Changed) and thus into the shared cfg the App consumes,
// so the settings screen's file values are the next invocation's
// defaults. Unparsable env values count as unset (CLI-102 leniency).
package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "jiso/internal/config"
)

func TestPrecedenceSettingsConfigFileFeedsFlags(t *testing.T) {
	isolateConfig(t)
	writeUserConfig(t, "reconnect_attempts: 5\nconnect_timeout: 8s\ntotal_connect_timeout: 30s\nresponse_timeout: 3s\nlisten_timeout: 90s\nhex: true\n")

	rootCmd, _, _, err := runPrecedence(t, "version")
	require.NoError(t, err)

	flags := rootCmd.Flags()
	assert.True(t, flags.Changed("connect-timeout"), "config value must mark the flag Changed")

	if v, _ := flags.GetInt("reconnect-attempts"); v != 5 {
		t.Errorf("reconnect-attempts = %d, want 5", v)
	}
	if v, _ := flags.GetDuration("connect-timeout"); v != 8*time.Second {
		t.Errorf("connect-timeout = %v, want 8s", v)
	}
	if v, _ := flags.GetDuration("total-connect-timeout"); v != 30*time.Second {
		t.Errorf("total-connect-timeout = %v, want 30s", v)
	}
	if v, _ := flags.GetDuration("response-timeout"); v != 3*time.Second {
		t.Errorf("response-timeout = %v, want 3s", v)
	}
	if v, _ := flags.GetDuration("listen-timeout"); v != 90*time.Second {
		t.Errorf("listen-timeout = %v, want 90s", v)
	}
	if v, _ := flags.GetBool("hex"); !v {
		t.Errorf("hex = %v, want true", v)
	}

	c := cfg.GetConfig()
	assert.Equal(t, 5, c.GetReconnectAttempts())
	assert.Equal(t, 8*time.Second, c.GetConnectTimeout())
	assert.Equal(t, 3*time.Second, c.GetResponseTimeout())
	assert.True(t, c.GetHex())
}

func TestPrecedenceSettingsEnvBeatsConfig(t *testing.T) {
	isolateConfig(t)
	writeUserConfig(t, "connect_timeout: 8s\nresponse_timeout: 4s\n")
	t.Setenv("JISO_CONNECT_TIMEOUT", "9s")
	t.Setenv("JISO_RESPONSE_TIMEOUT", "nonsense") // unparsable = unset

	rootCmd, _, _, err := runPrecedence(t, "version")
	require.NoError(t, err)

	if v, _ := rootCmd.Flags().GetDuration("connect-timeout"); v != 9*time.Second {
		t.Errorf("connect-timeout = %v, want env 9s", v)
	}
	// The unparsable env falls through to the config layer.
	if v, _ := rootCmd.Flags().GetDuration("response-timeout"); v != 4*time.Second {
		t.Errorf("response-timeout = %v, want config 4s", v)
	}
}
