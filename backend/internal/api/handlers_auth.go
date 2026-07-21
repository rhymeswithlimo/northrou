package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// accountDTO is the household's auth-root email.
type accountDTO struct {
	Email string `json:"email"`
}

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

type requestPinRequest struct {
	Email string `json:"email"`
}

// handleRequestPin emails a one-time sign-in pin to the address if it is the
// account address. It always returns 200 with the same body so callers cannot
// use it to discover the account email.
func (a *API) handleRequestPin(w http.ResponseWriter, r *http.Request) {
	var req requestPinRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}
	if err := a.Auth.RequestLoginPin(r.Context(), req.Email); err != nil {
		// Log server-side but do not surface: the response is identical whether
		// or not the address is the account's.
		slog.Warn("request pin failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "If that email is the account address, a sign-in code has been sent.",
	})
}

type verifyPinRequest struct {
	Email string `json:"email"`
	Pin   string `json:"pin"`
}

// loginResponse is returned by verify-pin and select-profile. verify-pin
// includes the full profile list so the client can show the picker; the tokens
// are scoped to Profile (the default until the user picks another).
type loginResponse struct {
	Profile      profileDTO   `json:"profile"`
	Profiles     []profileDTO `json:"profiles,omitempty"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresAt    string       `json:"expires_at"`
}

// handleVerifyPin exchanges a valid sign-in pin for a token pair scoped to the
// default profile, and returns the profile list for the picker.
func (a *API) handleVerifyPin(w http.ResponseWriter, r *http.Request) {
	var req verifyPinRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	profiles, selected, pair, err := a.Auth.VerifyLoginPin(r.Context(), req.Email, strings.TrimSpace(req.Pin))
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid or expired code")
			return
		}
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		Profile:      toProfileDTO(*selected),
		Profiles:     toProfileDTOs(profiles),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

type selectProfileRequest struct {
	RefreshToken string `json:"refresh_token"`
	ProfileID    int64  `json:"profile_id"`
}

// handleSelectProfile switches the active profile for a signed-in device and
// returns fresh tokens scoped to it. No pin is required.
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

// meResponse tells the client who it is signed in as: the account email, the
// current profile, and the full profile list for the switcher.
type meResponse struct {
	Account  accountDTO   `json:"account"`
	Profile  profileDTO   `json:"profile"`
	Profiles []profileDTO `json:"profiles"`
	Admin    bool         `json:"admin"`
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
	acct, err := a.DB.GetAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	profiles, err := a.DB.ListProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		Account:  accountDTO{Email: acct.Email},
		Profile:  toProfileDTO(*prof),
		Profiles: toProfileDTOs(profiles),
		Admin:    claims.Admin,
	})
}

// handleRequestAdminOTP emails an admin-elevation code to the account address.
// Any signed-in profile may request it; whoever has the emailed code can
// elevate. The response is generic so it reveals nothing.
func (a *API) handleRequestAdminOTP(w http.ResponseWriter, r *http.Request) {
	if err := a.Auth.RequestAdminOTP(r.Context()); err != nil {
		slog.Warn("request admin otp failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "An admin code has been sent to the account email.",
	})
}

type verifyAdminOTPRequest struct {
	OTP string `json:"otp"`
}

type adminElevationResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   string `json:"expires_at"`
}

// handleVerifyAdminOTP exchanges a valid admin code for a short-lived elevated
// access token, scoped to the calling profile. The client uses it as the bearer
// for admin mutations until it expires.
func (a *API) handleVerifyAdminOTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req verifyAdminOTPRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token, exp, err := a.Auth.VerifyAdminOTP(r.Context(), claims.ProfileID, strings.TrimSpace(req.OTP))
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid or expired code")
			return
		}
		writeError(w, http.StatusInternalServerError, "elevation failed")
		return
	}
	writeJSON(w, http.StatusOK, adminElevationResponse{
		AccessToken: token,
		ExpiresAt:   exp.UTC().Format(http.TimeFormat),
	})
}
