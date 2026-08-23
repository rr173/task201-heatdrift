package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"task201-heatdrift/internal/model"
)

// ---------- 观测点 ----------

// InsertObservation 幂等插入观测点：同 (mission_id, seq) 冲突时忽略并返回 false。
func (s *Store) InsertObservation(ctx context.Context, q queryer, o *model.Observation) (bool, error) {
	res, err := q.ExecContext(ctx, `
		INSERT INTO observations (id, mission_id, seq, ts, lat, lon, temp, status, matched_road, dist_m, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (mission_id, seq) DO NOTHING`,
		o.ID, o.MissionID, o.Seq, o.TS.Format(timeLayout), o.Lat, o.Lon, o.Temp,
		o.Status, o.MatchedRoad, o.DistM, o.CreatedAt.Format(timeLayout))
	if err != nil {
		return false, fmt.Errorf("insert observation: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetObservation 查询单个观测点。
func (s *Store) GetObservation(ctx context.Context, missionID string, seq int64) (*model.Observation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, mission_id, seq, ts, lat, lon, temp, status, matched_road, dist_m, created_at
		FROM observations WHERE mission_id = ? AND seq = ?`, missionID, seq)
	return scanObservation(row)
}

// GetObservationByID 按观测点 ID 查询。
func (s *Store) GetObservationByID(ctx context.Context, id string) (*model.Observation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, mission_id, seq, ts, lat, lon, temp, status, matched_road, dist_m, created_at
		FROM observations WHERE id = ?`, id)
	return scanObservation(row)
}

// ListObservations 按状态过滤列出观测点。
func (s *Store) ListObservations(ctx context.Context, missionID, status string, limit int) ([]*model.Observation, error) {
	q := `SELECT id, mission_id, seq, ts, lat, lon, temp, status, matched_road, dist_m, created_at
		FROM observations WHERE mission_id = ?`
	args := []any{missionID}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY seq`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query observations: %w", err)
	}
	defer rows.Close()
	var out []*model.Observation
	for rows.Next() {
		var o model.Observation
		var ts, created string
		if err := rows.Scan(&o.ID, &o.MissionID, &o.Seq, &ts, &o.Lat, &o.Lon, &o.Temp,
			&o.Status, &o.MatchedRoad, &o.DistM, &created); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		o.TS = parseTime(ts)
		o.CreatedAt = parseTime(created)
		out = append(out, &o)
	}
	return out, rows.Err()
}

// UpdateObservationStatus 更新观测点状态与匹配信息。
func (s *Store) UpdateObservationStatus(ctx context.Context, q queryer, id, status, roadID string, distM float64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE observations SET status = ?, matched_road = ?, dist_m = ? WHERE id = ?`,
		status, roadID, distM, id)
	if err != nil {
		return fmt.Errorf("update observation: %w", err)
	}
	return nil
}

// LastObservationSeq 返回任务内最大设备序号（重启续传游标来源）。
func (s *Store) LastObservationSeq(ctx context.Context, missionID string) (int64, error) {
	var seq sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM observations WHERE mission_id = ?`, missionID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("query max seq: %w", err)
	}
	return seq.Int64, nil
}

// ObservationExists 判断指定任务内某设备序号是否已存在（幂等判定）。
func (s *Store) ObservationExists(ctx context.Context, missionID string, seq int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM observations WHERE mission_id = ? AND seq = ? LIMIT 1`, missionID, seq).Scan(&one)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("query observation exists: %w", err)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanObservation(row rowScanner) (*model.Observation, error) {
	var o model.Observation
	var ts, created string
	if err := row.Scan(&o.ID, &o.MissionID, &o.Seq, &ts, &o.Lat, &o.Lon, &o.Temp,
		&o.Status, &o.MatchedRoad, &o.DistM, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan observation: %w", err)
	}
	o.TS = parseTime(ts)
	o.CreatedAt = parseTime(created)
	return &o, nil
}
