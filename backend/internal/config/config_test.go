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
	if c.Email.SMTPHost != "" {
		t.Error("no SMTP should be configured by default")
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
