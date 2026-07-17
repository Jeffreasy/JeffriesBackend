package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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

// ListDevices exposes only the device fields the LAN bridge needs. It is mounted
// under the bridge-only key, avoiding use of the all-powerful application key.
func (h *BridgeHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	_ = h.commands.TouchBridge(r.Context())
	devices, err := h.devices.GetAll(r.Context(), 0, 500)
	if err != nil {
		InternalError(w, r, fmt.Errorf("bridge device fetch: %w", err))
		return
	}
	result := make([]bridgeDevice, 0, len(devices))
	for _, device := range devices {
		if mapped, ok := mapBridgeDevice(device); ok {
			result = append(result, mapped)
		}
	}
	JSON(w, http.StatusOK, result)
}
func (h *BridgeHandler) ClaimCommands(w http.ResponseWriter, r *http.Request) {
	_ = h.commands.TouchBridge(r.Context()) // bridge liveness heartbeat
	var input bridgeClaimRequest
	if err := DecodeJSON(r, &input); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 25
	}

	commands, err := h.commands.ClaimPending(r.Context(), input.Limit)
	if err != nil {
		InternalError(w, r, fmt.Errorf("command claim: %w", err))
		return
	}
	if len(commands) == 0 {
		JSON(w, http.StatusOK, bridgeClaimResponse{Commands: []bridgeCommand{}})
		return
	}

	devices, err := h.devices.GetAll(r.Context(), 0, 500)
	if err != nil {
		InternalError(w, r, fmt.Errorf("device fetch: %w", err))
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
	_ = h.commands.TouchBridge(r.Context()) // bridge liveness heartbeat
	id, err := uuid.Parse(chi.URLParam(r, "commandID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig command-id.")
		return
	}

	var input bridgeCompleteRequest
	if err := DecodeJSON(r, &input); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if input.Status == "" {
		input.Status = store.DeviceCommandStatusDone
	}
	if input.Status != store.DeviceCommandStatusDone && input.Status != store.DeviceCommandStatusFailed {
		Error(w, http.StatusBadRequest, "Ongeldige commandostatus.")
		return
	}

	// A reported failure is requeued for a few attempts before becoming terminal,
	// so a transient LAN/UDP blip doesn't permanently fail the command.
	if input.Status == store.DeviceCommandStatusFailed {
		requeued, deviceID, err := h.commands.RequeueOrFail(r.Context(), id, 3)
		if err != nil {
			InternalError(w, r, fmt.Errorf("command completion: %w", err))
			return
		}
		// Terminal failure (3rd attempt): mark the target device offline so the
		// UI stops showing the optimistic state as if the command landed (N7 —
		// queue mode answers 204 before execution, so this is the only signal
		// the frontend gets that the lamp never reacted).
		if !requeued && deviceID != nil {
			if serr := h.devices.SetStatus(r.Context(), *deviceID, "offline"); serr != nil {
				slog.Warn("failed to mark device offline after terminal command failure",
					"commandID", id, "deviceID", *deviceID, "error", serr)
			} else {
				slog.Warn("device command failed terminally; device marked offline",
					"commandID", id, "deviceID", *deviceID)
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.commands.MarkDone(r.Context(), id, input.Status); err != nil {
		InternalError(w, r, fmt.Errorf("command completion: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *BridgeHandler) UpdateDeviceStatus(w http.ResponseWriter, r *http.Request) {
	_ = h.commands.TouchBridge(r.Context()) // bridge liveness heartbeat
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig apparaat-id.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var input bridgeDeviceStatusRequest
	if err := DecodeJSON(r, &input); err != nil {
		RespondDecodeError(w, err)
		return
	}
	status, currentState, err := validateBridgeDeviceStatus(input)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	if status != "" {
		if err := h.devices.SetStatus(r.Context(), id, status); err != nil {
			InternalError(w, r, fmt.Errorf("status update: %w", err))
			return
		}
	}
	if currentState != nil {
		if err := h.devices.UpdateState(r.Context(), id, currentState); err != nil {
			InternalError(w, r, fmt.Errorf("state update: %w", err))
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func validateBridgeDeviceStatus(input bridgeDeviceStatusRequest) (string, map[string]any, error) {
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status != "" && status != "online" && status != "offline" {
		return "", nil, fmt.Errorf("Ongeldige apparaatstatus.")
	}
	if input.CurrentState == nil {
		return status, nil, nil
	}

	state := make(map[string]any, len(input.CurrentState))
	for key, raw := range input.CurrentState {
		switch key {
		case "on":
			value, ok := raw.(bool)
			if !ok {
				return "", nil, fmt.Errorf("Ongeldige waarde voor current_state.%s.", key)
			}
			state[key] = value
		case "brightness":
			value, ok := toIntVal(raw)
			if !ok || value < 0 || value > 100 {
				return "", nil, fmt.Errorf("Ongeldige waarde voor current_state.%s.", key)
			}
			state[key] = value
		case "color_temp":
			value, ok := toIntVal(raw)
			if !ok || (value != 0 && (value < 2200 || value > 6500)) {
				return "", nil, fmt.Errorf("Ongeldige waarde voor current_state.%s.", key)
			}
			state[key] = value
		case "r", "g", "b":
			value, ok := toIntVal(raw)
			if !ok || value < 0 || value > 255 {
				return "", nil, fmt.Errorf("Ongeldige waarde voor current_state.%s.", key)
			}
			state[key] = value
		case "scene_id":
			value, ok := toIntVal(raw)
			if !ok || value < 0 || value > 32 {
				return "", nil, fmt.Errorf("Ongeldige waarde voor current_state.%s.", key)
			}
			state[key] = value
		default:
			return "", nil, fmt.Errorf("Onbekend current_state-veld: %s.", key)
		}
	}
	return status, state, nil
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
