package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// BridgeHandler exposes a small cloud API for the local LAN bridge.
type BridgeHandler struct {
	devices  *store.DeviceStore
	commands *store.DeviceCommandStore
}

func NewBridgeHandler(devices *store.DeviceStore, commands *store.DeviceCommandStore) *BridgeHandler {
	return &BridgeHandler{devices: devices, commands: commands}
}

type bridgeClaimRequest struct {
	Limit int `json:"limit,omitempty"`
}

type bridgeDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IPAddress  string `json:"ip_address"`
	DeviceType string `json:"device_type"`
}

type bridgeCommand struct {
	ID       string          `json:"id"`
	DeviceID *string         `json:"device_id,omitempty"`
	Command  json.RawMessage `json:"command"`
	Devices  []bridgeDevice  `json:"devices"`
}

type bridgeClaimResponse struct {
	Commands []bridgeCommand `json:"commands"`
}

type bridgeCompleteRequest struct {
	Status string `json:"status"`
}

type bridgeDeviceStatusRequest struct {
	Status       string         `json:"status,omitempty"`
	CurrentState map[string]any `json:"current_state,omitempty"`
}

func (h *BridgeHandler) ClaimCommands(w http.ResponseWriter, r *http.Request) {
	var input bridgeClaimRequest
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 25
	}

	commands, err := h.commands.ClaimPending(r.Context(), input.Limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Command claim failed: "+err.Error())
		return
	}
	if len(commands) == 0 {
		JSON(w, http.StatusOK, bridgeClaimResponse{Commands: []bridgeCommand{}})
		return
	}

	devices, err := h.devices.GetAll(r.Context(), 0, 500)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Device fetch failed: "+err.Error())
		return
	}

	deviceMap := make(map[uuid.UUID]bridgeDevice, len(devices))
	allDevices := make([]bridgeDevice, 0, len(devices))
	for _, d := range devices {
		bd, ok := mapBridgeDevice(d)
		if !ok {
			continue
		}
		deviceMap[d.ID] = bd
		allDevices = append(allDevices, bd)
	}

	result := make([]bridgeCommand, 0, len(commands))
	for _, cmd := range commands {
		targets := allDevices
		var deviceID *string
		if cmd.DeviceID != nil {
			id := cmd.DeviceID.String()
			deviceID = &id
			if d, ok := deviceMap[*cmd.DeviceID]; ok {
				targets = []bridgeDevice{d}
			} else {
				_ = h.commands.MarkDone(r.Context(), cmd.ID, store.DeviceCommandStatusFailed)
				continue
			}
		}
		if len(targets) == 0 {
			_ = h.commands.MarkDone(r.Context(), cmd.ID, store.DeviceCommandStatusFailed)
			continue
		}
		result = append(result, bridgeCommand{
			ID:       cmd.ID.String(),
			DeviceID: deviceID,
			Command:  cmd.Command,
			Devices:  targets,
		})
	}

	JSON(w, http.StatusOK, bridgeClaimResponse{Commands: result})
}

func (h *BridgeHandler) CompleteCommand(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "commandID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid command ID")
		return
	}

	var input bridgeCompleteRequest
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Status == "" {
		input.Status = store.DeviceCommandStatusDone
	}
	if input.Status != store.DeviceCommandStatusDone && input.Status != store.DeviceCommandStatusFailed {
		Error(w, http.StatusBadRequest, "Invalid command status")
		return
	}

	if err := h.commands.MarkDone(r.Context(), id, input.Status); err != nil {
		Error(w, http.StatusInternalServerError, "Command completion failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *BridgeHandler) UpdateDeviceStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid device ID")
		return
	}

	var input bridgeDeviceStatusRequest
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.Status != "" {
		if err := h.devices.SetStatus(r.Context(), id, input.Status); err != nil {
			Error(w, http.StatusInternalServerError, "Status update failed: "+err.Error())
			return
		}
	}
	if input.CurrentState != nil {
		if err := h.devices.UpdateState(r.Context(), id, input.CurrentState); err != nil {
			Error(w, http.StatusInternalServerError, "State update failed: "+err.Error())
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func mapBridgeDevice(d model.Device) (bridgeDevice, bool) {
	if d.IPAddress == nil || *d.IPAddress == "" {
		return bridgeDevice{}, false
	}
	dt := d.DeviceType
	if dt == "" {
		dt = "color_light"
	}
	return bridgeDevice{
		ID:         d.ID.String(),
		Name:       d.Name,
		IPAddress:  *d.IPAddress,
		DeviceType: dt,
	}, true
}
