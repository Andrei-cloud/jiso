// Package userconfig loads the XDG user configuration file, the lowest
// precedence layer of CLI-104: --flag > $JISO_* > user config > default.
package userconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvConfigVar overrides the user config file location.
const EnvConfigVar = "JISO_CONFIG"

// dirName is the per-application directory under the OS user config dir
// (XDG_CONFIG_HOME on Linux, ~/Library/Application Support on macOS).
const dirName = "jiso"

// File is the minimal user config. Keys mirror the lowercased $JISO_* env
// names; a nil field means the key is absent from the file, so a higher
// layer (flag or env) is not shadowed by a zero value. The timeout keys
// are duration strings (Go spellings: "5s", "5m"); reconnect_attempts is
// an int and hex a bool (§L settings persistence).
type File struct {
	Spec                *string `yaml:"spec"`
	File                *string `yaml:"file"`
	DB                  *string `yaml:"db"`
	Host                *string `yaml:"host"`
	Port                *string `yaml:"port"`
	Header              *string `yaml:"header"`
	TLSConfig           *string `yaml:"tls_config"`
	VisaStationID       *string `yaml:"visa_station_id"`
	JSON                *bool   `yaml:"json"`
	Quiet               *bool   `yaml:"quiet"`
	Debug               *bool   `yaml:"debug"`
	Unsecure            *bool   `yaml:"unsecure"`
	ReconnectAttempts   *int    `yaml:"reconnect_attempts"`
	ConnectTimeout      *string `yaml:"connect_timeout"`
	TotalConnectTimeout *string `yaml:"total_connect_timeout"`
	ResponseTimeout     *string `yaml:"response_timeout"`
	ListenTimeout       *string `yaml:"listen_timeout"`
	Hex                 *bool   `yaml:"hex"`
}

// Path resolves the user config location: $JISO_CONFIG when set (a leading
// ~ is expanded), otherwise <os.UserConfigDir>/jiso/config.yaml.
func Path() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvConfigVar)); p != "" {
		return expandHome(p), nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user config directory: %w", err)
	}

	return filepath.Join(dir, dirName, "config.yaml"), nil
}

// Load reads the user config file. A missing file is not an error: it
// yields an empty File. A malformed file returns an error naming the path
// so the caller can exit 3 on it (CLI-105 pattern).
func Load() (f *File, path string, err error) {
	path, err = Path()
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{}, path, nil
	}
	if err != nil {
		return nil, path, fmt.Errorf("failed to read config file: %w", err)
	}

	f = &File{}
	if err := yaml.Unmarshal(data, f); err != nil {
		return nil, path, fmt.Errorf("failed to parse config file: %w", err)
	}

	return f, path, nil
}

// expandHome expands a leading ~ to the user home directory. When the home
// directory cannot be resolved the path is returned unchanged.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}

	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
}
