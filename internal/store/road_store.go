package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"task201-heatdrift/internal/model"
)

// ---------- 道路片段 ----------

// CreateRoad 登记道路片段。
func (s *Store) CreateRoad(ctx context.Context, r *model.RoadSegment) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO road_segments (id, name, lat1, lon1, lat2, lon2, base_temp, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Lat1, r.Lon1, r.Lat2, r.Lon2, r.BaseTemp, r.Status, r.CreatedAt.Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert road: %w", err)
	}
	return nil
}

// GetRoad 查询道路。
func (s *Store) GetRoad(ctx context.Context, id string) (*model.RoadSegment, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, lat1, lon1, lat2, lon2, base_temp, status, created_at
		FROM road_segments WHERE id = ?`, id)
	var r model.RoadSegment
	var created string
	if err := row.Scan(&r.ID, &r.Name, &r.Lat1, &r.Lon1, &r.Lat2, &r.Lon2,
		&r.BaseTemp, &r.Status, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan road: %w", err)
	}
	r.CreatedAt = parseTime(created)
	return &r, nil
}

// ListRoads 列出全部道路片段。
func (s *Store) ListRoads(ctx context.Context) ([]*model.RoadSegment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, lat1, lon1, lat2, lon2, base_temp, status, created_at
		FROM road_segments ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("query roads: %w", err)
	}
	defer rows.Close()
	var out []*model.RoadSegment
	for rows.Next() {
		var r model.RoadSegment
		var created string
		if err := rows.Scan(&r.ID, &r.Name, &r.Lat1, &r.Lon1, &r.Lat2, &r.Lon2,
			&r.BaseTemp, &r.Status, &created); err != nil {
			return nil, fmt.Errorf("scan road: %w", err)
		}
		r.CreatedAt = parseTime(created)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// SetRoadStatus 更新道路状态（active/retired）。
func (s *Store) SetRoadStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE road_segments SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update road: %w", err)
	}
	return nil
}

// ---------- 道路候选 ----------

// InsertCandidate 保存候选。
func (s *Store) InsertCandidate(ctx context.Context, q queryer, c *model.RoadCandidate) error {
	chosen := 0
	if c.Chosen {
		chosen = 1
	}
	_, err := q.ExecContext(ctx, `
		INSERT INTO road_candidates (id, point_id, road_id, dist_m, proj_lat, proj_lon, ratio, score, chosen, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.PointID, c.RoadID, c.DistM, c.ProjLat, c.ProjLon, c.Ratio, c.Score, chosen, c.CreatedAt.Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert candidate: %w", err)
	}
	return nil
}

// ListCandidatesByPoint 列出某观测点的候选。
func (s *Store) ListCandidatesByPoint(ctx context.Context, pointID string) ([]*model.RoadCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, point_id, road_id, dist_m, proj_lat, proj_lon, ratio, score, chosen, created_at
		FROM road_candidates WHERE point_id = ? ORDER BY score`, pointID)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()
	var out []*model.RoadCandidate
	for rows.Next() {
		var c model.RoadCandidate
		var chosen int
		var created string
		if err := rows.Scan(&c.ID, &c.PointID, &c.RoadID, &c.DistM, &c.ProjLat, &c.ProjLon,
			&c.Ratio, &c.Score, &chosen, &created); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		c.Chosen = chosen == 1
		c.CreatedAt = parseTime(created)
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ClearCandidates 清除任务下某观测点的旧候选（重算时使用）。
func (s *Store) ClearCandidates(ctx context.Context, q queryer, pointIDs []string) error {
	for _, id := range pointIDs {
		if _, err := q.ExecContext(ctx, `DELETE FROM road_candidates WHERE point_id = ?`, id); err != nil {
			return fmt.Errorf("clear candidates: %w", err)
		}
	}
	return nil
}
