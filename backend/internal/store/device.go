package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// DeviceStore handles all device database operations.
type DeviceStore struct {
	db *DB
}

// NewDeviceStore creates a new DeviceStore.
func NewDeviceStore(db *DB) *DeviceStore {
	return &DeviceStore{db: db}
}

// GetAll returns all devices with optional pagination.
func (s *DeviceStore) GetAll(ctx context.Context, skip, limit int) ([]model.Device, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, room_id, ip_address, mac_address, matter_node_id, matter_endpoint_id,
		        name, device_type, manufacturer, model, firmware_version,
		        current_state, status, last_seen, commissioned_at
		 FROM devices
		 ORDER BY name
		 OFFSET $1 LIMIT $2`,
		skip, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return pgx.CollectRows(rows, scanDevice)
}

// GetByID returns a single device or nil.
func (s *DeviceStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Device, error) {
	row := s.db.Pool.QueryRow(ctx,
		`SELECT id, room_id, ip_address, mac_address, matter_node_id, matter_endpoint_id,
		        name, device_type, manufacturer, model, firmware_version,
		        current_state, status, last_seen, commissioned_at
		 FROM devices WHERE id = $1`, id,
	)
	d, err := scanDeviceRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// GetByIP returns a device by its IP address.
func (s *DeviceStore) GetByIP(ctx context.Context, ip string) (*model.Device, error) {
	row := s.db.Pool.QueryRow(ctx,
		`SELECT id, room_id, ip_address, mac_address, matter_node_id, matter_endpoint_id,
		        name, device_type, manufacturer, model, firmware_version,
		        current_state, status, last_seen, commissioned_at
		 FROM devices WHERE ip_address = $1`, ip,
	)
	d, err := scanDeviceRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// Create inserts a new device.
func (s *DeviceStore) Create(ctx context.Context, d model.Device) (*model.Device, error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.CommissionedAt.IsZero() {
		d.CommissionedAt = time.Now().UTC()
	}

	stateJSON, err := json.Marshal(d.CurrentState)
	if err != nil {
		return nil, err
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO devices (id, room_id, ip_address, mac_address, matter_node_id, matter_endpoint_id,
		                      name, device_type, manufacturer, model, firmware_version,
		                      current_state, status, last_seen, commissioned_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		d.ID, d.RoomID, d.IPAddress, d.MACAddress, d.MatterNodeID, d.MatterEndpoint,
		d.Name, d.DeviceType, d.Manufacturer, d.Model, d.FirmwareVersion,
		stateJSON, d.Status, d.LastSeen, d.CommissionedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateState merges a partial state update into current_state JSONB.
func (s *DeviceStore) UpdateState(ctx context.Context, id uuid.UUID, patch map[string]any) error {
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE devices
		 SET current_state = current_state || $2::jsonb,
		     last_seen = $3
		 WHERE id = $1`,
		id, patchJSON, time.Now().UTC(),
	)
	return err
}

// SetStatus updates the device status.
func (s *DeviceStore) SetStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE devices SET status = $2, last_seen = $3 WHERE id = $1`,
		id, status, time.Now().UTC(),
	)
	return err
}

// Delete removes a device by ID.
func (s *DeviceStore) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// --- scan helpers ---

func scanDevice(row pgx.CollectableRow) (model.Device, error) {
	var d model.Device
	var stateJSON []byte
	err := row.Scan(
		&d.ID, &d.RoomID, &d.IPAddress, &d.MACAddress, &d.MatterNodeID, &d.MatterEndpoint,
		&d.Name, &d.DeviceType, &d.Manufacturer, &d.Model, &d.FirmwareVersion,
		&stateJSON, &d.Status, &d.LastSeen, &d.CommissionedAt,
	)
	if err != nil {
		return d, err
	}
	if len(stateJSON) > 0 {
		_ = json.Unmarshal(stateJSON, &d.CurrentState)
	}
	return d, nil
}

func scanDeviceRow(row pgx.Row) (*model.Device, error) {
	var d model.Device
	var stateJSON []byte
	err := row.Scan(
		&d.ID, &d.RoomID, &d.IPAddress, &d.MACAddress, &d.MatterNodeID, &d.MatterEndpoint,
		&d.Name, &d.DeviceType, &d.Manufacturer, &d.Model, &d.FirmwareVersion,
		&stateJSON, &d.Status, &d.LastSeen, &d.CommissionedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(stateJSON) > 0 {
		_ = json.Unmarshal(stateJSON, &d.CurrentState)
	}
	return &d, nil
}
