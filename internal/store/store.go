// Package store 提供基于 SQLite 的持久化实现。
// 采用 modernc.org/sqlite 纯 Go 驱动（CGO 无关），支持建表迁移、
// 事务封装与重启恢复语义（游标续传、设备序号幂等）。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store 数据库访问层。
type Store struct {
	db *sql.DB
}

// Open 打开（或创建）SQLite 数据库并执行迁移。
func Open(path string) (*Store, error) {
	if path == "" {
		path = "heatdrift.db"
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close 关闭数据库。
func (s *Store) Close() error {
	return s.db.Close()
}

// DB 返回底层句柄（供事务工具使用）。
func (s *Store) DB() *sql.DB { return s.db }

// migrate 执行建表迁移（幂等）。
func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS devices (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			serial     TEXT NOT NULL UNIQUE,
			lat        REAL NOT NULL,
			lon        REAL NOT NULL,
			status     TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS missions (
			id          TEXT PRIMARY KEY,
			device_id   TEXT NOT NULL,
			name        TEXT NOT NULL,
			status      TEXT NOT NULL,
			cursor      INTEGER NOT NULL DEFAULT 0,
			point_count INTEGER NOT NULL DEFAULT 0,
			jump_count  INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL,
			FOREIGN KEY (device_id) REFERENCES devices(id)
		)`,
		`CREATE TABLE IF NOT EXISTS observations (
			id           TEXT PRIMARY KEY,
			mission_id   TEXT NOT NULL,
			seq          INTEGER NOT NULL,
			ts           TEXT NOT NULL,
			lat          REAL NOT NULL,
			lon          REAL NOT NULL,
			temp         REAL NOT NULL,
			status       TEXT NOT NULL,
			matched_road TEXT NOT NULL DEFAULT '',
			dist_m       REAL NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL,
			UNIQUE (mission_id, seq),
			FOREIGN KEY (mission_id) REFERENCES missions(id)
		)`,
		`CREATE TABLE IF NOT EXISTS road_segments (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			lat1       REAL NOT NULL,
			lon1       REAL NOT NULL,
			lat2       REAL NOT NULL,
			lon2       REAL NOT NULL,
			base_temp  REAL NOT NULL,
			status     TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS road_candidates (
			id         TEXT PRIMARY KEY,
			point_id   TEXT NOT NULL,
			road_id    TEXT NOT NULL,
			dist_m     REAL NOT NULL,
			proj_lat   REAL NOT NULL,
			proj_lon   REAL NOT NULL,
			ratio      REAL NOT NULL,
			score      REAL NOT NULL,
			chosen     INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			FOREIGN KEY (point_id) REFERENCES observations(id),
			FOREIGN KEY (road_id) REFERENCES road_segments(id)
		)`,
		`CREATE TABLE IF NOT EXISTS correction_params (
			id             TEXT PRIMARY KEY,
			mission_id     TEXT NOT NULL UNIQUE,
			delay_sec      REAL NOT NULL,
			temp_offset    REAL NOT NULL,
			strategy       TEXT NOT NULL,
			speed_limit_kph REAL NOT NULL,
			created_at     TEXT NOT NULL,
			FOREIGN KEY (mission_id) REFERENCES missions(id)
		)`,
		`CREATE TABLE IF NOT EXISTS track_segments (
			id         TEXT PRIMARY KEY,
			mission_id TEXT NOT NULL,
			road_id    TEXT NOT NULL,
			start_seq  INTEGER NOT NULL,
			end_seq    INTEGER NOT NULL,
			status     TEXT NOT NULL,
			temp_mean  REAL NOT NULL,
			temp_max   REAL NOT NULL,
			length_m   REAL NOT NULL,
			strategy   TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (mission_id) REFERENCES missions(id)
		)`,
		`CREATE TABLE IF NOT EXISTS versions (
			id            TEXT PRIMARY KEY,
			mission_id    TEXT NOT NULL,
			number        INTEGER NOT NULL,
			status        TEXT NOT NULL,
			strategy      TEXT NOT NULL,
			delay_sec     REAL NOT NULL,
			segment_count INTEGER NOT NULL,
			point_count   INTEGER NOT NULL,
			published_at  TEXT,
			superseded_at TEXT,
			created_at    TEXT NOT NULL,
			UNIQUE (mission_id, number),
			FOREIGN KEY (mission_id) REFERENCES missions(id)
		)`,
		`CREATE TABLE IF NOT EXISTS annotations (
			id         TEXT PRIMARY KEY,
			segment_id TEXT NOT NULL,
			author     TEXT NOT NULL,
			note       TEXT NOT NULL,
			decision   TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (segment_id) REFERENCES track_segments(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_obs_mission ON observations(mission_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_obs_status ON observations(mission_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_cand_point ON road_candidates(point_id)`,
		`CREATE INDEX IF NOT EXISTS idx_seg_mission ON track_segments(mission_id)`,
		`CREATE INDEX IF NOT EXISTS idx_annot_seg ON annotations(segment_id)`,
		`CREATE INDEX IF NOT EXISTS idx_versions_mission ON versions(mission_id, number)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec migration: %w (stmt=%s)", err, stmt)
		}
	}
	return nil
}
