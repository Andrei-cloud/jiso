package config

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TLSFileConfig defines the structure for a consolidated TLS JSON configuration file
type TLSFileConfig struct {
	Enabled            bool   `json:"enabled"`
	ClientCert         string `json:"client_cert"`
	ClientKey          string `json:"client_key"`
	ServerCert         string `json:"server_cert,omitempty"`
	ServerKey          string `json:"server_key,omitempty"`
	CACert             string `json:"ca_cert"`
	ServerName         string `json:"server_name,omitempty"`
	MinVersion         string `json:"min_version,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`

	// Directory path where the config file is located, used to resolve relative cert paths
	baseDir string `json:"-"`
}

// LoadTLSConfig reads, parses, and validates a consolidated TLS JSON configuration file
func LoadTLSConfig(configPath string) (*TLSFileConfig, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, fmt.Errorf("empty TLS configuration file path")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read TLS config file '%s': %w", configPath, err)
	}

	var cfg TLSFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TLS config file '%s': %w", configPath, err)
	}

	cfg.baseDir = filepath.Dir(configPath)

	if cfg.Enabled {
		// Resolve relative certificate file paths relative to the config file directory
		cfg.ClientCert = resolvePath(cfg.baseDir, cfg.ClientCert)
		cfg.ClientKey = resolvePath(cfg.baseDir, cfg.ClientKey)
		cfg.ServerCert = resolvePath(cfg.baseDir, cfg.ServerCert)
		cfg.ServerKey = resolvePath(cfg.baseDir, cfg.ServerKey)
		cfg.CACert = resolvePath(cfg.baseDir, cfg.CACert)

		// Validate file existence for all specified certificate files
		if cfg.ClientCert != "" {
			if _, err := os.Stat(cfg.ClientCert); os.IsNotExist(err) {
				return nil, fmt.Errorf("client certificate file does not exist: %s", cfg.ClientCert)
			}
		}
		if cfg.ClientKey != "" {
			if _, err := os.Stat(cfg.ClientKey); os.IsNotExist(err) {
				return nil, fmt.Errorf("client private key file does not exist: %s", cfg.ClientKey)
			}
		}
		if cfg.ServerCert != "" {
			if _, err := os.Stat(cfg.ServerCert); os.IsNotExist(err) {
				return nil, fmt.Errorf("server certificate file does not exist: %s", cfg.ServerCert)
			}
		}
		if cfg.ServerKey != "" {
			if _, err := os.Stat(cfg.ServerKey); os.IsNotExist(err) {
				return nil, fmt.Errorf("server private key file does not exist: %s", cfg.ServerKey)
			}
		}
		if cfg.CACert != "" {
			if _, err := os.Stat(cfg.CACert); os.IsNotExist(err) {
				return nil, fmt.Errorf("CA certificate file does not exist: %s", cfg.CACert)
			}
		}
	}

	return &cfg, nil
}

// BuildCryptoTLSConfig generates a standard Go *tls.Config instance for client connections.
func (t *TLSFileConfig) BuildCryptoTLSConfig() (*tls.Config, error) {
	tlsCfg, err := t.buildBaseTLSConfig()
	if err != nil {
		return nil, err
	}
	if tlsCfg == nil {
		return nil, nil
	}

	if t.ClientCert != "" && t.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(t.ClientCert, t.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate key pair (PEM): %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

// BuildServerTLSConfig generates a standard Go *tls.Config instance for TLS server listeners.
func (t *TLSFileConfig) BuildServerTLSConfig() (*tls.Config, error) {
	tlsCfg, err := t.buildBaseTLSConfig()
	if err != nil {
		return nil, err
	}
	if tlsCfg == nil {
		return nil, nil
	}

	if t.ServerCert == "" || t.ServerKey == "" {
		return nil, fmt.Errorf("server_cert and server_key are required when TLS is enabled for server mode")
	}

	cert, err := tls.LoadX509KeyPair(t.ServerCert, t.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate key pair (PEM): %w", err)
	}
	tlsCfg.Certificates = []tls.Certificate{cert}

	return tlsCfg, nil
}

func (t *TLSFileConfig) buildBaseTLSConfig() (*tls.Config, error) {
	if t == nil || !t.Enabled {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		ServerName:         t.ServerName,
		InsecureSkipVerify: t.InsecureSkipVerify,
	}

	// Parse TLS minimum version
	switch strings.TrimSpace(t.MinVersion) {
	case "1.3":
		tlsCfg.MinVersion = tls.VersionTLS13
	case "1.2", "":
		tlsCfg.MinVersion = tls.VersionTLS12
	default:
		return nil, fmt.Errorf("unsupported TLS min_version '%s' (must be '1.2' or '1.3')", t.MinVersion)
	}

	// Load Root/Intermediate CA pool in PEM format
	if t.CACert != "" {
		caData, err := os.ReadFile(t.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file '%s': %w", t.CACert, err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse CA certificate PEM data from '%s'", t.CACert)
		}
		tlsCfg.RootCAs = caPool
		tlsCfg.ClientCAs = caPool
	}

	return tlsCfg, nil
}

func resolvePath(baseDir, pathStr string) string {
	pathStr = strings.TrimSpace(pathStr)
	if pathStr == "" {
		return ""
	}
	if filepath.IsAbs(pathStr) {
		return pathStr
	}
	return filepath.Join(baseDir, pathStr)
}
