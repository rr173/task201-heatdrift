package ingest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

// openStore 构造临时 SQLite 存储。
func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seedDeviceMission 登记设备与采集任务，返回 missionID。
func seedDeviceMission(t *testing.T, s *store.Store, devID string) string {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateDevice(ctx, &model.Device{
		ID: devID, Name: "dev-" + devID, Serial: "SN-" + devID,
		Lat: 1, Lon: 2, Status: "active", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	mID := "m-" + devID
	now := time.Now().UTC()
	if err := s.CreateMission(ctx, &model.Mission{
		ID: mID, DeviceID: devID, Name: mID, Status: "running",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create mission: %v", err)
	}
	return mID
}

// TestIngestArchivedDeviceRejected 验证设备归档后其采集任务的新增观测被拒绝。
func TestIngestArchivedDeviceRejected(t *testing.T) {
	s := openStore(t)
	svc := New(s)
	ctx := context.Background()
	mID := seedDeviceMission(t, s, "devA")

	// 归档前可正常上传。
	base := time.Now().UTC()
	pre, err := svc.Ingest(ctx, mID, BatchInput{Points: []PointInput{
		{Seq: 1, TS: base, Lat: 1, Lon: 1, Temp: 20},
	}})
	if err != nil {
		t.Fatalf("ingest before archive: %v", err)
	}
	if pre.Accepted != 1 {
		t.Fatalf("expected 1 accepted before archive, got %d", pre.Accepted)
	}

	// 归档设备：持久化状态必须落到 archived。
	if err := s.SetDeviceStatus(ctx, "devA", "archived"); err != nil {
		t.Fatalf("archive device: %v", err)
	}
	got, _ := s.GetDevice(ctx, "devA")
	if got.Status != "archived" {
		t.Fatalf("expected device archived, got %s", got.Status)
	}

	// 归档后新增观测必须被拒绝（而非落库）。
	_, err = svc.Ingest(ctx, mID, BatchInput{Points: []PointInput{
		{Seq: 2, TS: base.Add(time.Second), Lat: 1, Lon: 1, Temp: 21},
	}})
	if !errors.Is(err, model.ErrBadState) {
		t.Fatalf("expected ErrBadState for archived device, got %v", err)
	}

	// 确认未新增观测点。
	last, _ := s.LastObservationSeq(ctx, mID)
	if last != 1 {
		t.Fatalf("expected last seq still 1 after archived upload, got %d", last)
	}
}

// TestIngestActiveDeviceUnaffected 验证正常设备上传不受归档判断影响。
func TestIngestActiveDeviceUnaffected(t *testing.T) {
	s := openStore(t)
	svc := New(s)
	ctx := context.Background()
	mID := seedDeviceMission(t, s, "devB")

	base := time.Now().UTC()
	res, err := svc.Ingest(ctx, mID, BatchInput{Points: []PointInput{
		{Seq: 1, TS: base, Lat: 1, Lon: 1, Temp: 20},
		{Seq: 2, TS: base.Add(time.Second), Lat: 1.0001, Lon: 1, Temp: 20.5},
	}})
	if err != nil {
		t.Fatalf("ingest active device: %v", err)
	}
	if res.Accepted != 2 {
		t.Fatalf("expected 2 accepted, got %d", res.Accepted)
	}
	// 幂等重传：重复跳过，不报归档错误。
	dup, err := svc.Ingest(ctx, mID, BatchInput{Points: []PointInput{
		{Seq: 1, TS: base, Lat: 1, Lon: 1, Temp: 20},
	}})
	if err != nil {
		t.Fatalf("idempotent re-ingest: %v", err)
	}
	if dup.Duplicated != 1 {
		t.Fatalf("expected 1 duplicated, got %d", dup.Duplicated)
	}
}
