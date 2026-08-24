package version

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

func openTempStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "v.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func createDeviceMission(t *testing.T, s *store.Store, missionID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := s.CreateDevice(context.Background(), &model.Device{
		ID: "d-" + missionID, Name: "dev", Serial: "SN-" + missionID, Lat: 1, Lon: 2,
		Status: "active", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := s.CreateMission(context.Background(), &model.Mission{
		ID: missionID, DeviceID: "d-" + missionID, Name: missionID, Status: "running",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create mission: %v", err)
	}
}

func makeVersion(t *testing.T, s *store.Store, id, missionID string, number int, status string) *model.Version {
	t.Helper()
	v := &model.Version{
		ID: id, MissionID: missionID, Number: number, Status: status, Strategy: "snap",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateVersion(context.Background(), v); err != nil {
		t.Fatalf("create version %s: %v", id, err)
	}
	return v
}

// TestPublishSupersededRejected 验证已被新版本替代（superseded）的版本不能被重新发布，
// 且重新发布的尝试不会错误地替代任务当前已发布的版本。
func TestPublishSupersededRejected(t *testing.T) {
	ctx := context.Background()
	s := openTempStore(t)
	createDeviceMission(t, s, "m")

	svc := New(s)

	// v1: computing -> published（成为当前发布版本）。
	v1 := makeVersion(t, s, "v1", "m", 1, "computing")
	if _, err := svc.Publish(ctx, v1.ID); err != nil {
		t.Fatalf("publish v1: %v", err)
	}

	// v2: computing -> published，v1 自动 superseded。
	v2 := makeVersion(t, s, "v2", "m", 2, "computing")
	if _, err := svc.Publish(ctx, v2.ID); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	got1, _ := s.GetVersion(ctx, v1.ID)
	if got1.Status != "superseded" {
		t.Fatalf("expected v1 superseded, got %s", got1.Status)
	}

	// 重新发布已被替代的 v1 必须被拒绝：它是冻结终态。
	_, err := svc.Publish(ctx, v1.ID)
	if err == nil {
		t.Fatal("republishing a superseded version must be rejected")
	}
	if !isBadState(err) {
		t.Fatalf("expected ErrBadState when republishing superseded, got %v", err)
	}

	// 当前发布版本仍是 v2，未被错误替代。
	got2, _ := s.GetVersion(ctx, v2.ID)
	if got2.Status != "published" {
		t.Fatalf("current published version should remain published, got %s", got2.Status)
	}
	got1After, _ := s.GetVersion(ctx, v1.ID)
	if got1After.Status != "superseded" {
		t.Fatalf("superseded v1 should stay superseded after rejected republish, got %s", got1After.Status)
	}
}

// TestPublishComputingAllowed 验证 computing 版本可正常发布。
func TestPublishComputingAllowed(t *testing.T) {
	ctx := context.Background()
	s := openTempStore(t)
	createDeviceMission(t, s, "m")
	svc := New(s)

	v := makeVersion(t, s, "v", "m", 1, "computing")
	pub, err := svc.Publish(ctx, v.ID)
	if err != nil {
		t.Fatalf("publish computing version: %v", err)
	}
	if pub.Status != "published" {
		t.Fatalf("expected published, got %s", pub.Status)
	}
}

// isBadState 判断是否为状态流转错误（model.ErrBadState / StateError）。
func isBadState(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, model.ErrBadState) {
		return true
	}
	var se *model.StateError
	return errors.As(err, &se)
}
