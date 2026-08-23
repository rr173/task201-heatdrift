package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"task201-heatdrift/internal/model"
)

// ---------- 设备 ----------

// CreateDevice 注册设备。
func (s *Store) CreateDevice(ctx context.Context, d *model.Device) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO devices (id, name, serial, lat, lon, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.Serial, d.Lat, d.Lon, d.Status, d.CreatedAt.Format(timeLayout))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: device serial %s exists", model.ErrConflict, d.Serial)
		}
		return fmt.Errorf("insert device: %w", err)
	}
	return nil
}

// GetDevice 按 ID 查询设备。
func (s *Store) GetDevice(ctx context.Context, id string) (*model.Device, error) {
	row := s.db.QueryRowContext(context.Background(), `
		SELECT id, name, serial, lat, lon, status, created_at FROM devices WHERE id = ?`, id)
	var d model.Device
	var created string
	if err := row.Scan(&d.ID, &d.Name, &d.Serial, &d.Lat, &d.Lon, &d.Status, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan device: %w", err)
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}

// GetDeviceBySerial 按序列号查询设备。
func (s *Store) GetDeviceBySerial(ctx context.Context, serial string) (*model.Device, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, serial, lat, lon, status, created_at FROM devices WHERE serial = ?`, serial)
	var d model.Device
	var created string
	if err := row.Scan(&d.ID, &d.Name, &d.Serial, &d.Lat, &d.Lon, &d.Status, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan device: %w", err)
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}

// ListDevices 列出全部设备。
func (s *Store) ListDevices(ctx context.Context) ([]*model.Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, serial, lat, lon, status, created_at FROM devices ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("query devices: %w", err)
	}
	defer rows.Close()
	var out []*model.Device
	for rows.Next() {
		var d model.Device
		var created string
		if err := rows.Scan(&d.ID, &d.Name, &d.Serial, &d.Lat, &d.Lon, &d.Status, &created); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		d.CreatedAt = parseTime(created)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// SetDeviceStatus 更新设备状态。
func (s *Store) SetDeviceStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update device: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}
