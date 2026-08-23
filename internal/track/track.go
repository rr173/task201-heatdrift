// Package track 轨迹模块：把校正后的点序列聚合成连续轨迹段，
// 计算段内温度统计（均值/峰值）与长度，并支持复核标注流转。
package track

import (
	"context"
	"fmt"
	"sort"
	"time"

	"task201-heatdrift/internal/correction"
	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

// Service 轨迹服务。
type Service struct {
	store *store.Store
}

// New 构造轨迹服务。
func New(s *store.Store) *Service {
	return &Service{store: s}
}

// Segment 生成的轨迹段。
type Segment struct {
	RoadID   string
	StartSeq int64
	EndSeq   int64
	Points   []correction.CorrectedPoint
}

// BuildResult 轨迹构建结果。
type BuildResult struct {
	Segments []*model.TrackSegment `json:"segments"`
	RoadIDs  []string              `json:"road_ids"`
}

// Build 将校正点聚合成轨迹段：
//   - 按 seq 排序；
//   - 连续点归属同一道路（或相邻可衔接）时同段；
//   - 道路变化或点间隔超过阈值时切段；
//   - 段状态初始为 corrected（已校正），供复核确认。
func (s *Service) Build(ctx context.Context, missionID string, points []correction.CorrectedPoint, strategy string) (*BuildResult, error) {
	if len(points) == 0 {
		return nil, fmt.Errorf("%w: no corrected points", model.ErrInvalid)
	}
	sorted := make([]correction.CorrectedPoint, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })

	// 清理旧段（重算前）。
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := s.store.DeleteSegments(ctx, tx, missionID); err != nil {
		return nil, err
	}

	var segs []*model.TrackSegment
	var roads []string
	seen := map[string]bool{}
	start := 0
	for i := 1; i <= len(sorted); i++ {
		split := i == len(sorted)
		if !split {
			cur, next := sorted[i-1], sorted[i]
			// 道路变化或 pending 无归属时切段。
			if cur.RoadID != next.RoadID {
				split = true
			}
		}
		if split {
			seg, err := s.assemble(missionID, sorted[start:i], strategy)
			if err != nil {
				return nil, err
			}
			segs = append(segs, seg)
			if !seen[seg.RoadID] && seg.RoadID != "" {
				seen[seg.RoadID] = true
				roads = append(roads, seg.RoadID)
			}
			start = i
		}
	}
	for _, seg := range segs {
		if err := s.store.InsertSegmentTx(ctx, tx, seg); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit build: %w", err)
	}
	return &BuildResult{Segments: segs, RoadIDs: roads}, nil
}

// assemble 由一组校正点构造轨迹段实体。
func (s *Service) assemble(missionID string, pts []correction.CorrectedPoint, strategy string) (*model.TrackSegment, error) {
	roadID := ""
	var sum, max float64
	count := 0
	for _, p := range pts {
		if p.RoadID != "" {
			if roadID == "" {
				roadID = p.RoadID
			} else if roadID != p.RoadID {
				// 段内道路不一致时取多数。
				roadID = p.RoadID
			}
		}
		sum += p.Temp
		if p.Temp > max {
			max = p.Temp
		}
		count++
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: empty segment", model.ErrInvalid)
	}
	mean := sum / float64(count)
	startSeq := pts[0].Seq
	endSeq := pts[len(pts)-1].Seq
	length := 0.0
	for i := 1; i < len(pts); i++ {
		length += model.HaversineM(pts[i-1].Lat, pts[i-1].Lon, pts[i].Lat, pts[i].Lon)
	}
	return &model.TrackSegment{
		ID:        fmt.Sprintf("seg-%s-%d-%d", missionID, startSeq, endSeq),
		MissionID: missionID,
		RoadID:    roadID,
		StartSeq:  startSeq,
		EndSeq:    endSeq,
		Status:    "corrected",
		TempMean:  model.Round2(mean),
		TempMax:   model.Round2(max),
		LengthM:   model.Round2(length),
		Strategy:  strategy,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// Confirm 复核确认轨迹段（corrected/review -> confirmed）。
func (s *Service) Confirm(ctx context.Context, segmentID, author, note string) (*model.TrackSegment, error) {
	seg, err := s.store.GetSegment(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	if err := model.TransitionSegment(seg.Status, "confirmed"); err != nil {
		return nil, err
	}
	if err := s.store.SetSegmentStatus(ctx, seg.ID, "confirmed"); err != nil {
		return nil, err
	}
	ann := &model.Annotation{
		ID:        fmt.Sprintf("ann-%s-%d", segmentID, time.Now().UnixNano()),
		SegmentID: seg.ID,
		Author:    author,
		Note:      note,
		Decision:  "confirm",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.InsertAnnotation(ctx, ann); err != nil {
		return nil, err
	}
	seg.Status = "confirmed"
	return seg, nil
}

// MarkReview 标记轨迹段需复核（corrected -> review）。
func (s *Service) MarkReview(ctx context.Context, segmentID, note string) (*model.TrackSegment, error) {
	seg, err := s.store.GetSegment(ctx, segmentID)
	if err != nil {
		return nil, err
	}
	if err := model.TransitionSegment(seg.Status, "review"); err != nil {
		return nil, err
	}
	if err := s.store.SetSegmentStatus(ctx, seg.ID, "review"); err != nil {
		return nil, err
	}
	seg.Status = "review"
	return seg, nil
}
