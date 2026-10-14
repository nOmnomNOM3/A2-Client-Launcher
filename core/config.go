package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is remembered between runs in %APPDATA%\A2Launcher\config.json.
type Config struct {
	InstallDir  string `json:"install_dir"`
	ManifestURL string `json:"manifest_url,omitempty"` // override for testing
}

func configPath() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "A2Launcher", "config.json")
}

func LoadConfig() Config {
	var c Config
	if b, err := os.ReadFile(configPath()); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func SaveConfig(c Config) error {
	p := configPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// DefaultInstallDir avoids Program Files (needs admin, and Windows redirects writes).
func DefaultInstallDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return `C:\Games\Rage of Mages 2`
	}
	return filepath.Join(h, "Games", "Rage of Mages 2")
}
