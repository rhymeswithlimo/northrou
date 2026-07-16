package api

import (
	"net/http"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
)

// configDTO is the settings-page view of config.toml.
//
// It is deliberately a subset. The TMDB key never leaves the box: it is
// reported as a boolean ("is one set?") rather than echoed, so an elevated
// token leaking cannot also leak the household's credentials. Fields that would
// strand the operator (bind address, port, data_dir) are not editable here
// either; changing those from the very UI you are connected through is how you
// lock yourself out of your own server.
//
// Media folders are not here at all, in either direction. They are set on the
// box itself (`northrou admin` -> Library), because a path is a statement about
// the server's own filesystem: it can only be typed correctly by someone who
// can see that filesystem, and a client that can rewrite it can point the
// scanner anywhere the daemon can read. Read them from disk when you need them
// (see mediaDirs) rather than trusting the long-lived in-memory copy.
type configDTO struct {
	PreferSystemFFmpeg bool `json:"prefer_system_ffmpeg"`
	MaxTranscodes      int  `json:"max_transcodes"` // 0 = auto
	AllowSoftware4K    bool `json:"allow_software_4k"`
	Tonemap            bool `json:"tonemap"`

	RemoteEnabled  bool   `json:"remote_enabled"`
	ConnectionCode string `json:"connection_code,omitempty"`

	HasTMDBKey bool `json:"has_tmdb_key"`
}

func toConfigDTO(c *config.Config) configDTO {
	return configDTO{
		PreferSystemFFmpeg: c.Transcode.PreferSystemFFmpeg,
		MaxTranscodes:      c.Transcode.MaxTranscodes,
		AllowSoftware4K:    c.Transcode.AllowSoftware4K,
		Tonemap:            c.Transcode.Tonemap,
		RemoteEnabled:      c.Remote.Enabled,
		ConnectionCode:     c.Remote.ConnectionCode,
		HasTMDBKey:         c.TMDB.APIKey != "",
	}
}

// handleGetConfig returns the editable configuration. Open to any signed-in
// profile: it is status, not control, and carries no secrets.
func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toConfigDTO(a.Cfg))
}

// configPatch is a partial update. Every field is a pointer so "absent" is
// distinguishable from "set to false/empty": without that, a PATCH touching
// only the transcode cap would silently switch remote access off.
type configPatch struct {
	PreferSystemFFmpeg *bool `json:"prefer_system_ffmpeg"`
	MaxTranscodes      *int  `json:"max_transcodes"`
	AllowSoftware4K    *bool `json:"allow_software_4k"`
	Tonemap            *bool `json:"tonemap"`

	RemoteEnabled *bool `json:"remote_enabled"`

	TMDBAPIKey *string `json:"tmdb_api_key"`
}

// handlePatchConfig applies a partial configuration update. Requires an
// elevated token (wired under the admin-mutation group).
func (a *API) handlePatchConfig(w http.ResponseWriter, r *http.Request) {
	var p configPatch
	if err := decodeJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Start from what is on disk, not from the in-memory copy. The TUI edits
	// [media] in this same file while the daemon runs, so a.Cfg's Media can be
	// stale; saving a copy of it would silently revert the operator's folders.
	// Everything here is persisted, so re-reading loses nothing and also picks
	// up any other out-of-band edit.
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		// No file yet (pre-setup) or unreadable: fall back to the live config
		// rather than refusing to save.
		c := *a.Cfg
		cfg = &c
	}

	if p.PreferSystemFFmpeg != nil {
		cfg.Transcode.PreferSystemFFmpeg = *p.PreferSystemFFmpeg
	}
	if p.MaxTranscodes != nil {
		if *p.MaxTranscodes < 0 {
			writeError(w, http.StatusBadRequest, "max_transcodes must be 0 (auto) or positive")
			return
		}
		cfg.Transcode.MaxTranscodes = *p.MaxTranscodes
	}
	if p.AllowSoftware4K != nil {
		cfg.Transcode.AllowSoftware4K = *p.AllowSoftware4K
	}
	if p.Tonemap != nil {
		cfg.Transcode.Tonemap = *p.Tonemap
	}
	if p.RemoteEnabled != nil {
		cfg.Remote.Enabled = *p.RemoteEnabled
	}
	if p.TMDBAPIKey != nil {
		cfg.TMDB.APIKey = *p.TMDBAPIKey
	}

	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cfg.Save(a.ConfigPath); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save configuration")
		return
	}

	*a.Cfg = *cfg
	a.applyConfig()

	writeJSON(w, http.StatusOK, toConfigDTO(a.Cfg))
}

// mediaDirs returns the library folders as currently written on disk.
//
// The daemon must not answer this from a.Cfg: media folders are owned by the
// TUI, which writes config.toml directly while the server is running, so the
// in-memory copy goes stale the moment the operator adds a folder. Reading per
// scan is cheap (one small file, once per scan, not per request) and means a
// folder added in the TUI is picked up without restarting the service.
func (a *API) mediaDirs() (movies, shows []string) {
	if cfg, err := config.Load(a.ConfigPath); err == nil {
		return cfg.Media.MovieDirs, cfg.Media.ShowDirs
	}
	return a.Cfg.Media.MovieDirs, a.Cfg.Media.ShowDirs
}

// applyConfig pushes the settings that take effect without a restart into the
// running subsystems. Library dirs are read per scan (see mediaDirs), so they
// need nothing; the transcode cap is read per admission, so it does.
func (a *API) applyConfig() {
	// ffmpeg downloads in the background, so the streamer may not exist yet.
	// When it does arrive it is built from the live config, so nothing is lost.
	if s := a.getStreamer(); s != nil {
		s.Sessions().SetMaxTranscodes(a.Cfg.Transcode.MaxTranscodes)
	}
}
