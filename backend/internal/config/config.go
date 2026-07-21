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
	Email     EmailConfig     `toml:"email"`
	Auth      AuthConfig      `toml:"auth"`
	Update    UpdateConfig    `toml:"update"`
}

// AuthConfig turns on social sign-in. It is off by default: the emailed pin
// needs no setup, works with no internet, and proves exactly the same thing.
//
// The box never holds an OAuth client secret. Google and Apple require a
// registered client with fixed redirect URIs, which a self-hosted server at an
// arbitrary address cannot have, so the credentials live on the coordination
// broker and the box only verifies the short-lived assertions it signs.
type AuthConfig struct {
	// OAuthIssuer is the broker's base URL. Empty disables social sign-in.
	OAuthIssuer string `toml:"oauth_issuer"`
	// OAuthProviders lists what to offer, e.g. ["google", "apple"].
	OAuthProviders []string `toml:"oauth_providers"`
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
	Enabled         bool   `toml:"enabled"`
	CoordinationURL string `toml:"coordination_url"`
	SelfHostedCoord bool   `toml:"self_hosted_coordinator"`
	ServerID        string `toml:"server_id"`
	ConnectionCode  string `toml:"connection_code"`
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
	// manager restarts into the new version. On by default (like
	// EmailConfig.RelayDisabled, false means the feature is enabled). Always
	// off for dev builds and inside containers regardless of this setting,
	// since neither has a meaningful "restart into the new binary" story.
	AutoUpdateDisabled bool `toml:"auto_update_disabled"`
}

// TMDBConfig holds credentials for The Movie Database metadata provider.
type TMDBConfig struct {
	APIKey   string `toml:"api_key"`
	Language string `toml:"language"`
}

// DefaultRelayURL is the hosted pin-delivery relay used out of the box, so a
// household does not have to run its own mail server. See internal/email.
const DefaultRelayURL = "https://coord.northrou.sh"

// DefaultRelayToken is the shared bearer token every build presents to the
// hosted relay (DefaultRelayURL). It is deliberately NOT a secret: the relay
// itself documents this token as "a weak control that ships in an open-source
// client" whose only job is to deter trivial scanning of /v1/pin/send. The
// relay's real anti-abuse protection is its per-recipient rate limiting, not
// this value, which is why it can live here in the clear. Shipping it is what
// makes sign-in work out of the box; without it every fresh box got an
// otherwise-silent HTTP 401 from the relay. A self-hoster running their own
// relay sets a private relay_token (and matching RELAY_TOKEN) instead.
const DefaultRelayToken = "northrou-hosted-relay-v1"

// EmailConfig controls how one-time login pins are delivered. Delivery is the
// coordination relay's job: it owns the mail infrastructure and the template,
// so a household never runs a mail server to sign in. If the relay is turned
// off or unreachable, pins are logged to the server log for local single-box
// use.
//
// There is deliberately no SMTP option here. Running mail is the one piece of
// self-hosting that reliably fails (SPF/DKIM/DMARC, IP reputation, port 25
// blocked by most ISPs), and a sign-in code that silently lands in spam locks
// you out of your own server. The relay carries only an address and a pin.
type EmailConfig struct {
	// RelayURL is the hosted pin-delivery service. Defaults to DefaultRelayURL.
	// Ignored when RelayDisabled is true.
	RelayURL string `toml:"relay_url"`
	// RelayToken is an optional bearer token presented to the relay.
	RelayToken string `toml:"relay_token"`
	// RelayDisabled turns the hosted relay off, falling back to logging pins.
	// For an air-gapped box, or a household that would rather read the pin out
	// of the server log than have it touch anyone else's infrastructure.
	RelayDisabled bool `toml:"relay_disabled"`
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
		c.Remote.CoordinationURL = "https://coord.northrou.sh"
	}
	if c.TMDB.Language == "" {
		c.TMDB.Language = "en-US"
	}
	if !c.Email.RelayDisabled {
		if c.Email.RelayURL == "" {
			c.Email.RelayURL = DefaultRelayURL
		}
		// Talking to the hosted relay always means the shared client token, so
		// force it rather than only defaulting an empty value. A custom token
		// against the hosted relay is always wrong (it only accepts the shared
		// one), and forcing here self-heals a box left with a stale relay_token
		// from earlier manual config, which would otherwise 401 forever with no
		// pin ever delivered. A self-hoster on their own relay sets a different
		// relay_url, so their token is left untouched.
		if c.Email.RelayURL == DefaultRelayURL {
			c.Email.RelayToken = DefaultRelayToken
		}
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
