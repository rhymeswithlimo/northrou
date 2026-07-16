package api

import (
	"errors"
	"net/http"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
)

// oauthConfigDTO tells the client which sign-in methods this server actually
// offers, so the login page can show the buttons that work and hide the ones
// that don't. A household with no broker configured gets the pin, which needs
// no setup and works offline.
type oauthConfigDTO struct {
	Providers []string `json:"providers"`
	StartURL  string   `json:"start_url,omitempty"`
}

func (a *API) handleOAuthConfig(w http.ResponseWriter, r *http.Request) {
	if a.OAuth == nil {
		writeJSON(w, http.StatusOK, oauthConfigDTO{Providers: []string{}})
		return
	}
	writeJSON(w, http.StatusOK, oauthConfigDTO{
		Providers: a.Cfg.Auth.OAuthProviders,
		StartURL:  a.Cfg.Auth.OAuthIssuer + "/oauth",
	})
}

type oauthSignInRequest struct {
	Assertion string `json:"assertion"`
	Nonce     string `json:"nonce"`
}

// handleOAuthSignIn exchanges a broker assertion for a session.
func (a *API) handleOAuthSignIn(w http.ResponseWriter, r *http.Request) {
	if a.OAuth == nil {
		writeError(w, http.StatusNotFound, "social sign-in is not enabled on this server")
		return
	}

	var req oauthSignInRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Assertion == "" || req.Nonce == "" {
		writeError(w, http.StatusBadRequest, "assertion and nonce are required")
		return
	}

	profiles, selected, pair, err := a.Auth.SignInWithAssertion(r.Context(), a.OAuth, req.Assertion, req.Nonce)
	switch {
	case errors.Is(err, auth.ErrNotAccountEmail):
		// Worth its own message: the sign-in genuinely worked, it is just not
		// this household's address, and "invalid code" would send someone
		// hunting for a problem that isn't there.
		writeError(w, http.StatusForbidden, "that account is not this server's account")
		return
	case err != nil:
		writeError(w, http.StatusUnauthorized, "sign-in failed")
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
