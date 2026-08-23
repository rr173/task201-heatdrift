package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"task201-heatdrift/internal/model"
)

// ---------- 校正参数 ----------

// UpsertCorrection 保存任务校正参数（同任务唯一）。
func (s *Store) UpsertCorrection(ctx context.Context, c *model.CorrectionParams) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO correction_params (id, mission_id, delay_sec, temp_offset, strategy, speed_limit_kph, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (mission_id) DO UPDATE SET
			delay_sec = excluded.delay_sec,
			temp_offset = excluded.temp_offset,
			strategy = excluded.strategy,
			speed_limit_kph = excluded.speed_limit_kph`,
		c.ID, c.MissionID, c.DelaySec, c.TempOffset, c.Strategy, c.SpeedLimitKPH, c.CreatedAt.Format(timeLayout))
	if err != nil {
		return fmt.Errorf("upsert correction: %w", err)
	}
	return nil
}

// GetCorrection 查询任务校正参数；不存在返回 model.ErrNotFound。
func (s *Store) GetCorrection(ctx context.Context, missionID string) (*model.CorrectionParams, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, mission_id, delay_sec, temp_offset, strategy, speed_limit_kph, created_at
		FROM correction_params WHERE mission_id = ?`, missionID)
	var c model.CorrectionParams
	var created string
	if err := row.Scan(&c.ID, &c.MissionID, &c.DelaySec, &c.TempOffset, &c.Strategy, &c.SpeedLimitKPH, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan correction: %w", err)
	}
	c.CreatedAt = parseTime(created)
	return &c, nil
}
