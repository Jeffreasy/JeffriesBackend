package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── Room ────────────────────────────────────────────────────────────────────

type Room struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Icon        string    `json:"icon" db:"icon"`
	FloorNumber int       `json:"floor_number" db:"floor_number"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type RoomCreate struct {
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	FloorNumber int    `json:"floor_number"`
}

type RoomUpdate struct {
	Name        *string `json:"name,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	FloorNumber *int    `json:"floor_number,omitempty"`
}

// ─── Device ──────────────────────────────────────────────────────────────────

type Device struct {
	ID              uuid.UUID      `json:"id" db:"id"`
	RoomID          *uuid.UUID     `json:"room_id" db:"room_id"`
	IPAddress       *string        `json:"ip_address" db:"ip_address"`
	MACAddress      *string        `json:"mac_address" db:"mac_address"`
	MatterNodeID    int            `json:"matter_node_id" db:"matter_node_id"`
	MatterEndpoint  int            `json:"matter_endpoint_id" db:"matter_endpoint_id"`
	Name            string         `json:"name" db:"name"`
	DeviceType      string         `json:"device_type" db:"device_type"`
	Manufacturer    *string        `json:"manufacturer" db:"manufacturer"`
	Model           *string        `json:"model" db:"model"`
	FirmwareVersion *string        `json:"firmware_version" db:"firmware_version"`
	CurrentState    map[string]any `json:"current_state" db:"current_state"`
	Status          string         `json:"status" db:"status"`
	LastSeen        *time.Time     `json:"last_seen" db:"last_seen"`
	CommissionedAt  time.Time      `json:"commissioned_at" db:"commissioned_at"`
}

// DeviceResponse is the API response shape for a device (from Convex).
type DeviceResponse struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	DeviceType     string         `json:"device_type"`
	RoomID         *string        `json:"room_id"`
	IPAddress      *string        `json:"ip_address"`
	MACAddress     *string        `json:"mac_address"`
	CurrentState   map[string]any `json:"current_state"`
	Status         string         `json:"status"`
	LastSeen       *string        `json:"last_seen"`
	CommissionedAt string         `json:"commissioned_at"`
	Manufacturer   *string        `json:"manufacturer"`
	Model          *string        `json:"model"`
}

type DeviceRegisterRequest struct {
	IPAddress    string         `json:"ip_address"`
	Name         string         `json:"name"`
	RoomID       *string        `json:"room_id,omitempty"`
	SkipProbe    bool           `json:"skip_probe,omitempty"`
	Status       *string        `json:"status,omitempty"`
	CurrentState map[string]any `json:"current_state,omitempty"`
}

type DeviceCommandRequest struct {
	On              *bool `json:"on,omitempty"`
	Brightness      *int  `json:"brightness,omitempty"`
	ColorTempMireds *int  `json:"color_temp_mireds,omitempty"`
	R               *int  `json:"r,omitempty"`
	G               *int  `json:"g,omitempty"`
	B               *int  `json:"b,omitempty"`
	Hue             *int  `json:"hue,omitempty"`
	Saturation      *int  `json:"saturation,omitempty"`
	SceneID         *int  `json:"scene_id,omitempty"`
}

// ─── Scene ───────────────────────────────────────────────────────────────────

type Scene struct {
	ID        uuid.UUID     `json:"id" db:"id"`
	Name      string        `json:"name" db:"name"`
	Icon      string        `json:"icon" db:"icon"`
	ColorHex  string        `json:"color_hex" db:"color_hex"`
	CreatedAt time.Time     `json:"created_at" db:"created_at"`
	Actions   []SceneAction `json:"actions"`
}

type SceneAction struct {
	ID             uuid.UUID      `json:"id" db:"id"`
	SceneID        uuid.UUID      `json:"scene_id" db:"scene_id"`
	DeviceID       uuid.UUID      `json:"device_id" db:"device_id"`
	TargetState    map[string]any `json:"target_state" db:"target_state"`
	ExecutionOrder int            `json:"execution_order" db:"execution_order"`
	TransitionMs   int            `json:"transition_ms" db:"transition_ms"`
}

type SceneCreate struct {
	Name     string              `json:"name"`
	Icon     string              `json:"icon"`
	ColorHex string              `json:"color_hex"`
	Actions  []SceneActionCreate `json:"actions"`
}

type SceneActionCreate struct {
	DeviceID       uuid.UUID      `json:"device_id"`
	TargetState    map[string]any `json:"target_state"`
	ExecutionOrder int            `json:"execution_order"`
	TransitionMs   int            `json:"transition_ms"`
}

// ─── Automation (deprecated — managed via Convex) ────────────────────────────

type Automation struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	Name            string          `json:"name" db:"name"`
	Description     *string         `json:"description" db:"description"`
	IsEnabled       bool            `json:"is_enabled" db:"is_enabled"`
	TriggerConfig   json.RawMessage `json:"trigger_config" db:"trigger_config"`
	ConditionConfig json.RawMessage `json:"condition_config" db:"condition_config"`
	ActionConfig    json.RawMessage `json:"action_config" db:"action_config"`
	LastTriggered   *time.Time      `json:"last_triggered" db:"last_triggered"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
}

// ─── DeviceEvent ─────────────────────────────────────────────────────────────

type DeviceEvent struct {
	ID        uuid.UUID      `json:"id" db:"id"`
	Time      time.Time      `json:"time" db:"time"`
	DeviceID  uuid.UUID      `json:"device_id" db:"device_id"`
	EventType string         `json:"event_type" db:"event_type"`
	Payload   map[string]any `json:"payload" db:"payload"`
}
