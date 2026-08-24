package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// createDevice 插入测试设备（mission 外键依赖）。
func createDevice(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.CreateDevice(context.Background(), &model.Device{
		ID: id, Name: "dev-" + id, Serial: "SN-" + id, Lat: 1, Lon: 2,
		Status: "active", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
}

// createMission 插入测试任务。
func createMission(t *testing.T, s *Store, id, deviceID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := s.CreateMission(context.Background(), &model.Mission{
		ID: id, DeviceID: deviceID, Name: id, Status: "running", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create mission: %v", err)
	}
}

func TestDeviceCRUD(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	d := &model.Device{
		ID: "d1", Name: "dev", Serial: "SN1", Lat: 1, Lon: 2,
		Status: "active", CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateDevice(ctx, d); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetDevice(ctx, "d1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Serial != "SN1" {
		t.Fatalf("unexpected serial %s", got.Serial)
	}
	// 重复序列号冲突。
	dup := *d
	dup.ID = "d2"
	if err := s.CreateDevice(ctx, &dup); err == nil {
		t.Fatal("expected conflict on duplicate serial")
	}
	// 归档。
	if err := s.SetDeviceStatus(ctx, "d1", "archived"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if g, _ := s.GetDevice(ctx, "d1"); g.Status != "archived" {
		t.Fatalf("expected archived, got %s", g.Status)
	}
}

func TestRoadRetireStatusConsistency(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	rd := &model.RoadSegment{
		ID: "r1", Name: "R1", Lat1: 1, Lon1: 1, Lat2: 2, Lon2: 2,
		BaseTemp: 20, Status: "active", CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateRoad(ctx, rd); err != nil {
		t.Fatalf("create road: %v", err)
	}
	// 停用道路。
	if err := s.SetRoadStatus(ctx, "r1", "retired"); err != nil {
		t.Fatalf("retire road: %v", err)
	}
	// GetRoad 必须如实返回 retired，不得改写为 active。
	got, err := s.GetRoad(ctx, "r1")
	if err != nil {
		t.Fatalf("get road: %v", err)
	}
	if got.Status != "retired" {
		t.Fatalf("expected retired, got %s", got.Status)
	}
	// ListRoads 同样必须返回 retired。
	list, err := s.ListRoads(ctx)
	if err != nil {
		t.Fatalf("list roads: %v", err)
	}
	var found bool
	for _, r := range list {
		if r.ID == "r1" {
			found = true
			if r.Status != "retired" {
				t.Fatalf("expected retired in list, got %s", r.Status)
			}
		}
	}
	if !found {
		t.Fatal("retired road missing from list")
	}
}

func TestObservationIdempotent(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Now().UTC()
	createDevice(t, s, "dx")
	createMission(t, s, "m", "dx")
	o := &model.Observation{
		ID: "o1", MissionID: "m", Seq: 1, TS: now, Lat: 1, Lon: 1, Temp: 20,
		Status: "pending", CreatedAt: now,
	}
	ok, err := s.InsertObservation(ctx, s.db, o)
	if err != nil || !ok {
		t.Fatalf("first insert should succeed: ok=%v err=%v", ok, err)
	}
	ok2, err := s.InsertObservation(ctx, s.db, o)
	if err != nil || ok2 {
		t.Fatalf("duplicate insert should be skipped: ok=%v err=%v", ok2, err)
	}
	// 幂等存在性。
	if exists, _ := s.ObservationExists(ctx, "m", 1); !exists {
		t.Fatal("expected exists")
	}
	if exists, _ := s.ObservationExists(ctx, "m", 99); exists {
		t.Fatal("expected not exists")
	}
	last, err := s.LastObservationSeq(ctx, "m")
	if err != nil || last != 1 {
		t.Fatalf("expected last seq 1, got %d err=%v", last, err)
	}
}

func TestPublishVersionSupersedes(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Now().UTC()
	createDevice(t, s, "dx")
	createMission(t, s, "m", "dx")
	v1 := &model.Version{ID: "v1", MissionID: "m", Number: 1, Status: "computing", Strategy: "snap", CreatedAt: now}
	v2 := &model.Version{ID: "v2", MissionID: "m", Number: 2, Status: "computing", Strategy: "snap", CreatedAt: now}
	if err := s.CreateVersion(ctx, v1); err != nil {
		t.Fatalf("create v1: %v", err)
	}
	if err := s.CreateVersion(ctx, v2); err != nil {
		t.Fatalf("create v2: %v", err)
	}
	// 发布 v1。
	if err := s.PublishVersion(ctx, v1); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	// 发布 v2 -> v1 自动 superseded。
	if err := s.PublishVersion(ctx, v2); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	got1, _ := s.GetVersion(ctx, "v1")
	got2, _ := s.GetVersion(ctx, "v2")
	if got1.Status != "superseded" {
		t.Fatalf("expected v1 superseded, got %s", got1.Status)
	}
	if got2.Status != "published" {
		t.Fatalf("expected v2 published, got %s", got2.Status)
	}
	// 下一个版本号。
	n, err := s.NextVersionNumber(ctx, "m")
	if err != nil || n != 3 {
		t.Fatalf("expected next number 3, got %d err=%v", n, err)
	}
}

func TestReopenRestoresData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reopen.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	createDevice(t, s, "dx")
	createMission(t, s, "m", "dx")
	// 推进游标模拟已接收数据。
	if err := s.UpdateMissionCursor(ctx, s.db, "m", 5, 5); err != nil {
		t.Fatalf("update cursor: %v", err)
	}
	o := &model.Observation{
		ID: "o1", MissionID: "m", Seq: 5, TS: now, Lat: 1, Lon: 1, Temp: 20,
		Status: "matched", MatchedRoad: "r1", CreatedAt: now,
	}
	if _, err := s.InsertObservation(ctx, s.db, o); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// 重新打开同一数据库。
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	m, err := s2.GetMission(ctx, "m")
	if err != nil {
		t.Fatalf("get mission: %v", err)
	}
	if m.Cursor != 5 || m.Status != "running" {
		t.Fatalf("cursor/status not restored: %d %s", m.Cursor, m.Status)
	}
	obs, err := s2.GetObservation(ctx, "m", 5)
	if err != nil {
		t.Fatalf("get observation: %v", err)
	}
	if obs.Status != "matched" || obs.MatchedRoad != "r1" {
		t.Fatalf("observation not restored: %s %s", obs.Status, obs.MatchedRoad)
	}
}
