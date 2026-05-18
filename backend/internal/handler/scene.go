package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
)

// SceneHandler handles scene CRUD + activation via PostgreSQL and WiZ UDP.
type SceneHandler struct {
	scenes  *store.SceneStore
	devices *store.DeviceStore
	wiz     *wiz.Client
}

// NewSceneHandler creates a new SceneHandler.
func NewSceneHandler(scenes *store.SceneStore, devices *store.DeviceStore, w *wiz.Client) *SceneHandler {
	return &SceneHandler{scenes: scenes, devices: devices, wiz: w}
}

// List returns all scenes.
// @Summary List all scenes
// @Description Returns a list of all lighting scenes
// @Tags Scenes
// @Produce json
// @Param skip query int false "Skip count" default(0)
// @Param limit query int false "Limit count" default(100)
// @Success 200 {array} model.Scene
// @Failure 500 {string} string "Internal Server Error"
// @Router /scenes [get]
func (h *SceneHandler) List(w http.ResponseWriter, r *http.Request) {
	skip := queryInt(r, "skip", 0)
	limit := queryInt(r, "limit", 100)

	scenes, err := h.scenes.GetAll(r.Context(), skip, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, scenes)
}

// Get returns a single scene.
// @Summary Get a scene
// @Description Returns a single scene by its ID
// @Tags Scenes
// @Produce json
// @Param sceneID path string true "Scene ID (UUID)"
// @Success 200 {object} model.Scene
// @Failure 400 {string} string "Invalid scene ID"
// @Failure 404 {string} string "Scene not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /scenes/{sceneID} [get]
func (h *SceneHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sceneID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid scene ID")
		return
	}

	scene, err := h.scenes.GetByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if scene == nil {
		Error(w, http.StatusNotFound, "Scene not found")
		return
	}
	JSON(w, http.StatusOK, scene)
}

// Create adds a new scene with optional actions.
// @Summary Create a scene
// @Description Adds a new lighting scene with preset device actions
// @Tags Scenes
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.SceneCreate true "Scene Details"
// @Success 201 {object} model.Scene
// @Failure 400 {string} string "Invalid request body or missing name"
// @Failure 500 {string} string "Internal Server Error"
// @Router /scenes [post]
func (h *SceneHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input model.SceneCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Name == "" {
		Error(w, http.StatusBadRequest, "Name is required")
		return
	}

	scene, err := h.scenes.Create(r.Context(), input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, scene)
}

// Delete removes a scene and its actions.
// @Summary Delete a scene
// @Description Deletes a scene by its ID
// @Tags Scenes
// @Security ApiKeyAuth
// @Param sceneID path string true "Scene ID (UUID)"
// @Success 204 "No Content"
// @Failure 400 {string} string "Invalid scene ID"
// @Failure 404 {string} string "Scene not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /scenes/{sceneID} [delete]
func (h *SceneHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sceneID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid scene ID")
		return
	}

	deleted, err := h.scenes.Delete(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		Error(w, http.StatusNotFound, "Scene not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Activate sends WiZ commands to all scene devices in parallel.
// @Summary Activate a scene
// @Description Triggers all lighting actions associated with the scene
// @Tags Scenes
// @Security ApiKeyAuth
// @Param sceneID path string true "Scene ID (UUID)"
// @Success 204 "No Content"
// @Failure 400 {string} string "Invalid scene ID"
// @Failure 404 {string} string "Scene not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /scenes/{sceneID}/activate [post]
func (h *SceneHandler) Activate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sceneID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid scene ID")
		return
	}

	scene, err := h.scenes.GetByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if scene == nil {
		Error(w, http.StatusNotFound, "Scene not found")
		return
	}
	if len(scene.Actions) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ctx := r.Context()
	var wg sync.WaitGroup
	for _, action := range scene.Actions {
		wg.Add(1)
		go func(a model.SceneAction) {
			defer wg.Done()

			device, err := h.devices.GetByID(ctx, a.DeviceID)
			if err != nil || device == nil || device.IPAddress == nil {
				slog.Warn("scene activation: device not found or no IP",
					"scene", scene.Name, "device_id", a.DeviceID)
				return
			}

			on := true
			opts := wiz.StateOpts{On: &on}
			statePatch := map[string]any{"on": true}

			if v, ok := a.TargetState["brightness"]; ok {
				if b, ok := toIntVal(v); ok {
					opts.Brightness = &b
					statePatch["brightness"] = b
				}
			}
			if v, ok := a.TargetState["color_temp"]; ok {
				if ct, ok := toIntVal(v); ok {
					opts.ColorTemp = &ct
					statePatch["color_temp"] = ct
				}
			}
			if rv, ok := a.TargetState["r"]; ok {
				if r, ok := toIntVal(rv); ok {
					opts.R = &r
					statePatch["r"] = r
				}
			}
			if gv, ok := a.TargetState["g"]; ok {
				if g, ok := toIntVal(gv); ok {
					opts.G = &g
					statePatch["g"] = g
				}
			}
			if bv, ok := a.TargetState["b"]; ok {
				if b, ok := toIntVal(bv); ok {
					opts.B = &b
					statePatch["b"] = b
				}
			}

			if err := h.wiz.SetState(*device.IPAddress, opts); err != nil {
				slog.Error("WiZ command failed", "ip", *device.IPAddress, "error", err)
				return
			}
			
			if len(statePatch) > 0 {
				if err := h.devices.UpdateState(context.Background(), a.DeviceID, statePatch); err != nil {
					slog.Warn("scene state update failed", "device", a.DeviceID, "error", err)
				}
			}

			slog.Info("scene activated", "scene", scene.Name, "device", device.Name)
		}(action)
	}
	wg.Wait()

	w.WriteHeader(http.StatusNoContent)
}

// toIntVal converts a JSON number (float64 from unmarshalling) to int.
func toIntVal(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
