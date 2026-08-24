package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"task201-heatdrift/internal/model"
)

// ---------- 采集任务 ----------

// CreateMission 创建采集任务。
func (s *Store) CreateMission(ctx context.Context, m *model.Mission) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO missions (id, device_id, name, status, cursor, point_count, jump_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.DeviceID, m.Name, m.Status, m.Cursor, m.PointCount, m.JumpCount,
		m.CreatedAt.Format(timeLayout), m.UpdatedAt.Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert mission: %w", err)
	}
	return nil
}

// GetMission 按 ID 查询任务。
func (s *Store) GetMission(ctx context.Context, id string) (*model.Mission, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, device_id, name, status, cursor, point_count, jump_count, created_at, updated_at
		FROM missions WHERE id = ?`, id)
	var m model.Mission
	var created, updated string
	if err := row.Scan(&m.ID, &m.DeviceID, &m.Name, &m.Status, &m.Cursor,
		&m.PointCount, &m.JumpCount, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan mission: %w", err)
	}
	m.CreatedAt = parseTime(created)
	m.UpdatedAt = parseTime(updated)
	return &m, nil
}

// ListMissions 按设备列出任务。
func (s *Store) ListMissions(ctx context.Context, deviceID string) ([]*model.Mission, error) {
	q := `SELECT id, device_id, name, status, cursor, point_count, jump_count, created_at, updated_at
		FROM missions`
	var args []any
	if deviceID != "" {
		q += ` WHERE device_id = ?`
		args = append(args, deviceID)
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query missions: %w", err)
	}
	defer rows.Close()
	var out []*model.Mission
	for rows.Next() {
		var m model.Mission
		var created, updated string
		if err := rows.Scan(&m.ID, &m.DeviceID, &m.Name, &m.Status, &m.Cursor,
			&m.PointCount, &m.JumpCount, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan mission: %w", err)
		}
		m.CreatedAt = parseTime(created)
		m.UpdatedAt = parseTime(updated)
		out = append(out, &m)
	}
	return out, rows.Err()
}

// UpdateMissionCursor 在事务内推进任务游标与点数（原子）。
func (s *Store) UpdateMissionCursor(ctx context.Context, q queryer, missionID string, cursor, points int64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE missions SET cursor = ?, point_count = ?, updated_at = ?
		WHERE id = ?`, cursor, points, nowRFC3339(), missionID)
	if err != nil {
		return fmt.Errorf("update mission cursor: %w", err)
	}
	return nil
}

// IncMissionJump 增加跳点计数并迁移任务状态到 cleaning。
func (s *Store) IncMissionJump(ctx context.Context, q queryer, missionID string, jumps int64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE missions SET jump_count = jump_count + ?, status = 'cleaning', updated_at = ?
		WHERE id = ?`, jumps, nowRFC3339(), missionID)
	if err != nil {
		return fmt.Errorf("update mission jump: %w", err)
	}
	return nil
}

// SetMissionStatus 更新任务状态（自动提交）。
// 缺口（gapped）状态在此原样持久化，不被降级回 running，
// 以保证缺口事实在接收 -> 流水线 -> 持久化之间不丢失。
func (s *Store) SetMissionStatus(ctx context.Context, missionID, status string) error {
	return s.SetMissionStatusTx(ctx, s.db, missionID, status)
}

// SetMissionStatusTx 在指定事务/连接内更新任务状态，
// 供接收流程与点写入、游标推进同事务原子提交。
func (s *Store) SetMissionStatusTx(ctx context.Context, q queryer, missionID, status string) error {
	_, err := q.ExecContext(ctx, `
		UPDATE missions SET status = ?, updated_at = ? WHERE id = ?`, status, nowRFC3339(), missionID)
	if err != nil {
		return fmt.Errorf("update mission status: %w", err)
	}
	return nil
}
