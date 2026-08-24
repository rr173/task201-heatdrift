package ingest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db := filepath.Join(t.TempDir(), "ingest.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seed(t *testing.T, s *store.Store) (*model.Device, *model.Mission) {
	t.Helper()
	ctx := context.Background()
	dev := &model.Device{
		ID: "dev-g", Name: "g", Serial: "SG", Lat: 39.9042, Lon: 116.4074,
		Status: "active", CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateDevice(ctx, dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	m := &model.Mission{
		ID: "miss-g", DeviceID: dev.ID, Name: "g", Status: "running",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateMission(ctx, m); err != nil {
		t.Fatalf("create mission: %v", err)
	}
	return dev, m
}

// TestIngestGapMarksGapped 验证：相邻观测点时间间隔超过 MaxGapSec 时，
// 接收流程将任务标记为 gapped（而非丢弃回 running），缺口事实落库。
func TestIngestGapMarksGapped(t *testing.T) {
	s := newTestStore(t)
	svc := New(s)
	ctx := context.Background()
	_, m := seed(t, s)

	// 先接收一个正常点，建立时间基准。
	base := time.Now().UTC().Add(-5 * time.Minute)
	if _, err := svc.Ingest(ctx, m.ID, BatchInput{Points: []PointInput{
		{Seq: 1, TS: base, Lat: 39.9030, Lon: 116.4050, Temp: 28.0},
	}}); err != nil {
		t.Fatalf("ingest first: %v", err)
	}

	// 第二个点与第一个点间隔远超 MaxGapSec -> 缺口。
	gapTS := base.Add(MaxGapSec * 2 * time.Second)
	if _, err := svc.Ingest(ctx, m.ID, BatchInput{Points: []PointInput{
		{Seq: 2, TS: gapTS, Lat: 39.9031, Lon: 116.4051, Temp: 28.2},
	}}); err != nil {
		t.Fatalf("ingest gap: %v", err)
	}

	got, err := s.GetMission(ctx, m.ID)
	if err != nil {
		t.Fatalf("get mission: %v", err)
	}
	if got.Status != "gapped" {
		t.Fatalf("expected mission gapped after time gap, got %s", got.Status)
	}
}

// TestIngestNoGapStaysRunning 验证：正常间隔不触发 gapped。
func TestIngestNoGapStaysRunning(t *testing.T) {
	s := newTestStore(t)
	svc := New(s)
	ctx := context.Background()
	_, m := seed(t, s)

	base := time.Now().UTC().Add(-5 * time.Minute)
	if _, err := svc.Ingest(ctx, m.ID, BatchInput{Points: []PointInput{
		{Seq: 1, TS: base, Lat: 39.9030, Lon: 116.4050, Temp: 28.0},
		{Seq: 2, TS: base.Add(30 * time.Second), Lat: 39.9031, Lon: 116.4051, Temp: 28.2},
	}}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	got, _ := s.GetMission(ctx, m.ID)
	if got.Status != "running" {
		t.Fatalf("expected running without gap, got %s", got.Status)
	}
}

// TestGappedSurvivesReopen 验证：gapped 状态在关闭重开数据库后仍恢复，
// 缺口事实持久化不丢。
func TestGappedSurvivesReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reopen-gap.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	_, m := seed(t, s)

	ingSvc := New(s)
	base := time.Now().UTC().Add(-5 * time.Minute)
	if _, err := ingSvc.Ingest(ctx, m.ID, BatchInput{Points: []PointInput{
		{Seq: 1, TS: base, Lat: 39.9030, Lon: 116.4050, Temp: 28.0},
		{Seq: 2, TS: base.Add(MaxGapSec * 2 * time.Second), Lat: 39.9031, Lon: 116.4051, Temp: 28.2},
	}}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	got, err := s2.GetMission(ctx, m.ID)
	if err != nil {
		t.Fatalf("get mission: %v", err)
	}
	if got.Status != "gapped" {
		t.Fatalf("expected gapped restored after reopen, got %s", got.Status)
	}
	if got.Cursor != 2 {
		t.Fatalf("expected cursor 2 restored, got %d", got.Cursor)
	}
}
