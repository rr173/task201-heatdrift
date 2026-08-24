package service

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/ingest"
	"task201-heatdrift/internal/matching"
	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

func newTestStore(t *testing.T) (*store.Store, *Services) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	logger := log.New(os.Stderr, "[test] ", 0)
	return s, NewServices(s, logger)
}

func TestPipelineEndToEnd(t *testing.T) {
	_, svc := newTestStore(t)
	ctx := context.Background()

	dev := &model.Device{
		ID: "dev-1", Name: "d1", Serial: "S1",
		Lat: 39.9042, Lon: 116.4074, Status: "active", CreatedAt: time.Now(),
	}
	if err := svc.Store.CreateDevice(ctx, dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	roads := []*model.RoadSegment{
		{ID: "r1", Name: "r1", Lat1: 39.9030, Lon1: 116.4050, Lat2: 39.9070, Lon2: 116.4110, BaseTemp: 28, Status: "active"},
		{ID: "r2", Name: "r2", Lat1: 39.9060, Lon1: 116.4120, Lat2: 39.9100, Lon2: 116.4180, BaseTemp: 31, Status: "active"},
	}
	for _, r := range roads {
		if err := svc.Store.CreateRoad(ctx, r); err != nil {
			t.Fatalf("create road: %v", err)
		}
	}
	mission := &model.Mission{
		ID: "m1", DeviceID: dev.ID, Name: "m1", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := svc.Store.CreateMission(ctx, mission); err != nil {
		t.Fatalf("create mission: %v", err)
	}

	base := time.Now().Add(-time.Minute)
	var pts []ingest.PointInput
	lat, lon := 39.9030, 116.4050
	for i := 0; i < 10; i++ {
		p := ingest.PointInput{Seq: int64(i + 1), TS: base.Add(time.Duration(i) * 15 * time.Second), Lat: lat, Lon: lon, Temp: 28 + float64(i)*0.1}
		if i == 4 {
			p.Lat, p.Lon = 39.9090, 116.4130 // 跳点
		}
		pts = append(pts, p)
		lat += 0.0004
		lon += 0.0006
	}
	res, err := svc.Ingest.Ingest(ctx, "m1", ingest.BatchInput{Points: pts})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Accepted != 10 {
		t.Fatalf("expected 10 accepted, got %d", res.Accepted)
	}

	// 校正参数使用 interpolate 策略。
	if err := svc.Store.UpsertCorrection(ctx, &model.CorrectionParams{
		ID: "c1", MissionID: "m1", DelaySec: 2, TempOffset: 0, Strategy: "interpolate", SpeedLimitKPH: 80,
	}); err != nil {
		t.Fatalf("upsert correction: %v", err)
	}

	pipe, err := svc.RunPipeline(ctx, "m1")
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if pipe.SegmentCount == 0 {
		t.Fatal("expected segments")
	}
	// 跳点被插值保留 -> 全部点应可用。
	if pipe.Match.PointsJumped == 0 {
		t.Fatal("expected jump detected")
	}

	// 统计。
	st, err := svc.MissionStats(ctx, "m1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.TotalPoints != 10 {
		t.Fatalf("expected 10 total points, got %d", st.TotalPoints)
	}
}

func TestCompleteMissionFlow(t *testing.T) {
	st, svc := newTestStore(t)
	ctx := context.Background()
	dev := &model.Device{
		ID: "dev-x", Name: "x", Serial: "SX", Lat: 1, Lon: 2,
		Status: "active", CreatedAt: time.Now(),
	}
	if err := st.CreateDevice(ctx, dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	mission := &model.Mission{
		ID: "m2", DeviceID: "dev-x", Name: "m2", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := svc.Store.CreateMission(ctx, mission); err != nil {
		t.Fatalf("create mission: %v", err)
	}
	m, err := svc.CompleteMission(ctx, "m2")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if m.Status != "completed" {
		t.Fatalf("expected completed, got %s", m.Status)
	}
	// 再完成一次应报错。
	if _, err := svc.CompleteMission(ctx, "m2"); err == nil {
		t.Fatal("expected error on second complete")
	}
}

func TestMissionStatsEmpty(t *testing.T) {
	_, svc := newTestStore(t)
	st, err := svc.MissionStats(context.Background(), "missing")
	if err == nil {
		t.Fatalf("expected error for missing mission, got %+v", st)
	}
}

// TestMatchExcludesRetiredRoad 验证停用道路不再作为可用匹配道路：
// 当唯一的候选道路被停用后，匹配应报"无可用道路"而非继续将其作为候选。
func TestMatchExcludesRetiredRoad(t *testing.T) {
	_, svc := newTestStore(t)
	ctx := context.Background()
	dev := &model.Device{
		ID: "dev-r", Name: "r", Serial: "SR", Lat: 39.9042, Lon: 116.4074,
		Status: "active", CreatedAt: time.Now(),
	}
	if err := svc.Store.CreateDevice(ctx, dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	road := &model.RoadSegment{
		ID: "r1", Name: "r1", Lat1: 39.9030, Lon1: 116.4050, Lat2: 39.9070, Lon2: 116.4110,
		BaseTemp: 28, Status: "active", CreatedAt: time.Now(),
	}
	if err := svc.Store.CreateRoad(ctx, road); err != nil {
		t.Fatalf("create road: %v", err)
	}
	mission := &model.Mission{
		ID: "m1", DeviceID: dev.ID, Name: "m1", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := svc.Store.CreateMission(ctx, mission); err != nil {
		t.Fatalf("create mission: %v", err)
	}
	now := time.Now().UTC()
	pt := &model.Observation{
		ID: "o1", MissionID: "m1", Seq: 1, TS: now, Lat: 39.9042, Lon: 116.4074,
		Temp: 28, Status: "pending", CreatedAt: now,
	}
	if _, err := svc.Store.InsertObservation(ctx, svc.Store.DB(), pt); err != nil {
		t.Fatalf("insert obs: %v", err)
	}

	// 停用该道路后，匹配必须排除它：此时无 active 道路 -> ErrNotFound。
	if err := svc.Store.SetRoadStatus(ctx, "r1", "retired"); err != nil {
		t.Fatalf("retire road: %v", err)
	}
	if _, err := svc.Matching.MatchPoints(ctx, "m1", matching.DefaultOptions()); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when only road is retired, got %v", err)
	}

	// 恢复道路为 active，匹配应正常产出候选。
	if err := svc.Store.SetRoadStatus(ctx, "r1", "active"); err != nil {
		t.Fatalf("reactivate road: %v", err)
	}
	res, err := svc.Matching.MatchPoints(ctx, "m1", matching.DefaultOptions())
	if err != nil {
		t.Fatalf("match after reactivate: %v", err)
	}
	if res.Candidates == 0 {
		t.Fatal("expected candidates after reactivating road")
	}
}
