package api

import (
	"net/http"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
)

// configDTO is the settings-page view of config.toml.
//
// It is deliberately a subset. Secrets never leave the box: the TMDB key and
// the SMTP password are reported as booleans ("is one set?") rather than
// echoed, so an elevated token leaking cannot also leak the household's mail
// credentials. Fields that would strand the operator (bind address, port,
// data_dir) are not editable here either; changing those from the very UI you
// are connected through is how you lock yourself out of your own server.
type configDTO struct {
	MovieDirs []string `json:"movie_dirs"`
	ShowDirs  []string `json:"show_dirs"`

	PreferSystemFFmpeg bool `json:"prefer_system_ffmpeg"`
	MaxTranscodes      int  `json:"max_transcodes"` // 0 = auto
	AllowSoftware4K    bool `json:"allow_software_4k"`
	Tonemap            bool `json:"tonemap"`

	RemoteEnabled  bool   `json:"remote_enabled"`
	ConnectionCode string `json:"connection_code,omitempty"`

	// MailMode is "smtp" when the household runs its own mail server, "relay"
	// when it uses the hosted one, or "log" when neither is available and pins
	// fall back to the server log.
	MailMode    string `json:"mail_mode"`
	SMTPHost    string `json:"smtp_host,omitempty"`
	SMTPPort    int    `json:"smtp_port,omitempty"`
	SMTPUser    string `json:"smtp_username,omitempty"`
	FromAddress string `json:"from_address,omitempty"`

	HasTMDBKey      bool `json:"has_tmdb_key"`
	HasSMTPPassword bool `json:"has_smtp_password"`
}

func mailMode(c *config.Config) string {
	if c.Email.SMTPHost != "" {
		return "smtp"
	}
	if c.Email.RelayDisabled {
		return "log"
	}
	return "relay"
}

func toConfigDTO(c *config.Config) configDTO {
	return configDTO{
		MovieDirs:          c.Media.MovieDirs,
		ShowDirs:           c.Media.ShowDirs,
		PreferSystemFFmpeg: c.Transcode.PreferSystemFFmpeg,
		MaxTranscodes:      c.Transcode.MaxTranscodes,
		AllowSoftware4K:    c.Transcode.AllowSoftware4K,
		Tonemap:            c.Transcode.Tonemap,
		RemoteEnabled:      c.Remote.Enabled,
		ConnectionCode:     c.Remote.ConnectionCode,
		MailMode:           mailMode(c),
		SMTPHost:           c.Email.SMTPHost,
		SMTPPort:           c.Email.SMTPPort,
		SMTPUser:           c.Email.SMTPUsername,
		FromAddress:        c.Email.FromAddress,
		HasTMDBKey:         c.TMDB.APIKey != "",
		HasSMTPPassword:    c.Email.SMTPPassword != "",
	}
}

// handleGetConfig returns the editable configuration. Open to any signed-in
// profile: it is status, not control, and carries no secrets.
func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toConfigDTO(a.Cfg))
}

// configPatch is a partial update. Every field is a pointer so "absent" is
// distinguishable from "set to false/empty": without that, a PATCH touching
// only movie_dirs would silently switch remote access off.
type configPatch struct {
	MovieDirs *[]string `json:"movie_dirs"`
	ShowDirs  *[]string `json:"show_dirs"`

	PreferSystemFFmpeg *bool `json:"prefer_system_ffmpeg"`
	MaxTranscodes      *int  `json:"max_transcodes"`
	AllowSoftware4K    *bool `json:"allow_software_4k"`
	Tonemap            *bool `json:"tonemap"`

	RemoteEnabled *bool `json:"remote_enabled"`

	MailMode     *string `json:"mail_mode"`
	SMTPHost     *string `json:"smtp_host"`
	SMTPPort     *int    `json:"smtp_port"`
	SMTPUser     *string `json:"smtp_username"`
	SMTPPassword *string `json:"smtp_password"`
	FromAddress  *string `json:"from_address"`

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

	cfg := *a.Cfg // copy: don't mutate live config until it validates and saves

	if p.MovieDirs != nil {
		cfg.Media.MovieDirs = *p.MovieDirs
	}
	if p.ShowDirs != nil {
		cfg.Media.ShowDirs = *p.ShowDirs
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
	if p.SMTPHost != nil {
		cfg.Email.SMTPHost = *p.SMTPHost
	}
	if p.SMTPPort != nil {
		cfg.Email.SMTPPort = *p.SMTPPort
	}
	if p.SMTPUser != nil {
		cfg.Email.SMTPUsername = *p.SMTPUser
	}
	if p.SMTPPassword != nil {
		cfg.Email.SMTPPassword = *p.SMTPPassword
	}
	if p.FromAddress != nil {
		cfg.Email.FromAddress = *p.FromAddress
	}
	if p.TMDBAPIKey != nil {
		cfg.TMDB.APIKey = *p.TMDBAPIKey
	}

	if p.MailMode != nil {
		switch *p.MailMode {
		case "relay":
			cfg.Email.RelayDisabled = false
			// SMTPHost takes precedence over the relay, so choosing the relay
			// has to clear it or the setting would appear to do nothing.
			cfg.Email.SMTPHost = ""
		case "smtp":
			cfg.Email.RelayDisabled = true
			if cfg.Email.SMTPHost == "" {
				writeError(w, http.StatusBadRequest, "smtp_host required to use your own SMTP")
				return
			}
		case "log":
			cfg.Email.RelayDisabled = true
			cfg.Email.SMTPHost = ""
		default:
			writeError(w, http.StatusBadRequest, `mail_mode must be "relay", "smtp" or "log"`)
			return
		}
	}

	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cfg.Save(a.ConfigPath); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save configuration")
		return
	}

	*a.Cfg = cfg
	a.applyConfig()

	writeJSON(w, http.StatusOK, toConfigDTO(a.Cfg))
}

// applyConfig pushes the settings that take effect without a restart into the
// running subsystems. Library dirs are read per scan, so they need nothing;
// the transcode cap is read per admission, so it does.
func (a *API) applyConfig() {
	// ffmpeg downloads in the background, so the streamer may not exist yet.
	// When it does arrive it is built from the live config, so nothing is lost.
	if s := a.getStreamer(); s != nil {
		s.Sessions().SetMaxTranscodes(a.Cfg.Transcode.MaxTranscodes)
	}
}
