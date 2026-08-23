package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"task201-heatdrift/internal/model"
)

// ---------- 版本 ----------

// CreateVersion 创建版本（返回版本号）。
func (s *Store) CreateVersion(ctx context.Context, v *model.Version) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO versions (id, mission_id, number, status, strategy, delay_sec, segment_count, point_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.MissionID, v.Number, v.Status, v.Strategy, v.DelaySec,
		v.SegmentCount, v.PointCount, v.CreatedAt.Format(timeLayout))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: version number %d for mission %s", model.ErrConflict, v.Number, v.MissionID)
		}
		return fmt.Errorf("insert version: %w", err)
	}
	return nil
}

// NextVersionNumber 返回任务下一个版本号。
func (s *Store) NextVersionNumber(ctx context.Context, missionID string) (int, error) {
	var n sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(number) FROM versions WHERE mission_id = ?`, missionID).Scan(&n); err != nil {
		return 0, fmt.Errorf("query max version: %w", err)
	}
	return int(n.Int64) + 1, nil
}

// GetVersion 查询版本。
func (s *Store) GetVersion(ctx context.Context, id string) (*model.Version, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, mission_id, number, status, strategy, delay_sec, segment_count, point_count,
			published_at, superseded_at, created_at
		FROM versions WHERE id = ?`, id)
	return scanVersion(row)
}

// ListVersions 列出任务下版本。
func (s *Store) ListVersions(ctx context.Context, missionID string) ([]*model.Version, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, mission_id, number, status, strategy, delay_sec, segment_count, point_count,
			published_at, superseded_at, created_at
		FROM versions WHERE mission_id = ? ORDER BY number`, missionID)
	if err != nil {
		return nil, fmt.Errorf("query versions: %w", err)
	}
	defer rows.Close()
	var out []*model.Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PublishVersion 发布版本（原子：旧发布版本 -> superseded）。
func (s *Store) PublishVersion(ctx context.Context, v *model.Version) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 将任务下其它 published 版本标记为 superseded。
	if _, err := tx.ExecContext(ctx, `
		UPDATE versions SET status = 'superseded', superseded_at = ?
		WHERE mission_id = ? AND status = 'published' AND id != ?`,
		nowRFC3339(), v.MissionID, v.ID); err != nil {
		return fmt.Errorf("supersede older versions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE versions SET status = 'published', published_at = ?, superseded_at = NULL
		WHERE id = ?`, nowRFC3339(), v.ID); err != nil {
		return fmt.Errorf("publish version: %w", err)
	}
	return tx.Commit()
}

// SupersedeVersion 将指定版本标记为替代。
func (s *Store) SupersedeVersion(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE versions SET status = 'superseded', superseded_at = ? WHERE id = ?`, nowRFC3339(), id)
	if err != nil {
		return fmt.Errorf("supersede version: %w", err)
	}
	return nil
}

func scanVersion(row rowScanner) (*model.Version, error) {
	var v model.Version
	var published, superseded, created sql.NullString
	if err := row.Scan(&v.ID, &v.MissionID, &v.Number, &v.Status, &v.Strategy, &v.DelaySec,
		&v.SegmentCount, &v.PointCount, &published, &superseded, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan version: %w", err)
	}
	if published.Valid {
		v.PublishedAt = parseTime(published.String)
	}
	if superseded.Valid {
		v.SupersededAt = parseTime(superseded.String)
	}
	v.CreatedAt = parseTime(created.String)
	return &v, nil
}
