package matching

import (
	"testing"
	"time"

	"task201-heatdrift/internal/model"
)

func mkObs(seq int64, ts time.Time, lat, lon, temp float64) *model.Observation {
	return &model.Observation{
		ID: "o", MissionID: "m", Seq: seq, TS: ts,
		Lat: lat, Lon: lon, Temp: temp, Status: "pending",
	}
}

func TestDetectJumps(t *testing.T) {
	base := time.Now()
	points := []*model.Observation{
		mkObs(1, base, 39.9030, 116.4050, 28.0),
		mkObs(2, base.Add(10*time.Second), 39.9040, 116.4060, 28.1),
		// 跳点：1 秒内移动约 600 米 -> 2160 km/h，远超阈值。
		mkObs(3, base.Add(11*time.Second), 39.9080, 116.4120, 30.0),
		// 恢复点：从跳点缓慢移回道路附近（20 秒，距离 < 阈值）。
		mkObs(4, base.Add(31*time.Second), 39.9060, 116.4100, 28.3),
	}
	jumps := DetectJumps(points, Options{SpeedLimitKPH: 80})
	if len(jumps) != 1 {
		t.Fatalf("expected 1 jump, got %d", len(jumps))
	}
	if jumps[0].Seq != 3 {
		t.Fatalf("expected jump at seq 3, got %d", jumps[0].Seq)
	}
	// 时间倒退点返回负速度并记为 jump。
	backward := []*model.Observation{
		mkObs(1, base, 39.9030, 116.4050, 28.0),
		mkObs(2, base.Add(-5*time.Second), 39.9040, 116.4060, 28.1),
	}
	if jb := DetectJumps(backward, Options{SpeedLimitKPH: 80}); len(jb) != 1 {
		t.Fatalf("expected backward jump detected, got %d", len(jb))
	}
}

func TestProjectionCandidates(t *testing.T) {
	// 直接用 ProjectPoint 验证候选生成的核心几何。
	// 点 (39.9042,116.4100) 在水平道路 rd (39.9042,116.4050)-(39.9042,116.4150) 上。
	pl, po, ratio, dist := model.ProjectPoint(39.9042, 116.4100, 39.9042, 116.4050, 39.9042, 116.4150)
	if ratio < 0.49 || ratio > 0.51 {
		t.Fatalf("expected ratio 0.5, got %v", ratio)
	}
	if dist > 1 {
		t.Fatalf("expected on-road dist ~0, got %v", dist)
	}
	_ = pl
	_ = po
}

func TestDetectNoJumpsOnSmoothTrack(t *testing.T) {
	base := time.Now()
	var points []*model.Observation
	lat, lon := 39.9030, 116.4050
	for i := 0; i < 20; i++ {
		points = append(points, mkObs(int64(i+1), base.Add(time.Duration(i)*15*time.Second), lat, lon, 28.0))
		lat += 0.0002
		lon += 0.0003
	}
	jumps := DetectJumps(points, Options{SpeedLimitKPH: 80})
	if len(jumps) != 0 {
		t.Fatalf("expected no jumps, got %d", len(jumps))
	}
}
