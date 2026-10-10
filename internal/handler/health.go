package handler

import (
	"net/http"

	"criaisis/internal/server"
)

// HealthHandler reports process and dependency health for load balancers.
type HealthHandler struct {
	srv *server.Server
}

// NewHealthHandler wires the health probe.
func NewHealthHandler(srv *server.Server) *HealthHandler {
	return &HealthHandler{srv: srv}
}

// Live reports that the process is up. It intentionally touches no dependency, so
// a database blip cannot cause an orchestrator to kill a healthy process.
func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready reports whether the process can serve traffic, which requires the database.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.srv.DB.Pool.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
