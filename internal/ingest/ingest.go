// Package ingest 采集模块：接收设备观测点，执行幂等去重、
// 坐标/时间校验，并推进任务游标。同一任务内设备序号唯一，
// 重启后从最后游标继续接收。
package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

// 采集配置。
const (
	// MaxBatchSize 单次批量上传上限。
	MaxBatchSize = 500
	// MaxGapSec 相邻点最大允许时间间隔（秒），超过视为缺口。
	MaxGapSec = 120.0
)

// Service 采集服务。
type Service struct {
	store *store.Store
}

// New 构造采集服务。
func New(s *store.Store) *Service {
	return &Service{store: s}
}

// PointInput 单条观测输入。
type PointInput struct {
	Seq  int64     `json:"seq"`
	TS   time.Time `json:"ts"`
	Lat  float64   `json:"lat"`
	Lon  float64   `json:"lon"`
	Temp float64   `json:"temp"`
}

// BatchInput 批量上传请求。
type BatchInput struct {
	Points []PointInput `json:"points"`
}

// BatchResult 批量上传结果。
type BatchResult struct {
	Accepted int64    `json:"accepted"`  // 新插入点数
	Duplicated int64  `json:"duplicated"` // 幂等跳过的点数
	Rejected int64    `json:"rejected"`  // 校验失败点数
	Errors   []string `json:"errors"`
	Cursor  int64     `json:"cursor"`
}

// Ingest 校验并持久化观测点。
// 规则：
//   - 设备必须存在且未归档；
//   - 坐标在合法范围；
//   - 时间戳不得早于任务内最后一个点（时间倒退拒绝）；
//   - (mission_id, seq) 冲突视为重复，跳过（幂等）。
//
// 若出现时间倒退或坐标越界，整批中该点被拒绝并记录错误，
// 其余合法点继续落库。
func (s *Service) Ingest(ctx context.Context, missionID string, input BatchInput) (*BatchResult, error) {
	if len(input.Points) > MaxBatchSize {
		return nil, fmt.Errorf("%w: batch too large (%d > %d)", model.ErrInvalid, len(input.Points), MaxBatchSize)
	}
	mission, err := s.store.GetMission(ctx, missionID)
	if err != nil {
		return nil, err
	}
	device, err := s.store.GetDevice(ctx, mission.DeviceID)
	if err != nil {
		return nil, err
	}
	if device.Status == "archived" {
		return nil, fmt.Errorf("%w: device %s archived", model.ErrBadState, device.ID)
	}
	lastSeq, err := s.store.LastObservationSeq(ctx, missionID)
	if err != nil {
		return nil, err
	}
	lastTS := time.Time{}
	if lastSeq > 0 {
		last, err := s.store.GetObservation(ctx, missionID, lastSeq)
		if err == nil {
			lastTS = last.TS
		}
	}

	res := &BatchResult{}
	// 时间倒退检查：seq 必须严格递增，且 ts 不得早于 lastTS。
	// seq <= 游标 时：若该 seq 已存在则视为重复（幂等），否则视为乱序拒绝。
	prevTS := lastTS
	prevSeq := lastSeq

	var newPoints []*model.Observation
	for i := range input.Points {
		p := input.Points[i]
		if p.Seq <= prevSeq {
			exists, err := s.store.ObservationExists(ctx, missionID, p.Seq)
			if err != nil {
				return nil, err
			}
			if exists {
				res.Duplicated++
				continue
			}
			res.Rejected++
			res.Errors = append(res.Errors, fmt.Sprintf("seq %d: not after %d", p.Seq, prevSeq))
			continue
		}
		if !model.ValidCoord(p.Lat, p.Lon) {
			res.Rejected++
			res.Errors = append(res.Errors, fmt.Sprintf("seq %d: coordinates out of bounds", p.Seq))
			continue
		}
		if !prevTS.IsZero() && p.TS.Before(prevTS) {
			res.Rejected++
			res.Errors = append(res.Errors, fmt.Sprintf("seq %d: timestamp goes backwards", p.Seq))
			continue
		}
		if !prevTS.IsZero() && p.TS.Sub(prevTS).Seconds() > MaxGapSec {
			// 缺口：任务进入 gapped，但点仍接收（保留原始数据）。
			if err := s.store.SetMissionStatus(ctx, missionID, "gapped"); err != nil {
				return nil, err
			}
		}
		obs := &model.Observation{
			ID:        fmt.Sprintf("obs-%s-%d", missionID, p.Seq),
			MissionID: missionID,
			Seq:       p.Seq,
			TS:        p.TS,
			Lat:       p.Lat,
			Lon:       p.Lon,
			Temp:      p.Temp,
			Status:    "pending",
			CreatedAt: time.Now().UTC(),
		}
		newPoints = append(newPoints, obs)
		prevTS = p.TS
		prevSeq = p.Seq
	}

	if len(newPoints) == 0 {
		res.Cursor = lastSeq
		return res, nil
	}

	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, obs := range newPoints {
		ok, err := s.store.InsertObservation(ctx, tx, obs)
		if err != nil {
			return nil, err
		}
		if ok {
			res.Accepted++
		} else {
			res.Duplicated++
		}
	}
	// 推进游标：cursor = 本批最大 seq。
	newCursor := prevSeq
	if err := s.store.UpdateMissionCursor(ctx, tx, missionID, newCursor, res.Accepted); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ingest: %w", err)
	}
	res.Cursor = newCursor
	return res, nil
}

// ErrEmptyBatch 空批次错误。
var ErrEmptyBatch = errors.New("empty batch")
