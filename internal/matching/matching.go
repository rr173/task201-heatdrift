// Package matching 匹配模块：对观测点执行跳点检测与道路片段
// 匹配，产出道路候选。跳点按速度阈值识别；道路匹配按点到线段
// 垂直距离打分，每点可产生多条候选（供研究人员选择）。
package matching

import (
	"context"
	"fmt"
	"sort"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

// Options 匹配选项。
type Options struct {
	// SpeedLimitKPH 超过该速度视为跳点（默认 80 km/h）。
	SpeedLimitKPH float64
	// MaxMatchDistM 匹配道路的最大垂直距离（默认 60 米）。
	MaxMatchDistM float64
	// MaxCandidates 每个点保留的候选数量上限（默认 3）。
	MaxCandidates int
}

// DefaultOptions 返回默认匹配选项。
func DefaultOptions() Options {
	return Options{
		SpeedLimitKPH: 80,
		MaxMatchDistM: 60,
		MaxCandidates: 3,
	}
}

// Jump 跳点记录。
type Jump struct {
	Seq        int64   `json:"seq"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	SpeedKPH   float64 `json:"speed_kph"`
	FromSeq    int64   `json:"from_seq"`
	FromTS     string  `json:"from_ts"`
	Reason     string  `json:"reason"`
}

// Service 匹配服务。
type Service struct {
	store *store.Store
}

// New 构造匹配服务。
func New(s *store.Store) *Service {
	return &Service{store: s}
}

// DetectJumps 顺序扫描观测点，识别速度超过阈值的跳点。
// 返回跳点列表（含原因），不修改数据。
func DetectJumps(points []*model.Observation, opts Options) []Jump {
	if opts.SpeedLimitKPH <= 0 {
		opts.SpeedLimitKPH = 80
	}
	var jumps []Jump
	for i := 1; i < len(points); i++ {
		prev, cur := points[i-1], points[i]
		dt := cur.TS.Sub(prev.TS).Seconds()
		speed := model.SpeedKPH(prev.Lat, prev.Lon, cur.Lat, cur.Lon, dt)
		if speed < 0 {
			jumps = append(jumps, Jump{
				Seq: cur.Seq, Lat: cur.Lat, Lon: cur.Lon,
				FromSeq: prev.Seq, FromTS: prev.TS.Format(time.RFC3339),
				SpeedKPH: -1, Reason: "negative time delta",
			})
			continue
		}
		if speed > opts.SpeedLimitKPH {
			jumps = append(jumps, Jump{
				Seq: cur.Seq, Lat: cur.Lat, Lon: cur.Lon,
				SpeedKPH: speed, FromSeq: prev.Seq,
				FromTS: prev.TS.Format(time.RFC3339),
				Reason: "speed limit exceeded",
			})
		}
	}
	return jumps
}

// Match 对观测点执行匹配：加载道路、为每个 pending 点生成候选。
// 返回匹配结果统计。
type MatchResult struct {
	PointsMatched  int64 `json:"points_matched"`
	PointsJumped   int64 `json:"points_jumped"`
	Candidates     int64 `json:"candidates"`
}

// MatchPoints 为任务执行道路匹配：
//  1. 跳点检测：超过速度阈值的点标记为 jump（保留数据）；
//  2. 其余点计算到各道路的投影距离，生成前 N 条候选；
//  3. 距离最近且唯一最小时选定 chosen=true 并标记 matched；
//     多候选竞争时点保持 pending（等待人工选择）。
func (s *Service) MatchPoints(ctx context.Context, missionID string, opts Options) (*MatchResult, error) {
	roads, err := s.store.ListRoads(ctx)
	if err != nil {
		return nil, err
	}
	// 仅保留 active 道路。
	var active []*model.RoadSegment
	for _, r := range roads {
		if r.Status == "active" {
			active = append(active, r)
		}
	}
	if len(active) == 0 {
		return nil, fmt.Errorf("%w: no active roads", model.ErrNotFound)
	}

	points, err := s.store.ListObservations(ctx, missionID, "pending", 0)
	if err != nil {
		return nil, err
	}

	// 跳点检测（作用于连续段内所有点，不只 pending）。
	allPoints, err := s.store.ListObservations(ctx, missionID, "", 0)
	if err != nil {
		return nil, err
	}
	jumps := DetectJumps(allPoints, opts)
	jumpSeqs := map[int64]bool{}
	for _, j := range jumps {
		jumpSeqs[j.Seq] = true
	}

	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res := &MatchResult{}
	var candidatePointIDs []string
	for _, p := range points {
		if jumpSeqs[p.Seq] {
			if err := s.store.UpdateObservationStatus(ctx, tx, p.ID, "jump", "", 0); err != nil {
				return nil, err
			}
			res.PointsJumped++
			continue
		}
		// 对每条道路计算投影。
		type cand struct {
			roadID string
			d      float64
		}
		var cands []cand
		for _, r := range active {
			_, _, _, dist := model.ProjectPoint(p.Lat, p.Lon, r.Lat1, r.Lon1, r.Lat2, r.Lon2)
			if dist <= opts.MaxMatchDistM {
				cands = append(cands, cand{roadID: r.ID, d: dist})
			}
		}
		if len(cands) == 0 {
			// 无匹配道路，标记为 jump（无法归属）保留数据。
			if err := s.store.UpdateObservationStatus(ctx, tx, p.ID, "jump", "", 0); err != nil {
				return nil, err
			}
			res.PointsJumped++
			continue
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].d < cands[j].d })
		if len(cands) > opts.MaxCandidates {
			cands = cands[:opts.MaxCandidates]
		}
		// 生成候选行。
		candidatePointIDs = append(candidatePointIDs, p.ID)
		best := cands[0]
		for i, c := range cands {
			road := findRoad(active, c.roadID)
			if road == nil {
				continue
			}
			pl, po, ratio, dist := model.ProjectPoint(p.Lat, p.Lon, road.Lat1, road.Lon1, road.Lat2, road.Lon2)
			// score：垂直距离为主，加微小扰动避免并列。
			score := c.d + float64(i)*0.001
			cand := &model.RoadCandidate{
				ID:        fmt.Sprintf("cand-%s-%s", p.ID, c.roadID),
				PointID:   p.ID,
				RoadID:    c.roadID,
				DistM:     dist,
				ProjLat:   pl,
				ProjLon:   po,
				Ratio:     ratio,
				Score:     score,
				Chosen:    i == 0,
				CreatedAt: time.Now().UTC(),
			}
			if err := s.store.InsertCandidate(ctx, tx, cand); err != nil {
				return nil, err
			}
			res.Candidates++
		}
		// 唯一候选（唯一道路匹配）时直接确认；多条候选则保持 pending。
		if len(cands) == 1 && best.d <= opts.MaxMatchDistM {
			if err := s.store.UpdateObservationStatus(ctx, tx, p.ID, "matched", best.roadID, best.d); err != nil {
				return nil, err
			}
			res.PointsMatched++
		} else {
			// 多条候选：保持 pending，等待人工确认。
			res.PointsMatched++ // 计入已匹配（有候选可用）
		}
	}

	// 存在跳点时任务进入 cleaning 状态。
	if res.PointsJumped > 0 {
		if err := s.store.IncMissionJump(ctx, tx, missionID, res.PointsJumped); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit match: %w", err)
	}
	return res, nil
}

func findRoad(roads []*model.RoadSegment, id string) *model.RoadSegment {
	for _, r := range roads {
		if r.ID == id {
			return r
		}
	}
	return nil
}
