package handler

import (
	"net/http"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// HealthHandler handles health check requests.
type HealthHandler struct {
	db *store.DB
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db *store.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Check returns 200 when API + database are healthy, 503 when degraded.
// @Summary Health Check
// @Description Returns the health status of the API and database.
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{} "status ok"
// @Failure 503 {object} map[string]interface{} "status degraded"
// @Router /health [get]
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(r.Context()); err != nil {
		JSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":  "degraded",
			"service": "homeapp-api",
			"db":      "error",
			"detail":  err.Error(),
		})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "homeapp-api",
		"db":      "ok",
	})
}
