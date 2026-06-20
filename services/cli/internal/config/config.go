package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds the CLI configuration.
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	OrgID int    `json:"org_id"`
}

// Path returns the path to the config file (~/.openedge/config.json).
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".openedge/config.json"
	}
	return filepath.Join(home, ".openedge", "config.json")
}

// Load reads the config file and returns the Config.
func Load() (Config, error) {
	var cfg Config
	p := Path()
	data, err := os.ReadFile(p)
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

// Save writes cfg to the config file, creating the directory if needed.
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
