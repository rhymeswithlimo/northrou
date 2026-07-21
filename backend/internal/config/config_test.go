package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultsApplied(t *testing.T) {
	c := Default()
	if c.Server.Port == 0 {
		t.Error("expected default port")
	}
	if c.Server.DataDir == "" {
		t.Error("expected default data dir")
	}
	if c.TMDB.Language != "en-US" {
		t.Errorf("expected default language en-US, got %q", c.TMDB.Language)
	}
	// Zero-config login delivery must resolve to the hosted relay, so a fresh
	// install can email sign-in pins without the household configuring anything.
	if c.Email.RelayURL != DefaultRelayURL {
		t.Errorf("expected default relay %q, got %q", DefaultRelayURL, c.Email.RelayURL)
	}
	if c.Email.RelayDisabled {
		t.Error("relay should be enabled by default")
	}
	// The shared client token must ship by default so the hosted relay accepts
	// a fresh box; without it the relay returns 401 and no pin is delivered.
	if c.Email.RelayToken != DefaultRelayToken {
		t.Errorf("expected default relay token %q, got %q", DefaultRelayToken, c.Email.RelayToken)
	}
}

func TestRelayTokenNotDefaultedForCustomRelay(t *testing.T) {
	// A self-hoster on their own relay manages their own token; we must not
	// override it (or inject the shared one) when the URL isn't the hosted one.
	c := &Config{}
	c.Email.RelayURL = "https://relay.example.com"
	c.ApplyDefaults()
	if c.Email.RelayToken != "" {
		t.Errorf("custom relay must keep an empty token, got %q", c.Email.RelayToken)
	}

	// And an explicitly-set token on a custom relay is preserved.
	c2 := &Config{}
	c2.Email.RelayURL = "https://relay.example.com"
	c2.Email.RelayToken = "private-abc"
	c2.ApplyDefaults()
	if c2.Email.RelayToken != "private-abc" {
		t.Errorf("explicit token must be preserved, got %q", c2.Email.RelayToken)
	}
}

func TestOldHostedURLMigrated(t *testing.T) {
	// A box set up before the app/coord split wrote app.northrou.sh for both;
	// that host is now the Pages web client, so remote pairing and pin delivery
	// break until it's moved to coord.northrou.sh. ApplyDefaults migrates it.
	c := &Config{}
	c.Remote.CoordinationURL = "https://app.northrou.sh"
	c.Email.RelayURL = "https://app.northrou.sh"
	c.ApplyDefaults()
	if c.Remote.CoordinationURL != DefaultCoordinationURL {
		t.Errorf("coordination_url not migrated: %q", c.Remote.CoordinationURL)
	}
	if c.Email.RelayURL != DefaultRelayURL {
		t.Errorf("relay_url not migrated: %q", c.Email.RelayURL)
	}
	// A self-hoster's own coordinator must be left alone.
	c2 := &Config{}
	c2.Remote.CoordinationURL = "https://coord.example.com"
	c2.ApplyDefaults()
	if c2.Remote.CoordinationURL != "https://coord.example.com" {
		t.Errorf("custom coordinator must be preserved, got %q", c2.Remote.CoordinationURL)
	}
}

func TestHostedRelayTokenIsForced(t *testing.T) {
	// A box left with a stale token against the hosted relay (e.g. from an
	// earlier manual edit) must self-heal to the shared token, or it 401s
	// forever. Forcing it is safe: the hosted relay only accepts the shared one.
	c := &Config{}
	c.Email.RelayURL = DefaultRelayURL
	c.Email.RelayToken = "stale-wrong-value"
	c.ApplyDefaults()
	if c.Email.RelayToken != DefaultRelayToken {
		t.Errorf("hosted relay must force the shared token, got %q", c.Email.RelayToken)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	c := Default()
	c.Media.MovieDirs = []string{"/a", "/b"}
	c.TMDB.APIKey = "key123"
	c.Remote.Enabled = true
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Media.MovieDirs) != 2 || got.Media.MovieDirs[0] != "/a" {
		t.Errorf("movie dirs not round-tripped: %v", got.Media.MovieDirs)
	}
	if got.TMDB.APIKey != "key123" {
		t.Errorf("tmdb key not round-tripped: %q", got.TMDB.APIKey)
	}
	if !got.Remote.Enabled {
		t.Error("remote.enabled not round-tripped")
	}
}

func TestLoadOrInitMissingFile(t *testing.T) {
	_, firstRun, err := LoadOrInit(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !firstRun {
		t.Error("expected firstRun=true for missing file")
	}
}

func TestValidateRejectsBadPort(t *testing.T) {
	c := Default()
	c.Server.Port = 70000
	if err := c.Validate(); err == nil {
		t.Error("expected validation error for out-of-range port")
	}
}
