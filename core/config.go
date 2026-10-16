package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// PreferredInstallDir is where a fresh install goes on Windows. A new folder
// directly under C:\ is allowed for standard users (no admin prompt).
const PreferredInstallDir = `C:\Allods2`

// DefaultInstallDir returns C:\Allods2 when it can be written to, otherwise
// %USERPROFILE%\Games\Allods2. note says why the fallback was used ("" if not).
// It never leaves an empty C:\Allods2 behind.
func DefaultInstallDir() (dir, note string) {
	fallback := func() string {
		h, err := os.UserHomeDir()
		if err != nil {
			return `C:\Games\Allods2`
		}
		return filepath.Join(h, "Games", "Allods2")
	}
	if runtime.GOOS != "windows" {
		return fallback(), ""
	}
	if err := probeWritable(PreferredInstallDir); err != nil {
		return fallback(), fmt.Sprintf("default folder %s not writable (%v); using %s", PreferredInstallDir, err, fallback())
	}
	return PreferredInstallDir, ""
}

// probeWritable checks dir can hold files: an existing dir gets a test file,
// a missing one is created and removed again.
func probeWritable(dir string) error {
	if fi, err := os.Stat(dir); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("%s is a file", dir)
		}
		f, err := os.CreateTemp(dir, ".a2probe-*")
		if err != nil {
			return err
		}
		f.Close()
		return os.Remove(f.Name())
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	return os.Remove(dir)
}

// GameInstalled reports whether dir already holds a game (allods2.exe).
func GameInstalled(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, "allods2.exe"))
	return err == nil && !fi.IsDir()
}
