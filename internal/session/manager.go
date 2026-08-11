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

	if cfg.GetDbPath() != "" {
		_ = db.UpsertSession(sessionID, specPath, specName, txPath, txFileName, "active")
	}

	return sessionID, nil
}

func (m *SessionManager) RotateSession(specPath, txPath string) (string, error) {
	cfg := config.GetConfig()
	oldSessionID := cfg.GetSessionId()

	if cfg.GetDbPath() != "" && oldSessionID != "" {
		// Update old session status
		rec, err := db.GetSessionByID(oldSessionID)
		if err == nil && rec != nil {
			_ = db.UpsertSession(oldSessionID, rec.SpecPath, rec.SpecName, rec.TxFilePath, rec.TxFileName, "reloaded")
		}
	}

	newSessionID := cfg.RotateSessionId()
	specName := extractFileName(specPath)
	txFileName := extractFileName(txPath)

	if cfg.GetDbPath() != "" {
		_ = db.UpsertSession(newSessionID, specPath, specName, txPath, txFileName, "active")
	}

	return newSessionID, nil
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
