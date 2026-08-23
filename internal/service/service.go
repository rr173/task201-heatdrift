// Package service 编排层：组合 ingest/matching/correction/track/version
// 业务包，对外提供统一的任务级流水线（接收 -> 匹配 -> 校正 -> 轨迹 -> 版本）。
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"task201-heatdrift/internal/correction"
	"task201-heatdrift/internal/ingest"
	"task201-heatdrift/internal/matching"
	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
	"task201-heatdrift/internal/track"
	"task201-heatdrift/internal/version"
)

// Services 聚合各业务服务。
type Services struct {
	Store    *store.Store
	Ingest   *ingest.Service
	Matching *matching.Service
	Track    *track.Service
	Version  *version.Service
	Logger   *log.Logger
}

// NewServices 构造全部服务。
func NewServices(st *store.Store, logger *log.Logger) *Services {
	return &Services{
		Store:    st,
		Ingest:   ingest.New(st),
		Matching: matching.New(st),
		Track:    track.New(st),
		Version:  version.New(st),
		Logger:   logger,
	}
}

// RunPipeline 执行完整处理流水线：
//  1. 检测跳点并执行道路匹配（matching）；
//  2. 按任务校正参数重建校正点序列（correction）；
//  3. 聚合轨迹段（track）。
//
// 若任务尚未设置校正参数，使用默认参数（delay=2s, offset=0, snap）。
func (s *Services) RunPipeline(ctx context.Context, missionID string) (*PipelineResult, error) {
	mission, err := s.Store.GetMission(ctx, missionID)
	if err != nil {
		return nil, err
	}
	// 1. 匹配。
	matchRes, err := s.Matching.MatchPoints(ctx, missionID, matching.DefaultOptions())
	if err != nil {
		return nil, fmt.Errorf("match failed: %w", err)
	}
	// 2. 校正参数（缺省用默认）。
	cp, err := s.Store.GetCorrection(ctx, missionID)
	params := correction.Params{DelaySec: 2, TempOffset: 0, Strategy: correction.StrategySnap}
	strategy := "snap"
	if err == nil {
		params = correction.Params{
			DelaySec:   cp.DelaySec,
			TempOffset: cp.TempOffset,
			Strategy:   correction.Strategy(cp.Strategy),
		}
		strategy = cp.Strategy
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	// 加载观测点与候选。
	points, err := s.Store.ListObservations(ctx, missionID, "", 0)
	if err != nil {
		return nil, err
	}
	candMap := map[string][]*model.RoadCandidate{}
	for _, p := range points {
		cs, err := s.Store.ListCandidatesByPoint(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if len(cs) > 0 {
			candMap[p.ID] = cs
		}
	}
	// 3. 重建校正点。
	corrected := correction.Rebuild(points, candMap, params, params.Strategy)
	// 4. 聚合轨迹段。
	build, err := s.Track.Build(ctx, missionID, corrected, strategy)
	if err != nil {
		return nil, fmt.Errorf("build tracks: %w", err)
	}
	// 5. 任务状态：有跳点则 cleaning，否则 running（已有 gapped 保留）。
	status := "running"
	if mission.Status == "gapped" {
		status = "gapped"
	}
	if matchRes.PointsJumped > 0 {
		status = "cleaning"
	}
	if err := s.Store.SetMissionStatus(ctx, missionID, status); err != nil {
		return nil, err
	}
	return &PipelineResult{
		MissionID:     missionID,
		Match:         matchRes,
		Segments:      build.Segments,
		SegmentCount:  len(build.Segments),
		Status:        status,
		Strategy:      strategy,
	}, nil
}

// PipelineResult 流水线结果。
type PipelineResult struct {
	MissionID    string                    `json:"mission_id"`
	Match        *matching.MatchResult      `json:"match"`
	Segments     []*model.TrackSegment      `json:"segments"`
	SegmentCount int                       `json:"segment_count"`
	Status       string                    `json:"status"`
	Strategy     string                    `json:"strategy"`
}

// CompleteMission 完成任务（running/gapped/cleaning -> completed）。
func (s *Services) CompleteMission(ctx context.Context, missionID string) (*model.Mission, error) {
	mission, err := s.Store.GetMission(ctx, missionID)
	if err != nil {
		return nil, err
	}
	if err := model.TransitionMission(mission.Status, "completed"); err != nil {
		return nil, err
	}
	if err := s.Store.SetMissionStatus(ctx, missionID, "completed"); err != nil {
		return nil, err
	}
	mission.Status = "completed"
	return mission, nil
}

// MissionStats 计算任务统计。
func (s *Services) MissionStats(ctx context.Context, missionID string) (*model.MissionStats, error) {
	if _, err := s.Store.GetMission(ctx, missionID); err != nil {
		return nil, err
	}
	points, err := s.Store.ListObservations(ctx, missionID, "", 0)
	if err != nil {
		return nil, err
	}
	segs, err := s.Store.ListSegments(ctx, missionID)
	if err != nil {
		return nil, err
	}
	st := &model.MissionStats{
		MissionID: missionID,
		TotalPoints: int64(len(points)),
	}
	var tempSum float64
	for _, p := range points {
		switch p.Status {
		case "matched":
			st.Matched++
			tempSum += p.Temp
		case "jump":
			st.Jumps++
			tempSum += p.Temp
		case "discarded":
			st.Discarded++
		}
	}
	st.Segments = len(segs)
	var segTempSum float64
	for _, seg := range segs {
		segTempSum += seg.TempMean
		if seg.Status == "confirmed" {
			st.ConfirmedSeg++
		}
	}
	if st.TotalPoints > 0 {
		st.MeanTemp = model.Round2(tempSum / float64(st.TotalPoints))
	}
	if len(segs) > 0 {
		// 热岛增量：段均温相对道路基线（取第一条段所在道路）。
		road, err := s.Store.GetRoad(ctx, segs[0].RoadID)
		if err == nil {
			st.HeatDelta = model.Round2(segTempSum/float64(len(segs)) - road.BaseTemp)
		}
	}
	return st, nil
}

// Health 健康检查：确认数据库可访问。
func (s *Services) Health(ctx context.Context) error {
	if err := s.Store.DB().PingContext(ctx); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}
	return nil
}

// NowUTC 供测试与自检使用。
func NowUTC() time.Time { return time.Now().UTC() }
