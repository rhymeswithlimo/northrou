package api

import (
	"net/http"

	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
)

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if err := a.DB.PingContext(r.Context()); err != nil {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: status, Version: buildinfo.Version})
}
