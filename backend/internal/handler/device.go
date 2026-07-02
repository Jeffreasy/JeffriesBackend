package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
)

// validDeviceIP requires a concrete private LAN IP literal before the address is
// ever dialed over UDP: it rejects hostnames (which would do DNS per command),
// host:port forms, loopback/multicast/broadcast/unspecified, and any public IP —
// a WiZ lamp always lives on the local network.
func validDeviceIP(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// DeviceHandler handles device operations (PostgreSQL + WiZ UDP).
type DeviceHandler struct {
	devices     *store.DeviceStore
	commands    *store.DeviceCommandStore
	wiz         *wiz.Client
	userID      string
	commandMode string
}

// NewDeviceHandler creates a new DeviceHandler.
func NewDeviceHandler(devices *store.DeviceStore, commands *store.DeviceCommandStore, w *wiz.Client, userID, commandMode string) *DeviceHandler {
	return &DeviceHandler{devices: devices, commands: commands, wiz: w, userID: userID, commandMode: commandMode}
}

// List returns all devices from PostgreSQL.
// @Summary List all devices
// @Description Returns a list of all devices in the homeapp
// @Tags Devices
// @Produce json
// @Param skip query int false "Skip count" default(0)
// @Param limit query int false "Limit count" default(100)
// @Success 200 {array} model.DeviceResponse
// @Failure 500 {string} string "Failed to fetch devices"
// @Router /devices [get]
func (h *DeviceHandler) List(w http.ResponseWriter, r *http.Request) {
	skip := queryInt(r, "skip", 0)
	limit := queryInt(r, "limit", 100)

	devices, err := h.devices.GetAll(r.Context(), skip, limit)
	if err != nil {
		InternalError(w, r, fmt.Errorf("fetch devices: %w", err))
		return
	}

	responses := make([]model.DeviceResponse, 0, len(devices))
	for _, d := range devices {
		responses = append(responses, mapDeviceModel(d))
	}
	JSON(w, http.StatusOK, responses)
}

// Get returns a single device.
// @Summary Get a device
// @Description Returns a single device by its ID
// @Tags Devices
// @Produce json
// @Param deviceID path string true "Device ID (UUID)"
// @Success 200 {object} model.DeviceResponse
// @Failure 400 {string} string "Invalid device ID"
// @Failure 404 {string} string "Device not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /devices/{deviceID} [get]
func (h *DeviceHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig apparaat-id.")
		return
	}

	d, err := h.devices.GetByID(r.Context(), id)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if d == nil {
		Error(w, http.StatusNotFound, "Apparaat niet gevonden.")
		return
	}
	JSON(w, http.StatusOK, mapDeviceModel(*d))
}

// Update patches a device.
// @Summary Update a device
// @Description Modifies an existing device
// @Tags Devices
// @Accept json
// @Produce json
// @Param deviceID path string true "Device ID (UUID)"
// @Param request body map[string]interface{} true "Updated Device Details"
// @Success 200 {object} model.DeviceResponse
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Device not found"
// @Failure 500 {string} string "Internal Server Error"
// @Failure 502 {string} string "WiZ lamp unreachable"
// @Router /devices/{deviceID} [patch]
func (h *DeviceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig apparaat-id.")
		return
	}

	var data map[string]any
	if err := DecodeJSON(r, &data); err != nil {
		RespondDecodeError(w, err)
		return
	}

	d, err := h.devices.GetByID(r.Context(), id)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if d == nil {
		Error(w, http.StatusNotFound, "Apparaat niet gevonden.")
		return
	}

	if name, ok := data["name"].(string); ok {
		name = strings.TrimSpace(name)
		if name == "" {
			Error(w, http.StatusBadRequest, "Naam mag niet leeg zijn.")
			return
		}
		d.Name = name
	}

	// Verify IP if changing
	if newIP, ok := data["ip_address"].(string); ok && newIP != "" {
		newIP = strings.TrimSpace(newIP)
		if newIP == "" {
			Error(w, http.StatusBadRequest, "ip_address mag niet leeg zijn.")
			return
		}
		if !validDeviceIP(newIP) {
			Error(w, http.StatusBadRequest, "ip_address moet een geldig privaat LAN-adres zijn (geen hostnaam/publiek IP)")
			return
		}
		if !h.queueLightCommands() {
			if _, err := h.wiz.GetState(newIP); err != nil {
				Error(w, http.StatusBadGateway, "WiZ lamp op "+newIP+" niet bereikbaar.")
				return
			}
		}
		d.IPAddress = &newIP
	} else if _, ok := data["ip_address"]; ok && data["ip_address"] == nil {
		d.IPAddress = nil
	}

	if v, ok := data["room_id"]; ok {
		if v == nil {
			d.RoomID = nil
		} else if roomID, ok := v.(string); ok {
			roomID = strings.TrimSpace(roomID)
			if roomID == "" {
				d.RoomID = nil
			} else {
				rid, err := uuid.Parse(roomID)
				if err != nil {
					Error(w, http.StatusBadRequest, "Ongeldig room_id.")
					return
				}
				d.RoomID = &rid
			}
		} else {
			Error(w, http.StatusBadRequest, "room_id moet een UUID-string of null zijn.")
			return
		}
	}

	updated, err := h.devices.UpdateMetadata(r.Context(), *d)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if updated == nil {
		Error(w, http.StatusNotFound, "Apparaat niet gevonden.")
		return
	}
	JSON(w, http.StatusOK, mapDeviceModel(*updated))
}

// Delete removes a device.
// @Summary Delete a device
// @Description Deletes a device by its ID
// @Tags Devices
// @Param deviceID path string true "Device ID (UUID)"
// @Success 204 "No Content"
// @Failure 400 {string} string "Invalid device ID"
// @Failure 404 {string} string "Device not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /devices/{deviceID} [delete]
func (h *DeviceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig apparaat-id.")
		return
	}

	deleted, err := h.devices.Delete(r.Context(), id)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if !deleted {
		Error(w, http.StatusNotFound, "Apparaat niet gevonden.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Register verifies a WiZ bulb via UDP, then stores it in PostgreSQL.
// @Summary Register a WiZ device
// @Description Registers a new WiZ bulb via local UDP verification
// @Tags Devices
// @Accept json
// @Produce json
// @Param request body model.DeviceRegisterRequest true "Device Registration Details"
// @Success 201 {object} model.DeviceResponse
// @Failure 400 {string} string "Invalid request body or missing IP/name"
// @Failure 500 {string} string "Internal Server Error"
// @Failure 502 {string} string "Cannot reach WiZ bulb"
// @Router /devices/register [post]
func (h *DeviceHandler) Register(w http.ResponseWriter, r *http.Request) {
	var input model.DeviceRegisterRequest
	if err := DecodeJSON(r, &input); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if input.IPAddress == "" || input.Name == "" {
		Error(w, http.StatusBadRequest, "ip_address en naam zijn verplicht.")
		return
	}
	if !validDeviceIP(input.IPAddress) {
		Error(w, http.StatusBadRequest, "ip_address moet een geldig privaat LAN-adres zijn (geen hostnaam/publiek IP)")
		return
	}

	existing, err := h.devices.GetByIP(r.Context(), input.IPAddress)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if existing != nil {
		JSON(w, http.StatusOK, mapDeviceModel(*existing))
		return
	}

	dt := "color_light"
	mfr := "WiZ"
	mdl := "GU10 Color"
	currentState := input.CurrentState
	status := ""
	if input.Status != nil {
		status = strings.TrimSpace(*input.Status)
	}
	if status == "" {
		status = "registered"
	}

	shouldProbe := !input.SkipProbe && !strings.EqualFold(h.commandMode, "queue")
	if shouldProbe {
		state, err := h.wiz.GetState(input.IPAddress)
		if err != nil {
			Error(w, http.StatusBadGateway, "WiZ-lamp op "+input.IPAddress+":38899 is niet bereikbaar.")
			return
		}
		currentState = map[string]any{
			"on":         state.On,
			"brightness": state.Brightness,
			"color_temp": state.ColorTemp,
			"r":          state.R,
			"g":          state.G,
			"b":          state.B,
		}
		status = "online"
	} else if currentState == nil {
		currentState = map[string]any{
			"on":         false,
			"brightness": 0,
		}
	}

	d := model.Device{
		Name:         input.Name,
		IPAddress:    &input.IPAddress,
		DeviceType:   dt,
		Manufacturer: &mfr,
		Model:        &mdl,
		CurrentState: currentState,
		Status:       status,
	}
	if input.RoomID != nil {
		rid, err := uuid.Parse(*input.RoomID)
		if err == nil {
			d.RoomID = &rid
		}
	}

	created, err := h.devices.Create(r.Context(), d)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	slog.Info("registered WiZ bulb", "name", input.Name, "ip", input.IPAddress)
	JSON(w, http.StatusCreated, mapDeviceModel(*created))
}

// Command sends a control command to a WiZ bulb via local UDP.
// @Summary Send command to WiZ device
// @Description Controls a WiZ bulb via UDP (on/off, brightness, color, scene)
// @Tags Devices
// @Accept json
// @Produce json
// @Param deviceID path string true "Device ID (UUID)"
// @Param request body model.DeviceCommandRequest true "Command Details"
// @Success 204 "No Content"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Device not found"
// @Failure 422 {string} string "Device has no IP address"
// @Failure 502 {string} string "WiZ command failed"
// @Router /devices/{deviceID}/command [post]
func (h *DeviceHandler) Command(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "deviceID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig apparaat-id.")
		return
	}

	var cmd model.DeviceCommandRequest
	if err := DecodeJSON(r, &cmd); err != nil {
		RespondDecodeError(w, err)
		return
	}

	d, err := h.devices.GetByID(r.Context(), id)
	if err != nil || d == nil {
		Error(w, http.StatusNotFound, "Apparaat niet gevonden.")
		return
	}
	if d.IPAddress == nil || *d.IPAddress == "" {
		Error(w, http.StatusUnprocessableEntity, "Device heeft geen IP-adres.")
		return
	}
	ip := *d.IPAddress

	opts := wiz.StateOpts{}
	statePatch := map[string]any{}

	if cmd.On != nil {
		opts.On = cmd.On
		statePatch["on"] = *cmd.On
	}
	if cmd.Brightness != nil {
		// Clamp to the lamp's own range so the optimistic DB copy can't display a
		// value (e.g. 5) the bulb will never actually sit at (it floors to 10).
		b := clampInt(*cmd.Brightness, 10, 100)
		opts.Brightness = &b
		statePatch["brightness"] = b
	}
	if cmd.ColorTempMireds != nil {
		kelvin := clampInt(int(math.Round(1_000_000.0/float64(*cmd.ColorTempMireds))), 2200, 6500)
		opts.ColorTemp = &kelvin
		statePatch["color_temp"] = kelvin
		// Mirror the frontend simulation (lib/deviceCommands.ts): switching to
		// white mode clears any RGB color and scene, otherwise the jsonb-merge
		// keeps the old r/g/b and the card snaps back to the previous color on
		// the next refetch (N8).
		statePatch["r"] = 0
		statePatch["g"] = 0
		statePatch["b"] = 0
		statePatch["scene_id"] = 0
	}

	if cmd.Hue != nil && cmd.Saturation != nil {
		rv, gv, bv := hsvToRGB(float64(*cmd.Hue)/254.0, float64(*cmd.Saturation)/254.0, 1.0)
		opts.R = &rv
		opts.G = &gv
		opts.B = &bv
		statePatch["r"] = rv
		statePatch["g"] = gv
		statePatch["b"] = bv
		statePatch["scene_id"] = 0
	} else if cmd.R != nil || cmd.G != nil || cmd.B != nil {
		rv := clampInt(derefOr(cmd.R, 0), 0, 255)
		gv := clampInt(derefOr(cmd.G, 0), 0, 255)
		bv := clampInt(derefOr(cmd.B, 0), 0, 255)
		opts.R = &rv
		opts.G = &gv
		opts.B = &bv
		statePatch["r"] = rv
		statePatch["g"] = gv
		statePatch["b"] = bv
		statePatch["scene_id"] = 0
	}

	if cmd.SceneID != nil {
		statePatch["on"] = true
		// Persist the scene so the UI's scene-highlight survives a refetch (N8).
		statePatch["scene_id"] = *cmd.SceneID
	}

	if h.queueLightCommands() {
		raw, err := json.Marshal(cmd)
		if err != nil {
			Error(w, http.StatusInternalServerError, "Command serialiseren mislukt")
			return
		}
		if _, err := h.commands.Create(r.Context(), h.userID, &id, raw); err != nil {
			InternalError(w, r, fmt.Errorf("command queue: %w", err))
			return
		}
		if len(statePatch) > 0 {
			go func() {
				if err := h.devices.UpdateState(context.Background(), id, statePatch); err != nil {
					slog.Warn("optimistic state update failed", "device", id, "error", err)
				}
			}()
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if cmd.SceneID != nil {
		if err := h.wiz.SetScene(ip, *cmd.SceneID); err != nil {
			slog.Error("WiZ command failed", "device", id, "ip", ip, "error", err)
			Error(w, http.StatusBadGateway, "WiZ-commando mislukt — de lamp reageerde niet.")
			return
		}
	} else {
		if err := h.wiz.SetState(ip, opts); err != nil {
			slog.Error("WiZ command failed", "device", id, "ip", ip, "error", err)
			Error(w, http.StatusBadGateway, "WiZ-commando mislukt — de lamp reageerde niet.")
			return
		}
	}

	// Update state in PostgreSQL
	if len(statePatch) > 0 {
		go func() {
			if err := h.devices.UpdateState(context.Background(), id, statePatch); err != nil {
				slog.Warn("state update failed", "device", id, "error", err)
			}
		}()
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *DeviceHandler) queueLightCommands() bool {
	return strings.EqualFold(h.commandMode, "queue")
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func mapDeviceModel(d model.Device) model.DeviceResponse {
	resp := model.DeviceResponse{
		ID:           d.ID.String(),
		Name:         d.Name,
		DeviceType:   orDefault(d.DeviceType, "color_light"),
		CurrentState: d.CurrentState,
		Status:       orDefault(d.Status, "offline"),
	}
	if d.RoomID != nil {
		s := d.RoomID.String()
		resp.RoomID = &s
	}
	if d.IPAddress != nil {
		resp.IPAddress = d.IPAddress
	}
	if d.LastSeen != nil {
		s := d.LastSeen.Format("2006-01-02T15:04:05Z")
		resp.LastSeen = &s
	}
	if d.Manufacturer != nil {
		resp.Manufacturer = d.Manufacturer
	}
	if d.Model != nil {
		resp.Model = d.Model
	}
	if !d.CommissionedAt.IsZero() {
		resp.CommissionedAt = d.CommissionedAt.Format("2006-01-02T15:04:05Z")
	}
	return resp
}

func orDefault(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

func derefOr(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}

func hsvToRGB(h, s, v float64) (int, int, int) {
	if s == 0 {
		c := int(v * 255)
		return c, c, c
	}
	h *= 6
	i := int(h)
	f := h - float64(i)
	p := v * (1 - s)
	q := v * (1 - s*f)
	t := v * (1 - s*(1-f))

	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	case 5:
		r, g, b = v, p, q
	}
	return int(r * 255), int(g * 255), int(b * 255)
}

// queryInt is defined in helpers — keep for standalone compilation
func init() {
	_ = json.Marshal
}
