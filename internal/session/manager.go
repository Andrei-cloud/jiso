package session

import (
	"path/filepath"
	"strings"

	"jiso/internal/config"
	"jiso/internal/db"
)

// Manager is the entry point for session bookkeeping: it hands out the id
// a run logs under and records the row the session browser lists. It keeps no
// state of its own because the id lives in config, where the CLI precedence rules
// put it -- a second copy here would be free to disagree with every reader.
type Manager struct{}

var defaultManager = &Manager{}

// GetManager returns the process-wide Manager, so call sites do not each
// construct one. The state that matters is in config and the session database, not
// here.
func GetManager() *Manager {
	return defaultManager
}

// StartSession makes sure this run has a session id and records the session
// context -- spec and transaction file, peer host and port, whether TLS is on --
// in the session database when one is configured. Without --db it still returns a
// usable id: session logging is off, which the panes report as an empty state
// rather than as an error.
func (m *Manager) StartSession(specPath, txPath string) (string, error) {
	cfg := config.GetConfig()
	cfg.EnsureSessionID()
	sessionID := cfg.GetSessionID()

	specName := extractFileName(specPath)
	txFileName := extractFileName(txPath)
	host := cfg.GetHost()
	port := cfg.GetPort()
	tlsEnabled := cfg.GetTLSConfigPath() != "" || (cfg.GetTLSConfig() != nil && cfg.GetTLSConfig().Enabled)

	if cfg.GetDbPath() != "" {
		_ = db.UpsertSession(sessionID, specPath, specName, txPath, txFileName, host, port, "", "", "active", tlsEnabled)
	}

	return sessionID, nil
}

func extractFileName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}
