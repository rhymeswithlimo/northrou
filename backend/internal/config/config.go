// Package config defines Northrou's single-file TOML configuration, its
// defaults, loading/saving, validation, and first-run detection.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

const appName = "northrou"

// Config is the whole of Northrou's persisted configuration. It maps 1:1 to
// config.toml. Every field has a sane zero-value default applied by
// ApplyDefaults so a minimal (or empty) file still boots.
type Config struct {
	Server    ServerConfig    `toml:"server"`
	Media     MediaConfig     `toml:"media"`
	Remote    RemoteConfig    `toml:"remote"`
	Transcode TranscodeConfig `toml:"transcode"`
	TMDB      TMDBConfig      `toml:"tmdb"`
}

// ServerConfig covers how the HTTP daemon binds and where it stores state.
type ServerConfig struct {
	// BindAddr is the interface the HTTP server listens on. Empty means all
	// interfaces. Local clients connect straight here; remote clients arrive
	// over the peer-to-peer tunnel (see RemoteConfig).
	BindAddr string `toml:"bind_addr"`
	Port     int    `toml:"port"`
	// DataDir holds the SQLite database, cached images, managed ffmpeg,
	// generated subtitles, and HLS scratch space.
	DataDir string `toml:"data_dir"`
}

// MediaConfig lists the on-disk libraries to scan.
type MediaConfig struct {
	MovieDirs []string `toml:"movie_dirs"`
	ShowDirs  []string `toml:"show_dirs"`
}

// RemoteConfig controls peer-to-peer remote access via the coordination server.
type RemoteConfig struct {
	Enabled            bool   `toml:"enabled"`
	CoordinationURL    string `toml:"coordination_url"`
	SelfHostedCoord    bool   `toml:"self_hosted_coordinator"`
	ServerID           string `toml:"server_id"`
	ConnectionCode     string `toml:"connection_code"`
}

// TranscodeConfig tunes the streaming/transcoding decision cascade.
type TranscodeConfig struct {
	// HWAccel overrides hardware-acceleration auto-detection. Empty means
	// auto-detect; "none" forces software; otherwise one of the known
	// backends ("nvenc", "qsv", "videotoolbox", "amf", "vaapi").
	HWAccel string `toml:"hw_accel"`
	// AllowSoftware4K permits real-time software transcoding of 4K sources.
	// Off by default because it is not real-time on most hardware.
	AllowSoftware4K bool `toml:"allow_software_4k"`
	// MaxBitrateKbps caps the top HLS rung for remote streams. 0 = uncapped.
	MaxBitrateKbps int `toml:"max_bitrate_kbps"`
	// Tonemap enables HDR->SDR tone mapping when transcoding for SDR clients.
	Tonemap bool `toml:"tonemap"`
	// PreferSystemFFmpeg uses a system-installed ffmpeg (if new enough)
	// instead of the managed download.
	PreferSystemFFmpeg bool `toml:"prefer_system_ffmpeg"`
}

// TMDBConfig holds credentials for The Movie Database metadata provider.
type TMDBConfig struct {
	APIKey   string `toml:"api_key"`
	Language string `toml:"language"`
}

// Default returns a fully-populated Config with defaults applied. It does not
// touch disk.
func Default() *Config {
	c := &Config{}
	c.ApplyDefaults()
	return c
}

// ApplyDefaults fills any unset field with its default. Idempotent.
func (c *Config) ApplyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8674 // "NORT" ~ arbitrary, memorable, avoids common ports
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = DefaultDataDir()
	}
	if c.Remote.CoordinationURL == "" {
		c.Remote.CoordinationURL = "https://coord.northrou.app"
	}
	if c.TMDB.Language == "" {
		c.TMDB.Language = "en-US"
	}
}

// Validate returns an error if the configuration cannot produce a working
// server. It is intentionally lenient about optional subsystems (remote,
// TMDB) that only warn when unconfigured.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port %d out of range", c.Server.Port)
	}
	if c.Server.DataDir == "" {
		return errors.New("server.data_dir must be set")
	}
	if c.Remote.Enabled && c.Remote.CoordinationURL == "" {
		return errors.New("remote.enabled is true but remote.coordination_url is empty")
	}
	return nil
}

// Load reads config.toml from the given path, applies defaults, and validates.
// A missing file is reported via os.IsNotExist so callers can trigger the
// first-run setup wizard.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := toml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// LoadOrInit loads config.toml, or if it does not exist returns a default
// Config with firstRun=true so the caller can launch the setup wizard.
func LoadOrInit(path string) (cfg *Config, firstRun bool, err error) {
	cfg, err = Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), true, nil
		}
		return nil, false, err
	}
	return cfg, false, nil
}

// Save writes the config to path as TOML, creating parent directories.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Exists reports whether a config file is present at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
