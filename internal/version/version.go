// Package version 版本模块：管理冻结的轨迹研究版本。
// 版本状态机 computing -> published -> superseded。
// 发布版本绑定修正策略与校正参数快照；已发布版本不可直接覆盖，
// 只能通过新版本替代（model.ErrFrozen 拒绝直接改写）。
package version

import (
	"context"
	"fmt"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

// Service 版本服务。
type Service struct {
	store *store.Store
}

// New 构造版本服务。
func New(s *store.Store) *Service {
	return &Service{store: s}
}

// CreateInput 创建版本输入。
type CreateInput struct {
	MissionID   string
	Strategy    string
	DelaySec    float64
	SegmentCnt  int
	PointCount  int64
}

// Create 创建计算中的版本。若任务已有已发布版本且输入完全相同，
// 视为重复并返回 ErrConflict。
func (s *Service) Create(ctx context.Context, in CreateInput) (*model.Version, error) {
	mission, err := s.store.GetMission(ctx, in.MissionID)
	if err != nil {
		return nil, err
	}
	if mission.Status == "completed" && !missionHasChanges(ctx, s.store, in) {
		return nil, fmt.Errorf("%w: mission completed and unchanged", model.ErrConflict)
	}
	number, err := s.store.NextVersionNumber(ctx, in.MissionID)
	if err != nil {
		return nil, err
	}
	v := &model.Version{
		ID:           fmt.Sprintf("ver-%s-%d", in.MissionID, number),
		MissionID:    in.MissionID,
		Number:       number,
		Status:       "computing",
		Strategy:     in.Strategy,
		DelaySec:     in.DelaySec,
		SegmentCount: in.SegmentCnt,
		PointCount:   in.PointCount,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.store.CreateVersion(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// missionHasChanges 占位：确认是否存在未发布变化（简化判定）。
func missionHasChanges(ctx context.Context, st *store.Store, in CreateInput) bool {
	// 版本数量 > 0 且最新版本已发布且参数相同则无变化。
	versions, err := st.ListVersions(ctx, in.MissionID)
	if err != nil {
		return true
	}
	if len(versions) == 0 {
		return true
	}
	last := versions[len(versions)-1]
	if last.Status == "published" && last.Strategy == in.Strategy && last.SegmentCount == in.SegmentCnt {
		return false
	}
	return true
}

// List 列出任务版本。
func (s *Service) List(ctx context.Context, missionID string) ([]*model.Version, error) {
	return s.store.ListVersions(ctx, missionID)
}

// Get 查询版本。
func (s *Service) Get(ctx context.Context, id string) (*model.Version, error) {
	return s.store.GetVersion(ctx, id)
}

// Publish 发布版本：状态校验 computing -> published。
// 若任务已有其它 published 版本，旧版本自动替代（superseded）。
func (s *Service) Publish(ctx context.Context, id string) (*model.Version, error) {
	v, err := s.store.GetVersion(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := model.TransitionVersion(v.Status, "published"); err != nil {
		return nil, err
	}
	if err := s.store.PublishVersion(ctx, v); err != nil {
		return nil, err
	}
	// 任务标记完成（发布后不允许再接收新点或直接覆盖）。
	if err := s.store.SetMissionStatus(ctx, v.MissionID, "completed"); err != nil {
		return nil, err
	}
	v.Status = "published"
	return v, nil
}

// Supersede 替代版本：published -> superseded。
func (s *Service) Supersede(ctx context.Context, id string) (*model.Version, error) {
	v, err := s.store.GetVersion(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := model.TransitionVersion(v.Status, "superseded"); err != nil {
		return nil, err
	}
	if err := s.store.SupersedeVersion(ctx, v.ID); err != nil {
		return nil, err
	}
	v.Status = "superseded"
	return v, nil
}
