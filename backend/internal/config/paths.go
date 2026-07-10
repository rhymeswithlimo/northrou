package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir returns the OS-appropriate directory that holds config.toml.
//
//	Linux:   $XDG_CONFIG_HOME/northrou   (or ~/.config/northrou)
//	macOS:   ~/Library/Application Support/northrou
//	Windows: %ProgramData%\northrou      (falls back to %APPDATA%)
func ConfigDir() string {
	if override := os.Getenv("NORTHROU_CONFIG_DIR"); override != "" {
		return override
	}
	switch runtime.GOOS {
	case "windows":
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, appName)
		}
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, appName)
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", appName)
		}
	default: // linux and other unix
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, appName)
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", appName)
		}
	}
	// Last resort: current working directory.
	return filepath.Join(".", appName)
}

// ConfigPath is the full path to config.toml.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

// DefaultDataDir returns the default data directory (database, cached images,
// managed ffmpeg binaries, generated subtitles, HLS scratch space).
//
//	Linux:   $XDG_DATA_HOME/northrou (or ~/.local/share/northrou)
//	macOS:   ~/Library/Application Support/northrou/data
//	Windows: %ProgramData%\northrou\data
func DefaultDataDir() string {
	if override := os.Getenv("NORTHROU_DATA_DIR"); override != "" {
		return override
	}
	switch runtime.GOOS {
	case "windows", "darwin":
		return filepath.Join(ConfigDir(), "data")
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, appName)
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", appName)
		}
	}
	return filepath.Join(ConfigDir(), "data")
}
