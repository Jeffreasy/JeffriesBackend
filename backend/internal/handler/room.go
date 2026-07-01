package handler

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// RoomHandler handles room CRUD via PostgreSQL.
type RoomHandler struct {
	rooms *store.RoomStore
}

// NewRoomHandler creates a new RoomHandler.
func NewRoomHandler(rooms *store.RoomStore) *RoomHandler {
	return &RoomHandler{rooms: rooms}
}

// List returns all rooms.
// @Summary List all rooms
// @Description Returns a list of all rooms in the homeapp
// @Tags Rooms
// @Produce json
// @Param skip query int false "Skip count" default(0)
// @Param limit query int false "Limit count" default(100)
// @Success 200 {array} model.Room
// @Failure 500 {string} string "Internal Server Error"
// @Router /rooms [get]
func (h *RoomHandler) List(w http.ResponseWriter, r *http.Request) {
	skip := queryInt(r, "skip", 0)
	limit := queryInt(r, "limit", 100)

	rooms, err := h.rooms.GetAll(r.Context(), skip, limit)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, rooms)
}

// Get returns a single room.
// @Summary Get a room
// @Description Returns a single room by its ID
// @Tags Rooms
// @Produce json
// @Param roomID path string true "Room ID (UUID)"
// @Success 200 {object} model.Room
// @Failure 400 {string} string "Invalid room ID"
// @Failure 404 {string} string "Room not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /rooms/{roomID} [get]
func (h *RoomHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "roomID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig kamer-id.")
		return
	}

	room, err := h.rooms.GetByID(r.Context(), id)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if room == nil {
		Error(w, http.StatusNotFound, "Kamer niet gevonden.")
		return
	}
	JSON(w, http.StatusOK, room)
}

// Create adds a new room.
// @Summary Create a room
// @Description Adds a new room to the homeapp
// @Tags Rooms
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.RoomCreate true "Room Details"
// @Success 201 {object} model.Room
// @Failure 400 {string} string "Invalid request body or missing name"
// @Failure 500 {string} string "Internal Server Error"
// @Router /rooms [post]
func (h *RoomHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input model.RoomCreate
	if err := DecodeJSON(r, &input); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if input.Name == "" {
		Error(w, http.StatusBadRequest, "Naam is verplicht.")
		return
	}

	room, err := h.rooms.Create(r.Context(), input)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, room)
}

// Update modifies room fields.
// @Summary Update a room
// @Description Modifies an existing room
// @Tags Rooms
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param roomID path string true "Room ID (UUID)"
// @Param request body model.RoomUpdate true "Updated Room Details"
// @Success 200 {object} model.Room
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Room not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /rooms/{roomID} [patch]
func (h *RoomHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "roomID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig kamer-id.")
		return
	}

	var input model.RoomUpdate
	if err := DecodeJSON(r, &input); err != nil {
		RespondDecodeError(w, err)
		return
	}

	room, err := h.rooms.Update(r.Context(), id, input)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if room == nil {
		Error(w, http.StatusNotFound, "Kamer niet gevonden.")
		return
	}
	JSON(w, http.StatusOK, room)
}

// Delete removes a room.
// @Summary Delete a room
// @Description Deletes a room by its ID
// @Tags Rooms
// @Security ApiKeyAuth
// @Param roomID path string true "Room ID (UUID)"
// @Success 204 "No Content"
// @Failure 400 {string} string "Invalid room ID"
// @Failure 404 {string} string "Room not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /rooms/{roomID} [delete]
func (h *RoomHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "roomID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig kamer-id.")
		return
	}

	deleted, err := h.rooms.Delete(r.Context(), id)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if !deleted {
		Error(w, http.StatusNotFound, "Kamer niet gevonden.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// queryInt reads an integer query parameter with a default.
func queryInt(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscan(v, &n); err != nil {
		return fallback
	}
	// Cap LIMIT/OFFSET-style params so an abusive value (e.g. limit=2000000000)
	// can't force a huge scan/allocation. 10000 is far above any real list page.
	const maxQueryInt = 10000
	if n > maxQueryInt {
		return maxQueryInt
	}
	return n
}
