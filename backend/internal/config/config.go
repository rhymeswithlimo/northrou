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
	Update    UpdateConfig    `toml:"update"`
}

// ServerConfig covers how the HTTP daemon binds and where it stores state.
type ServerConfig struct {
	// Name is the human-facing name for this server ("Living Room NAS"),
	// chosen during setup and shown to every client that pairs with it.
	// Empty means unnamed; DisplayName falls back to the machine's hostname.
	Name string `toml:"name"`
	// BindAddr is the interface the HTTP server listens on. Empty means all
	// interfaces. Local clients connect straight here; remote clients arrive
	// over the peer-to-peer tunnel (see RemoteConfig).
	BindAddr string `toml:"bind_addr"`
	Port     int    `toml:"port"`
	// DataDir holds the SQLite database, cached images, managed ffmpeg,
	// generated subtitles, and HLS scratch space.
	DataDir string `toml:"data_dir"`
}

// DisplayName returns the configured server name, falling back to the
// machine's hostname and finally to "Northrou" so callers always have
// something presentable to show.
func (c *Config) DisplayName() string {
	if c.Server.Name != "" {
		return c.Server.Name
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "Northrou"
}

// MediaConfig lists the on-disk libraries to scan and the household's language
// preferences for playback track selection.
type MediaConfig struct {
	MovieDirs []string `toml:"movie_dirs"`
	ShowDirs  []string `toml:"show_dirs"`
	// PreferredAudioLangs / PreferredSubtitleLangs are ordered ISO-639 language
	// codes used to pick which audio track and default subtitle to serve when a
	// file has several. They default to ["en"] and are set from the settings
	// page, deliberately independent of TMDB.Language (which is metadata-only).
	PreferredAudioLangs    []string `toml:"preferred_audio_langs"`
	PreferredSubtitleLangs []string `toml:"preferred_subtitle_langs"`
}

// RemoteConfig controls peer-to-peer remote access via the coordination server.
// There is no coordinator URL knob: Northrou uses the official coordinator
// (DefaultCoordinationURL) exclusively. The ConnectionCode is the sole
// credential a remote client presents to pair with this box.
type RemoteConfig struct {
	Enabled        bool   `toml:"enabled"`
	ServerID       string `toml:"server_id"`
	ConnectionCode string `toml:"connection_code"`
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
	// ProbeDolbyVision runs a second, frame-level ffprobe to recover the Dolby
	// Vision profile when it is not in the stream side-data. Off by default (it
	// reads a frame per file); worth enabling for DV-heavy libraries so profile
	// 7 (dual-layer) is transcoded rather than mistaken for plain HDR.
	ProbeDolbyVision bool `toml:"probe_dolby_vision"`
	// PreferSystemFFmpeg uses a system-installed ffmpeg (if new enough)
	// instead of the managed download.
	PreferSystemFFmpeg bool `toml:"prefer_system_ffmpeg"`
	// MaxTranscodes overrides the concurrent-transcode cap that SessionManager
	// derives from the detected hardware. 0 = auto (the derived value), which
	// is almost always the right answer; set it to protect a box that shares
	// its CPU with other work. Cheap stream-copy paths are never counted.
	MaxTranscodes int `toml:"max_transcodes"`
}

// UpdateConfig controls the server's self-update behavior.
type UpdateConfig struct {
	// AutoUpdateDisabled turns off the background check: the server checks
	// GitHub for a newer release every few hours and, once no stream is
	// active, downloads, verifies, and applies it, then exits so the service
	// manager restarts into the new version. On by default (false means the
	// feature is enabled). Always off for dev builds and inside containers
	// regardless of this setting, since neither has a meaningful "restart into
	// the new binary" story.
	AutoUpdateDisabled bool `toml:"auto_update_disabled"`
}

// TMDBConfig holds credentials for The Movie Database metadata provider.
type TMDBConfig struct {
	APIKey   string `toml:"api_key"`
	Language string `toml:"language"`
}

// DefaultCoordinationURL is the official signaling coordinator. It is the only
// coordinator Northrou uses; there is no self-hosted-coordinator option (running
// your own broker is redundant, and a self-builder can change this constant).
const DefaultCoordinationURL = "https://coord.northrou.sh"

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
	if c.TMDB.Language == "" {
		c.TMDB.Language = "en-US"
	}
	if len(c.Media.PreferredAudioLangs) == 0 {
		c.Media.PreferredAudioLangs = []string{"en"}
	}
	if len(c.Media.PreferredSubtitleLangs) == 0 {
		c.Media.PreferredSubtitleLangs = []string{"en"}
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
