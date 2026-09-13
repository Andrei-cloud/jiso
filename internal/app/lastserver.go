// lastserver.go remembers the last mock-server START's parameters in
// the jiso state dir (same dir + env as the serve side-channel and the
// last-connection file), so the §G start form prefills the values the
// user last started with instead of fabricated flag defaults (UAT:
// "there should not be default values only values from previous
// testing (if any)"). Config values only fill fields the last start
// left unset; an empty form means "never started before".
package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// lastServerFile is the state-dir file name.
const lastServerFile = "last-server-start.json"

// LastServerStart is the remembered §G start-form parameter set.
type LastServerStart struct {
	Port   string `json:"port,omitempty"`
	Header string `json:"header,omitempty"`
	Spec   string `json:"spec,omitempty"`
	Routes string `json:"routes,omitempty"`
}

// LastServerStartPath is the state-dir path of the last-used file.
func LastServerStartPath() (string, error) {
	dir, err := ServeStateDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, lastServerFile), nil
}

// LoadLastServerStart reads the remembered parameters; no file yet is
// not an error (nil, nil).
func LoadLastServerStart() (*LastServerStart, error) {
	path, err := LastServerStartPath()
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

	var ls LastServerStart
	if err := json.Unmarshal(data, &ls); err != nil {
		return nil, err
	}

	return &ls, nil
}

// SaveLastServerStart writes the remembered parameters (best-effort:
// callers log and carry on).
func SaveLastServerStart(ls LastServerStart) error {
	path, err := LastServerStartPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.Marshal(ls)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}
