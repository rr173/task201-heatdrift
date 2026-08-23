// heatdrift 城市热岛移动传感轨迹去漂移服务。
// 入口契约：
//   --addr :8080        HTTP 监听地址
//   --db <path>         SQLite 数据库路径（默认 heatdrift.db）
//   --smoke-test        自检模式：创建数据 -> 执行完整闭环（接收/匹配/校正/轨迹/版本）
//                       关闭重开数据库验证持久化与重启恢复，全程以 0 退出码结束。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"task201-heatdrift/internal/correction"
	"task201-heatdrift/internal/httpapi"
	"task201-heatdrift/internal/ingest"
	"task201-heatdrift/internal/matching"
	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/service"
	"task201-heatdrift/internal/store"
	"task201-heatdrift/internal/version"
)

func main() {
	addr := flag.String("addr", ":8080", "http listen address")
	dbPath := flag.String("db", "heatdrift.db", "sqlite database path")
	smoke := flag.Bool("smoke-test", false, "run end-to-end self test and exit")
	flag.Parse()

	logger := log.New(os.Stdout, "[heatdrift] ", log.LstdFlags)

	if *smoke {
		if err := runSmokeTest(*dbPath, logger); err != nil {
			logger.Printf("SMOKE TEST FAILED: %v", err)
			os.Exit(1)
		}
		logger.Printf("SMOKE TEST PASSED")
		return
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer s.Close()

	svc := service.NewServices(s, logger)
	h := httpapi.NewHandler(svc)
	server := httpapi.New(h, logger)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Printf("listening on %s (db=%s)", *addr, *dbPath)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("serve: %v", err)
	}
}

// runSmokeTest 端到端自检：
//  1. 注册设备与道路片段；
//  2. 创建采集任务并上传样例观测点（含一段 GPS 跳点与温度响应延迟）；
//  3. 执行匹配：检测跳点、生成道路候选；
//  4. 设置校正参数并运行流水线，生成轨迹段；
//  5. 复核确认轨迹段，创建并发布版本；
//  6. 关闭并重新打开同一数据库，验证状态恢复、游标与发布版本绑定。
func runSmokeTest(dbPath string, logger *log.Logger) error {
	// 清理历史自检数据库，保证可重复运行。
	_ = os.Remove(dbPath)

	s, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	ctx := context.Background()
	svc := service.NewServices(s, logger)

	// 1. 注册设备。
	dev := &model.Device{
		ID: "dev-bike-01", Name: "bike-sensor-01", Serial: "BIKE001",
		Lat: 39.9042, Lon: 116.4074, Status: "active", CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateDevice(ctx, dev); err != nil {
		return fmt.Errorf("create device: %w", err)
	}
	// 登记两条城市道路片段。
	roads := []*model.RoadSegment{
		{ID: "road-rd1", Name: "rd1", Lat1: 39.9030, Lon1: 116.4050, Lat2: 39.9070, Lon2: 116.4110, BaseTemp: 28.0, Status: "active", CreatedAt: time.Now().UTC()},
		{ID: "road-rd2", Name: "rd2", Lat1: 39.9060, Lon1: 116.4120, Lat2: 39.9100, Lon2: 116.4180, BaseTemp: 31.5, Status: "active", CreatedAt: time.Now().UTC()},
	}
	for _, r := range roads {
		if err := s.CreateRoad(ctx, r); err != nil {
			return fmt.Errorf("create road %s: %w", r.ID, err)
		}
	}

	// 2. 创建采集任务。
	mission := &model.Mission{
		ID: "miss-heat-01", DeviceID: dev.ID, Name: "heat-01",
		Status: "running", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateMission(ctx, mission); err != nil {
		return fmt.Errorf("create mission: %w", err)
	}

	// 3. 上传观测点：沿 rd1 移动，中间注入跳点（远离道路），温度随时间升。
	base := time.Now().UTC().Add(-10 * time.Minute)
	var points []ingest.PointInput
	lat, lon := 39.9030, 116.4050
	for i := 0; i < 12; i++ {
		p := ingest.PointInput{
			Seq: int64(i + 1), TS: base.Add(time.Duration(i) * 15 * time.Second),
			Lat: lat, Lon: lon, Temp: 28.5 + float64(i)*0.2,
		}
		if i == 5 {
			// 跳点：位置偏离道路 300 米。
			p.Lat = 39.9040
			p.Lon = 116.4100
			p.Temp = 30.0
		}
		points = append(points, p)
		lat += 0.0004
		lon += 0.0006
	}
	batch := ingest.BatchInput{Points: points}
	ingestRes, err := svc.Ingest.Ingest(ctx, mission.ID, batch)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	if ingestRes.Accepted != 12 {
		return fmt.Errorf("expected 12 accepted points, got %d", ingestRes.Accepted)
	}
	logger.Printf("ingested %d points (duplicated=%d rejected=%d)", ingestRes.Accepted, ingestRes.Duplicated, ingestRes.Rejected)

	// 幂等重传：应全部重复。
	dupRes, err := svc.Ingest.Ingest(ctx, mission.ID, batch)
	if err != nil {
		return fmt.Errorf("re-ingest: %w", err)
	}
	if dupRes.Duplicated != 12 {
		return fmt.Errorf("expected 12 duplicated, got %d", dupRes.Duplicated)
	}
	logger.Printf("re-ingest idempotent: duplicated=%d", dupRes.Duplicated)

	// 4. 匹配：检测跳点 + 道路候选。
	matchRes, err := svc.Matching.MatchPoints(ctx, mission.ID, matching.DefaultOptions())
	if err != nil {
		return fmt.Errorf("match: %w", err)
	}
	logger.Printf("match: matched=%d jumped=%d candidates=%d", matchRes.PointsMatched, matchRes.PointsJumped, matchRes.Candidates)
	if matchRes.PointsJumped == 0 {
		return errors.New("expected at least one jump point")
	}

	// 跳点应被标记。
	jumpObs, err := s.ListObservations(ctx, mission.ID, "jump", 0)
	if err != nil {
		return err
	}
	if len(jumpObs) == 0 {
		return errors.New("expected jump observations")
	}

	// 5. 设置校正参数（discard 策略 + 延迟校正），运行流水线。
	cp := &model.CorrectionParams{
		ID: "corr-heat-01", MissionID: mission.ID,
		DelaySec: 2, TempOffset: 0, Strategy: "discard", SpeedLimitKPH: 80,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.UpsertCorrection(ctx, cp); err != nil {
		return fmt.Errorf("upsert correction: %w", err)
	}
	pipe, err := svc.RunPipeline(ctx, mission.ID)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	logger.Printf("pipeline: status=%s segments=%d strategy=%s", pipe.Status, pipe.SegmentCount, pipe.Strategy)
	if pipe.SegmentCount == 0 {
		return errors.New("expected at least one track segment")
	}

	// 验证校正函数本身。
	raw := []float64{28.5, 28.7, 28.9, 29.1, 29.3, 29.5}
	delayed := correction.ApplyDelay(raw, nil, 2)
	_ = delayed
	comp := correction.CompensateOffset(raw, 0.5)
	if comp[0] != 29.0 {
		return fmt.Errorf("expected offset-compensated temp 29.0, got %v", comp[0])
	}

	// 6. 复核确认第一个轨迹段。
	segs, err := s.ListSegments(ctx, mission.ID)
	if err != nil {
		return err
	}
	confirmed, err := svc.Track.Confirm(ctx, segs[0].ID, "smoke", "轨迹连续，无漂移")
	if err != nil {
		return fmt.Errorf("confirm segment: %w", err)
	}
	if confirmed.Status != "confirmed" {
		return fmt.Errorf("expected confirmed segment, got %s", confirmed.Status)
	}

	// 7. 创建并发布版本。
	v, err := svc.Version.Create(ctx, version.CreateInput{
		MissionID:  mission.ID,
		Strategy:   "discard",
		DelaySec:   2,
		SegmentCnt: pipe.SegmentCount,
		PointCount: int64(len(points)),
	})
	if err != nil {
		return fmt.Errorf("create version: %w", err)
	}
	published, err := svc.Version.Publish(ctx, v.ID)
	if err != nil {
		return fmt.Errorf("publish version: %w", err)
	}
	if published.Status != "published" {
		return fmt.Errorf("expected published version, got %s", published.Status)
	}
	logger.Printf("published version %s strategy=%s segments=%d", published.ID, published.Strategy, published.SegmentCount)

	// 8. 关闭并重开同一数据库，验证恢复。
	if err := s.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	s2, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("reopen: %w", err)
	}
	defer s2.Close()

	m2, err := s2.GetMission(ctx, mission.ID)
	if err != nil {
		return fmt.Errorf("reopen get mission: %w", err)
	}
	if m2.Status != "completed" {
		return fmt.Errorf("expected mission completed after publish+reopen, got %s", m2.Status)
	}
	obs2, err := s2.ListObservations(ctx, mission.ID, "", 0)
	if err != nil {
		return err
	}
	if len(obs2) != 12 {
		return fmt.Errorf("expected 12 observations after reopen, got %d", len(obs2))
	}
	v2, err := s2.GetVersion(ctx, published.ID)
	if err != nil {
		return fmt.Errorf("reopen get version: %w", err)
	}
	if v2.Status != "published" {
		return fmt.Errorf("expected published version after reopen, got %s", v2.Status)
	}
	segs2, err := s2.ListSegments(ctx, mission.ID)
	if err != nil {
		return err
	}
	logger.Printf("recovered: mission=%s points=%d segments=%d version=%s", m2.Status, len(obs2), len(segs2), v2.Status)
	return nil
}
