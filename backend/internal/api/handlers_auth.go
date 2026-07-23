package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
	"github.com/rhymeswithlimo/northrou/backend/internal/remote"
)

// profileDTO is a viewer profile as exposed to clients.
type profileDTO struct {
	ID                    int64  `json:"id"`
	Name                  string `json:"name"`
	Avatar                string `json:"avatar,omitempty"`
	PreferredAudioLang    string `json:"preferred_audio_lang,omitempty"`
	PreferredSubtitleLang string `json:"preferred_subtitle_lang,omitempty"`
}

func toProfileDTO(p model.Profile) profileDTO {
	return profileDTO{
		ID: p.ID, Name: p.Name, Avatar: p.Avatar,
		PreferredAudioLang:    p.PreferredAudioLang,
		PreferredSubtitleLang: p.PreferredSubtitleLang,
	}
}

func toProfileDTOs(ps []model.Profile) []profileDTO {
	out := make([]profileDTO, len(ps))
	for i, p := range ps {
		out[i] = toProfileDTO(p)
	}
	return out
}

// loginResponse is returned by pair and select-profile. pair includes the full
// profile list so the client can show the picker; the tokens are scoped to
// Profile (the default until the user picks another). ServerName lets the
// client label the server it just paired with ("Living Room NAS").
type loginResponse struct {
	Profile      profileDTO   `json:"profile"`
	Profiles     []profileDTO `json:"profiles,omitempty"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresAt    string       `json:"expires_at"`
	ServerName   string       `json:"server_name,omitempty"`
}

type pairRequest struct {
	Code string `json:"code"`
	// DeviceName labels this device in the operator's paired-devices list
	// ("Kim's iPhone"). Optional; the User-Agent stands in when absent.
	DeviceName string `json:"device_name"`
	// Ephemeral asks for an access token with no stored session, so this pair
	// never appears in the paired-devices list. Honored only for trusted local
	// requests (the operator's own tooling: status, the TUI, the CLI); a
	// remote client asking for it would just be dodging the device list, so
	// there it is ignored.
	Ephemeral bool `json:"ephemeral"`
}

// handlePair exchanges the server connection code for a signed-in session and
// returns a token pair scoped to the default profile plus the profile list for
// the picker.
//
// A trusted local request (loopback or a private/LAN peer, not tunneled) needs
// no code. Everything else — a remote client over the tunnel, or a direct
// request from a public IP — must present the connection code; wrong-code
// attempts are globally rate-limited to bound guessing (per-IP throttling of the
// tunnel pairing hop lives upstream at the coordinator, which sees real IPs).
func (a *API) handlePair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if !remote.IsLocal(r) {
		if !a.pairLimiter.allow("*") {
			writeError(w, http.StatusTooManyRequests, "too many pairing attempts; try again shortly")
			return
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if !a.connectionCodeMatches(req.Code) {
			writeError(w, http.StatusUnauthorized, "invalid connection code")
			return
		}
	} else {
		// Local pairs may name themselves too; a bad body is not worth
		// rejecting a trusted request over.
		_ = decodeJSON(r, &req)
	}

	// The operator's own tooling pairs ephemerally: no stored session, no
	// entry in the paired-devices list. Local-only - a remote client gets a
	// tracked session whatever it asked for.
	issue := func() ([]model.Profile, *model.Profile, *auth.TokenPair, error) {
		if req.Ephemeral && remote.IsLocal(r) {
			return a.Auth.IssueEphemeralSession(r.Context())
		}
		return a.Auth.IssueSession(r.Context(), deviceFrom(r, req.DeviceName))
	}
	profiles, selected, pair, err := issue()
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusConflict, "server has no profiles yet; finish setup first")
			return
		}
		writeError(w, http.StatusInternalServerError, "pairing failed")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		Profile:      toProfileDTO(*selected),
		Profiles:     toProfileDTOs(profiles),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(http.TimeFormat),
		ServerName:   a.Cfg.DisplayName(),
	})
}

// deviceFrom labels the pairing device: the client's own name when it sent
// one, otherwise a trimmed User-Agent, so the paired-devices list has
// something better to show than a blank.
func deviceFrom(r *http.Request, name string) auth.Device {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(r.UserAgent())
	}
	const maxName = 120
	if len(name) > maxName {
		name = name[:maxName]
	}
	return auth.Device{Name: name}
}

// connectionCodeMatches reports whether the submitted code equals this server's
// connection code, comparing in constant time and ignoring case, spaces, and
// dashes so "nr-abcd-efgh" and "NRABCDEFGH" both match.
func (a *API) connectionCodeMatches(submitted string) bool {
	want := normalizeConnectionCode(a.Cfg.Remote.ConnectionCode)
	got := normalizeConnectionCode(submitted)
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func normalizeConnectionCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

type selectProfileRequest struct {
	RefreshToken string `json:"refresh_token"`
	ProfileID    int64  `json:"profile_id"`
}

// handleSelectProfile switches the active profile for a signed-in device and
// returns fresh tokens scoped to it. No re-auth is required.
func (a *API) handleSelectProfile(w http.ResponseWriter, r *http.Request) {
	var req selectProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prof, pair, err := a.Auth.SelectProfile(r.Context(), req.RefreshToken, req.ProfileID)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidToken) {
			writeError(w, http.StatusUnauthorized, "invalid refresh token")
			return
		}
		writeError(w, http.StatusNotFound, "no such profile")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		Profile:      toProfileDTO(*prof),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pair, err := a.Auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_ = a.Auth.Logout(r.Context(), req.RefreshToken)
	w.WriteHeader(http.StatusNoContent)
}

// meResponse tells the client who it is signed in as: the current profile, the
// full profile list for the switcher, and whether this session may administer.
// Admin is true only for trusted local requests (see remote.IsLocal); a remote
// client through the tunnel, or a direct request from a public IP, is not admin.
type meResponse struct {
	Profile    profileDTO   `json:"profile"`
	Profiles   []profileDTO `json:"profiles"`
	Admin      bool         `json:"admin"`
	ServerName string       `json:"server_name"`
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	prof, err := a.DB.GetProfile(r.Context(), claims.ProfileID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "profile not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	profiles, err := a.DB.ListProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		Profile:    toProfileDTO(*prof),
		Profiles:   toProfileDTOs(profiles),
		Admin:      remote.IsLocal(r),
		ServerName: a.Cfg.DisplayName(),
	})
}
