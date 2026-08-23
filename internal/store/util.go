package store

import (
	"context"
	"database/sql"
	"time"
)

// 时间序列化布局（RFC3339 秒级）。
const timeLayout = "2006-01-02T15:04:05Z07:00"

// queryer 事务与连接共用的查询接口。
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func nowRFC3339() string {
	return time.Now().UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite 返回的错误文本包含 "UNIQUE constraint failed"。
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, frag := range []string{"UNIQUE constraint failed", "constraint failed"} {
		if contains(msg, frag) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---------- 系统统计 ----------

// SystemStats 汇总系统级计数。
func (s *Store) SystemStats(ctx context.Context) (devices, missions, roads, versions, published int64, err error) {
	query := func(sqlStmt string, dst *int64) error {
		if err := s.db.QueryRowContext(ctx, sqlStmt).Scan(dst); err != nil {
			return err
		}
		return nil
	}
	if err = query(`SELECT COUNT(*) FROM devices`, &devices); err != nil {
		return
	}
	if err = query(`SELECT COUNT(*) FROM missions`, &missions); err != nil {
		return
	}
	if err = query(`SELECT COUNT(*) FROM road_segments`, &roads); err != nil {
		return
	}
	if err = query(`SELECT COUNT(*) FROM versions`, &versions); err != nil {
		return
	}
	if err = query(`SELECT COUNT(*) FROM versions WHERE status = 'published'`, &published); err != nil {
		return
	}
	return
}
