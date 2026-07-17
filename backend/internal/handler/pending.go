package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/engine"
	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type PendingActionHandler struct {
	store *store.PendingStore
	pool  *store.DB
	cfg   *config.Config
}

type pendingActionView struct {
	ID        string         `json:"id"`
	UserID    string         `json:"userId"`
	AgentID   string         `json:"agentId"`
	ToolName  string         `json:"toolName"`
	Args      map[string]any `json:"args"`
	Summary   string         `json:"summary"`
	Code      string         `json:"code"`
	Status    string         `json:"status"`
	ExpiresAt time.Time      `json:"expiresAt"`
	CreatedAt time.Time      `json:"createdAt"`
}

func NewPendingActionHandler(db *store.DB, cfg *config.Config) *PendingActionHandler {
	return &PendingActionHandler{
		store: store.NewPendingStore(db.Pool),
		pool:  db,
		cfg:   cfg,
	}
}

func (h *PendingActionHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := h.resolveUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	actions, err := h.store.ListPending(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	views := make([]pendingActionView, 0, len(actions))
	for _, action := range actions {
		views = append(views, toPendingActionView(action))
	}
	JSON(w, http.StatusOK, views)
}

func (h *PendingActionHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	userID := h.resolveUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	result, err := engine.ConfirmPendingAction(r.Context(), h.pool.Pool, userID, chi.URLParam(r, "id"), h.googleClient())
	if err != nil {
		if errors.Is(err, engine.ErrPendingActionNotFound) {
			// User-fixable state (already confirmed/cancelled or expired) — not a 500.
			Error(w, http.StatusConflict, err.Error())
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, result)
}

func (h *PendingActionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	userID := h.resolveUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	result, err := engine.CancelPendingAction(r.Context(), h.pool.Pool, userID, chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, engine.ErrPendingActionNotFound) {
			Error(w, http.StatusConflict, err.Error())
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, result)
}

func (h *PendingActionHandler) resolveUserID(_ *http.Request) string {
	if h.cfg == nil {
		return ""
	}
	return strings.TrimSpace(h.cfg.HomeappUserID)
}

func (h *PendingActionHandler) googleClient() *google.OAuthClient {
	if h.cfg == nil || h.cfg.GoogleClientID == "" || h.cfg.GoogleClientSecret == "" || h.cfg.GoogleRefreshToken == "" {
		return nil
	}
	return google.SharedOAuthClient(h.cfg.GoogleClientID, h.cfg.GoogleClientSecret, h.cfg.GoogleRefreshToken)
}

func toPendingActionView(action store.PendingAction) pendingActionView {
	args := map[string]any{}
	_ = json.Unmarshal([]byte(action.ArgsJSON), &args)
	return pendingActionView{
		ID:        action.ID,
		UserID:    action.UserID,
		AgentID:   action.AgentID,
		ToolName:  action.ToolName,
		Args:      args,
		Summary:   action.Summary,
		Code:      action.Code,
		Status:    action.Status,
		ExpiresAt: action.ExpiresAt,
		CreatedAt: action.CreatedAt,
	}
}
