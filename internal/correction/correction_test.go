package correction

import (
	"math"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
)

func TestApplyDelay(t *testing.T) {
	temps := []float64{20, 21, 22, 23, 24}
	// 零延迟：原样输出。
	out0 := ApplyDelay(temps, nil, 0)
	for i, v := range out0 {
		if v != temps[i] {
			t.Fatalf("zero delay expected same value at %d, got %v", i, v)
		}
	}
	// 延迟>0：输出应平滑，首值不变。
	out := ApplyDelay(temps, nil, 2)
	if out[0] != 20 {
		t.Fatalf("first value should stay 20, got %v", out[0])
	}
	if out[len(out)-1] >= 24 {
		t.Fatalf("delayed series should not reach raw max 24, got %v", out[len(out)-1])
	}
}

func TestCompensateOffset(t *testing.T) {
	temps := []float64{20, 21, 22}
	out := CompensateOffset(temps, 0.5)
	if out[0] != 20.5 || out[2] != 22.5 {
		t.Fatalf("unexpected offset result: %v", out)
	}
}

func mkObs(seq int64, ts time.Time, lat, lon, temp float64, status, road string) *model.Observation {
	return &model.Observation{
		ID: "", MissionID: "m", Seq: seq, TS: ts,
		Lat: lat, Lon: lon, Temp: temp, Status: status, MatchedRoad: road,
	}
}

func TestRebuildDiscard(t *testing.T) {
	base := time.Now()
	points := []*model.Observation{
		mkObs(1, base, 39.9030, 116.4050, 28.0, "matched", "r1"),
		mkObs(2, base.Add(10*time.Second), 39.9040, 116.4060, 28.1, "jump", ""),
		mkObs(3, base.Add(20*time.Second), 39.9050, 116.4070, 28.2, "matched", "r1"),
	}
	// discard：剔除跳点。
	out := Rebuild(points, nil, Params{DelaySec: 1}, StrategyDiscard)
	if len(out) != 2 {
		t.Fatalf("discard expected 2 points, got %d", len(out))
	}
	if out[0].Seq != 1 || out[1].Seq != 3 {
		t.Fatalf("unexpected seqs: %d %d", out[0].Seq, out[1].Seq)
	}
}

func TestRebuildInterpolate(t *testing.T) {
	base := time.Now()
	points := []*model.Observation{
		mkObs(1, base, 39.9030, 116.4050, 28.0, "matched", "r1"),
		mkObs(2, base.Add(10*time.Second), 39.9040, 116.4060, 28.1, "jump", ""),
		mkObs(3, base.Add(20*time.Second), 39.9050, 116.4070, 28.2, "matched", "r1"),
	}
	out := Rebuild(points, nil, Params{DelaySec: 1}, StrategyInterpolate)
	if len(out) != 3 {
		t.Fatalf("interpolate expected 3 points, got %d", len(out))
	}
	if out[1].Status != "interpolated" {
		t.Fatalf("expected interpolated status, got %s", out[1].Status)
	}
	if math.Abs(out[1].Temp-28.1) > 1e-6 {
		t.Fatalf("interpolated temp should be midpoint 28.1, got %v", out[1].Temp)
	}
}

func TestRebuildSnap(t *testing.T) {
	base := time.Now()
	points := []*model.Observation{
		mkObs(1, base, 39.9030, 116.4050, 28.0, "jump", ""),
	}
	// 唯一候选：吸附到道路投影。
	cand := map[string][]*model.RoadCandidate{
		"": {
			{ID: "c1", PointID: "", RoadID: "r9", ProjLat: 39.9035, ProjLon: 116.4055, DistM: 30},
		},
	}
	_ = cand
	// 使用真实点 ID 构造候选映射。
	p := points[0]
	p.ID = "p1"
	cand2 := map[string][]*model.RoadCandidate{
		"p1": {
			{ID: "c1", PointID: "p1", RoadID: "r9", ProjLat: 39.9035, ProjLon: 116.4055, DistM: 30},
		},
	}
	out := Rebuild(points, cand2, Params{DelaySec: 1}, StrategySnap)
	if len(out) != 1 {
		t.Fatalf("snap expected 1 point, got %d", len(out))
	}
	if out[0].RoadID != "r9" || out[0].Status != "snapped" {
		t.Fatalf("expected snapped to r9, got road=%s status=%s", out[0].RoadID, out[0].Status)
	}
}
