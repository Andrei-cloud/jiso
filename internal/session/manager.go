package session

import (
	"path/filepath"
	"strings"

	"jiso/internal/config"
	"jiso/internal/db"
)

type SessionManager struct{}

var defaultManager = &SessionManager{}

func GetManager() *SessionManager {
	return defaultManager
}

func (m *SessionManager) StartSession(specPath, txPath string) (string, error) {
	cfg := config.GetConfig()
	cfg.EnsureSessionId()
	sessionID := cfg.GetSessionId()

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

func (m *SessionManager) RotateSession(specPath, txPath string) (string, error) {
	cfg := config.GetConfig()
	oldSessionID := cfg.GetSessionId()

	var connType, headerType string
	host := cfg.GetHost()
	port := cfg.GetPort()
	tlsEnabled := cfg.GetTLSConfigPath() != "" || (cfg.GetTLSConfig() != nil && cfg.GetTLSConfig().Enabled)

	if cfg.GetDbPath() != "" && oldSessionID != "" {
		// Update old session status
		rec, err := db.GetSessionByID(oldSessionID)
		if err == nil && rec != nil {
			connType = rec.ConnectionType
			headerType = rec.HeaderType
			if rec.Host != "" {
				host = rec.Host
			}
			if rec.Port != "" {
				port = rec.Port
			}
			if rec.TLSEnabled {
				tlsEnabled = true
			}
			_ = db.UpsertSession(oldSessionID, rec.SpecPath, rec.SpecName, rec.TxFilePath, rec.TxFileName, rec.Host, rec.Port, rec.ConnectionType, rec.HeaderType, "reloaded", rec.TLSEnabled)
		}
	}

	newSessionID := cfg.RotateSessionId()
	specName := extractFileName(specPath)
	txFileName := extractFileName(txPath)

	if cfg.GetDbPath() != "" {
		_ = db.UpsertSession(newSessionID, specPath, specName, txPath, txFileName, host, port, connType, headerType, "active", tlsEnabled)
	}

	return newSessionID, nil
}

// UpdateSession updates specification and transaction file metadata for the current session without changing the session ID.
func (m *SessionManager) UpdateSession(specPath, txPath string) error {
	cfg := config.GetConfig()
	cfg.EnsureSessionId()
	sessionID := cfg.GetSessionId()

	specName := extractFileName(specPath)
	txFileName := extractFileName(txPath)
	host := cfg.GetHost()
	port := cfg.GetPort()
	tlsEnabled := cfg.GetTLSConfigPath() != "" || (cfg.GetTLSConfig() != nil && cfg.GetTLSConfig().Enabled)

	if cfg.GetDbPath() != "" {
		return db.UpsertSession(sessionID, specPath, specName, txPath, txFileName, host, port, "", "", "active", tlsEnabled)
	}
	return nil
}

// UpdateOrRotateSession updates the current session if it is empty (has no recorded transactions),
// or rotates to a new session if the current session has already processed transactions.
// Returns the session ID, whether a rotation occurred, and any error.
func (m *SessionManager) UpdateOrRotateSession(specPath, txPath string) (string, bool, error) {
	cfg := config.GetConfig()
	currentSessionID := cfg.GetSessionId()

	if currentSessionID == "" {
		sID, err := m.StartSession(specPath, txPath)
		return sID, false, err
	}

	// Check if current session has any recorded transactions or stress tests
	hasActivity := false
	if cfg.GetDbPath() != "" {
		count, err := db.GetSessionTransactionCount(currentSessionID)
		if err == nil && count > 0 {
			hasActivity = true
		}
		if !hasActivity {
			summaries, err := db.GetSessionStressTestSummaries(currentSessionID)
			if err == nil && len(summaries) > 0 {
				hasActivity = true
			}
		}
	}

	if !hasActivity {
		// Session is empty/fresh: update existing session metadata without rotating session ID
		err := m.UpdateSession(specPath, txPath)
		return currentSessionID, false, err
	}

	// Session is non-empty: rotate to a new session
	newSessionID, err := m.RotateSession(specPath, txPath)
	return newSessionID, true, err
}

func (m *SessionManager) UpdateConnectionDetails(connType, host, port, headerType string, tlsEnabled bool) error {
	cfg := config.GetConfig()
	sessionID := cfg.GetSessionId()
	if sessionID == "" || cfg.GetDbPath() == "" {
		return nil
	}
	return db.UpdateSessionConnection(sessionID, connType, host, port, headerType, tlsEnabled)
}

func (m *SessionManager) GetCurrentSessionID() string {
	return config.GetConfig().GetSessionId()
}

func extractFileName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

