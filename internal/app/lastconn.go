// lastconn.go remembers the last SUCCESSFUL connect's details in the
// jiso state dir (same dir + env as the serve side-channel), so the
// connect form and the send wizard's connect step prefill the values
// the user last connected with (UAT: "make connection details as last
// used"). Explicit flag/env/config-file values still win; this only
// fills what the configuration leaves unset.
package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// lastConnectionFile is the state-dir file name.
const lastConnectionFile = "last-connection.json"

// LastConnection is the remembered connect form subset that matters for
// reconnecting to the same target.
type LastConnection struct {
	Host   string `json:"host"`
	Port   string `json:"port"`
	Header string `json:"header,omitempty"`
	TLS    string `json:"tls,omitempty"`
}

// LastConnectionPath is the state-dir path of the last-used file.
func LastConnectionPath() (string, error) {
	dir, err := ServeStateDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, lastConnectionFile), nil
}

// LoadLastConnection reads the remembered details; no file yet is not an
// error (nil, nil).
func LoadLastConnection() (*LastConnection, error) {
	path, err := LastConnectionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}
	var lc LastConnection
	if err := json.Unmarshal(data, &lc); err != nil {
		return nil, err
	}

	return &lc, nil
}

// SaveLastConnection writes the remembered details (best-effort:
// callers log and carry on).
func SaveLastConnection(lc LastConnection) error {
	path, err := LastConnectionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(lc)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}
