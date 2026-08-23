package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"task201-heatdrift/internal/model"
)

// ---------- 轨迹段 ----------

// InsertSegment 保存轨迹段。
func (s *Store) InsertSegment(ctx context.Context, seg *model.TrackSegment) error {
	return s.insertSegment(ctx, s.db, seg)
}

// InsertSegmentTx 在事务内保存轨迹段。
func (s *Store) InsertSegmentTx(ctx context.Context, tx *sql.Tx, seg *model.TrackSegment) error {
	return s.insertSegment(ctx, tx, seg)
}

// insertSegment 在指定 queryer（连接或事务）上保存轨迹段。
func (s *Store) insertSegment(ctx context.Context, q queryer, seg *model.TrackSegment) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO track_segments (id, mission_id, road_id, start_seq, end_seq, status,
			temp_mean, temp_max, length_m, strategy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seg.ID, seg.MissionID, seg.RoadID, seg.StartSeq, seg.EndSeq, seg.Status,
		seg.TempMean, seg.TempMax, seg.LengthM, seg.Strategy,
		seg.CreatedAt.Format(timeLayout), seg.UpdatedAt.Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert segment: %w", err)
	}
	return nil
}

// GetSegment 查询轨迹段。
func (s *Store) GetSegment(ctx context.Context, id string) (*model.TrackSegment, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, mission_id, road_id, start_seq, end_seq, status, temp_mean, temp_max, length_m, strategy, created_at, updated_at
		FROM track_segments WHERE id = ?`, id)
	return scanSegment(row)
}

// ListSegments 列出任务下轨迹段。
func (s *Store) ListSegments(ctx context.Context, missionID string) ([]*model.TrackSegment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, mission_id, road_id, start_seq, end_seq, status, temp_mean, temp_max, length_m, strategy, created_at, updated_at
		FROM track_segments WHERE mission_id = ? ORDER BY start_seq`, missionID)
	if err != nil {
		return nil, fmt.Errorf("query segments: %w", err)
	}
	defer rows.Close()
	var out []*model.TrackSegment
	for rows.Next() {
		var seg model.TrackSegment
		var created, updated string
		if err := rows.Scan(&seg.ID, &seg.MissionID, &seg.RoadID, &seg.StartSeq, &seg.EndSeq,
			&seg.Status, &seg.TempMean, &seg.TempMax, &seg.LengthM, &seg.Strategy,
			&created, &updated); err != nil {
			return nil, fmt.Errorf("scan segment: %w", err)
		}
		seg.CreatedAt = parseTime(created)
		seg.UpdatedAt = parseTime(updated)
		out = append(out, &seg)
	}
	return out, rows.Err()
}

// SetSegmentStatus 更新轨迹段状态。
func (s *Store) SetSegmentStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE track_segments SET status = ?, updated_at = ? WHERE id = ?`, status, nowRFC3339(), id)
	if err != nil {
		return fmt.Errorf("update segment: %w", err)
	}
	return nil
}

// DeleteSegments 删除任务下全部轨迹段（重算前清理）。
func (s *Store) DeleteSegments(ctx context.Context, q queryer, missionID string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM track_segments WHERE mission_id = ?`, missionID); err != nil {
		return fmt.Errorf("delete segments: %w", err)
	}
	return nil
}

// ---------- 复核标注 ----------

// InsertAnnotation 保存标注。
func (s *Store) InsertAnnotation(ctx context.Context, a *model.Annotation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO annotations (id, segment_id, author, note, decision, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.SegmentID, a.Author, a.Note, a.Decision, a.CreatedAt.Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert annotation: %w", err)
	}
	return nil
}

// ListAnnotations 列出轨迹段的标注。
func (s *Store) ListAnnotations(ctx context.Context, segmentID string) ([]*model.Annotation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, segment_id, author, note, decision, created_at
		FROM annotations WHERE segment_id = ? ORDER BY created_at`, segmentID)
	if err != nil {
		return nil, fmt.Errorf("query annotations: %w", err)
	}
	defer rows.Close()
	var out []*model.Annotation
	for rows.Next() {
		var a model.Annotation
		var created string
		if err := rows.Scan(&a.ID, &a.SegmentID, &a.Author, &a.Note, &a.Decision, &created); err != nil {
			return nil, fmt.Errorf("scan annotation: %w", err)
		}
		a.CreatedAt = parseTime(created)
		out = append(out, &a)
	}
	return out, rows.Err()
}

func scanSegment(row rowScanner) (*model.TrackSegment, error) {
	var seg model.TrackSegment
	var created, updated string
	if err := row.Scan(&seg.ID, &seg.MissionID, &seg.RoadID, &seg.StartSeq, &seg.EndSeq,
		&seg.Status, &seg.TempMean, &seg.TempMax, &seg.LengthM, &seg.Strategy,
		&created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan segment: %w", err)
	}
	seg.CreatedAt = parseTime(created)
	seg.UpdatedAt = parseTime(updated)
	return &seg, nil
}
