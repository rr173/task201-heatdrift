// Package model 定义城市热岛移动传感轨迹去漂移服务的领域实体、
// 状态机常量与领域错误。所有实体以 ID 字符串为主键，时间统一
// 使用 RFC3339 UTC 文本存储。
package model

import "time"

// ---------- 设备 ----------

// Device 传感器设备（车载温度/定位采集器）。
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Serial    string    `json:"serial"`
	Lat       float64   `json:"lat"`       // 部署基准纬度
	Lon       float64   `json:"lon"`       // 部署基准经度
	Status    string    `json:"status"`    // active / archived
	CreatedAt time.Time `json:"created_at"`
}

// ---------- 采集任务 ----------

// Mission 一次骑行采集任务。状态机：
// running -> gapped（检测到缺口）| cleaning（待清洗）-> completed
// 记录 device_seq 游标 cursor，用于重启后续传匹配。
type Mission struct {
	ID          string    `json:"id"`
	DeviceID    string    `json:"device_id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"` // running / gapped / cleaning / completed
	Cursor      int64     `json:"cursor"` // 已接收的最大设备序号
	PointCount  int64     `json:"point_count"`
	JumpCount   int64     `json:"jump_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ---------- 观测点 ----------

// Observation 单条 GPS+温度观测。状态机：
// pending（待匹配）-> matched（已匹配）/ jump（跳点）-> discarded（剔除）。
// 同一任务内 device_seq 幂等。
type Observation struct {
	ID         string    `json:"id"`
	MissionID  string    `json:"mission_id"`
	Seq        int64     `json:"seq"`         // 设备序号
	TS         time.Time `json:"ts"`          // 采样时刻
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	Temp       float64   `json:"temp"`        // 原始温度 ℃
	Status     string    `json:"status"`      // pending / matched / jump / discarded
	MatchedRoad string   `json:"matched_road"` // 匹配到的道路 ID
	DistM      float64   `json:"dist_m"`      // 到匹配道路的垂直距离
	CreatedAt  time.Time `json:"created_at"`
}

// ---------- 道路片段 ----------

// RoadSegment 城市道路片段（带参考温度基线，用于热岛对比）。
type RoadSegment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Lat1      float64   `json:"lat1"`   // 起点
	Lon1      float64   `json:"lon1"`
	Lat2      float64   `json:"lat2"`   // 终点
	Lon2      float64   `json:"lon2"`
	BaseTemp  float64   `json:"base_temp"` // 城市背景温度基线
	Status    string    `json:"status"`    // active / retired
	CreatedAt time.Time `json:"created_at"`
}

// RoadCandidate 观测点与道路的匹配候选（可能多点竞争）。
type RoadCandidate struct {
	ID        string    `json:"id"`
	PointID   string    `json:"point_id"`
	RoadID    string    `json:"road_id"`
	DistM     float64   `json:"dist_m"`     // 垂直距离
	ProjLat   float64   `json:"proj_lat"`   // 投影点
	ProjLon   float64   `json:"proj_lon"`
	Ratio     float64   `json:"ratio"`      // 投影比例 0~1
	Score     float64   `json:"score"`      // 分数（越小越优）
	Chosen    bool      `json:"chosen"`     // 是否被选用
	CreatedAt time.Time `json:"created_at"`
}

// ---------- 校正参数 ----------

// CorrectionParams 温度响应延迟与漂移修正参数。
type CorrectionParams struct {
	ID            string    `json:"id"`
	MissionID     string    `json:"mission_id"`
	DelaySec      float64   `json:"delay_sec"`      // 传感器响应延迟（秒）
	TempOffset    float64   `json:"temp_offset"`    // 系统温漂补偿
	Strategy      string    `json:"strategy"`       // snap / interpolate / discard
	SpeedLimitKPH float64   `json:"speed_limit_kph"` // 跳点速度阈值
	CreatedAt     time.Time `json:"created_at"`
}

// ---------- 轨迹段 ----------

// TrackSegment 连续热岛轨迹段。状态机：
// draft -> corrected -> review（需复核）-> confirmed。
type TrackSegment struct {
	ID        string    `json:"id"`
	MissionID string    `json:"mission_id"`
	RoadID    string    `json:"road_id"`
	StartSeq  int64     `json:"start_seq"`
	EndSeq    int64     `json:"end_seq"`
	Status    string    `json:"status"` // draft / corrected / review / confirmed
	TempMean  float64   `json:"temp_mean"`
	TempMax   float64   `json:"temp_max"`
	LengthM   float64   `json:"length_m"`
	Strategy  string    `json:"strategy"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ---------- 版本 ----------

// Version 冻结的轨迹版本。状态机：
// computing -> published -> superseded。
type Version struct {
	ID            string    `json:"id"`
	MissionID     string    `json:"mission_id"`
	Number        int       `json:"number"`
	Status        string    `json:"status"` // computing / published / superseded
	Strategy      string    `json:"strategy"`
	DelaySec      float64   `json:"delay_sec"`
	SegmentCount  int       `json:"segment_count"`
	PointCount    int64     `json:"point_count"`
	PublishedAt   time.Time `json:"published_at"`
	SupersededAt  time.Time `json:"superseded_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// ---------- 复核标注 ----------

// Annotation 复核者对轨迹段的标注/决定。
type Annotation struct {
	ID        string    `json:"id"`
	SegmentID string    `json:"segment_id"`
	Author    string    `json:"author"`
	Note      string    `json:"note"`
	Decision  string    `json:"decision"` // confirm / reject
	CreatedAt time.Time `json:"created_at"`
}

// ---------- 统计 ----------

// MissionStats 任务统计。
type MissionStats struct {
	MissionID    string  `json:"mission_id"`
	TotalPoints  int64   `json:"total_points"`
	Matched      int64   `json:"matched"`
	Jumps        int64   `json:"jumps"`
	Discarded    int64   `json:"discarded"`
	Segments     int     `json:"segments"`
	ConfirmedSeg int     `json:"confirmed_segments"`
	MeanTemp     float64 `json:"mean_temp"`
	HeatDelta    float64 `json:"heat_delta"` // 平均温升 vs 道路基线
}

// SystemStats 系统级统计。
type SystemStats struct {
	Devices   int64 `json:"devices"`
	Missions  int64 `json:"missions"`
	Roads     int64 `json:"roads"`
	Versions  int64 `json:"versions"`
	Published int64 `json:"published_versions"`
}
