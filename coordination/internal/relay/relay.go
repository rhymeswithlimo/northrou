// Package relay is Northrou's hosted sign-in-pin mail sender. Home servers keep
// their accounts and pins entirely local; they call this service only to
// deliver the pin email, so a household does not have to run its own SMTP. The
// relay never sees or stores accounts and cannot tell a real address from a
// fabricated one, so it is protected by rate limiting and input validation, not
// authentication.
//
// The load-bearing control is the PER-RECIPIENT limit: it is what stops the
// relay from being abused to spam or phish a third party's inbox with sign-in
// codes. Per-server and global limits protect the operator's cost and sender
// reputation.
//
// State (the rate-limit counters) is in-memory and process-local. Running more
// than one instance splits the counters, making the caps effectively
// per-instance; externalize them before scaling horizontally.
package relay

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// Limits configures the three rate-limit tiers.
type Limits struct {
	PerRecipientWindow time.Duration
	PerRecipientMax    int
	PerServerWindow    time.Duration
	PerServerMax       int
	GlobalWindow       time.Duration
	GlobalMax          int
}

// DefaultLimits is a conservative starting point. The per-recipient cap is the
// tightest: at most one code every 45s and a low daily ceiling per address.
func DefaultLimits() Limits {
	return Limits{
		PerRecipientWindow: 45 * time.Second,
		PerRecipientMax:    1,
		PerServerWindow:    time.Hour,
		PerServerMax:       30,
		GlobalWindow:       time.Hour,
		GlobalMax:          5000,
	}
}

// Handler is the relay HTTP handler.
type Handler struct {
	provider Provider
	token    string // optional shared token; empty disables the check
	perRcpt  *limiter
	perDaily *limiter
	perSrv   *limiter
	global   *limiter
}

// NewHandler builds a relay handler. A non-empty token requires callers to
// present it as a bearer token; understand this is a weak control (the token
// ships in an open-source client) that only deters trivial scanning.
func NewHandler(provider Provider, limits Limits, token string) *Handler {
	h := &Handler{
		provider: provider,
		token:    token,
		perRcpt:  newLimiter(limits.PerRecipientWindow, limits.PerRecipientMax),
		perDaily: newLimiter(24*time.Hour, 8),
		perSrv:   newLimiter(limits.PerServerWindow, limits.PerServerMax),
		global:   newLimiter(limits.GlobalWindow, limits.GlobalMax),
	}
	go h.pruneLoop()
	return h
}

// Routes registers the relay endpoints on mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/pin/send", h.handleSendPin)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

type sendPinRequest struct {
	ServerID string `json:"server_id"`
	Email    string `json:"email"`
	Pin      string `json:"pin"`
}

var pinRE = regexp.MustCompile(`^[0-9]{6}$`)

func (h *Handler) handleSendPin(w http.ResponseWriter, r *http.Request) {
	if h.token != "" && bearer(r) != h.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req sendPinRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	serverID := strings.TrimSpace(req.ServerID)
	if serverID == "" || !validEmail(email) || !pinRE.MatchString(req.Pin) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Per-recipient first (the control that protects third parties), then the
	// daily recipient ceiling, then per-server and global (which protect the
	// operator). A rejection at any tier is a 429 and sends nothing.
	if !h.perRcpt.allow(email) || !h.perDaily.allow(email) {
		w.Header().Set("Retry-After", "45")
		http.Error(w, "too many requests for this recipient", http.StatusTooManyRequests)
		return
	}
	if !h.perSrv.allow(serverID) {
		http.Error(w, "too many requests for this server", http.StatusTooManyRequests)
		return
	}
	if !h.global.allow("*") {
		http.Error(w, "relay is temporarily over capacity", http.StatusTooManyRequests)
		return
	}

	subject, body := renderPin(req.Pin)
	if err := h.provider.Send(r.Context(), email, subject, body); err != nil {
		// Do not echo provider detail (or the pin) to the caller.
		slog.Warn("relay delivery failed", "server_id", serverID, "err", err)
		http.Error(w, "delivery failed", http.StatusBadGateway)
		return
	}
	slog.Info("relay delivered sign-in pin", "server_id", serverID)
	w.WriteHeader(http.StatusAccepted)
}

// renderPin is the single source of truth for the pin email, so the template
// can change without updating deployed home servers.
func renderPin(pin string) (subject, body string) {
	subject = "Your Northrou sign-in code"
	body = fmt.Sprintf(
		"Your Northrou sign-in code is:\r\n\r\n    %s\r\n\r\n"+
			"It expires in 10 minutes. If you did not request it, ignore this email.\r\n",
		pin)
	return subject, body
}

func (h *Handler) pruneLoop() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		h.perRcpt.prune()
		h.perDaily.prune()
		h.perSrv.prune()
		h.global.prune()
	}
}

func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

func bearer(r *http.Request) string {
	if after, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}
