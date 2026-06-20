package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds the CLI configuration persisted to ~/.openedge/config.json.
type Config struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	OrgID    int    `json:"org_id"`
	Insecure bool   `json:"insecure,omitempty"` // skip TLS verification (on-prem self-signed CA)
}

// Path returns the absolute path to the config file.
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".openedge/config.json"
	}
	return filepath.Join(home, ".openedge", "config.json")
}

// Load reads the config file. Returns zero-value Config if file does not exist.
func Load() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes cfg to the config file (mode 0600, directory 0700).
func Save(cfg Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

// Delete removes the config file (used by logout).
func Delete() error {
	err := os.Remove(Path())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
