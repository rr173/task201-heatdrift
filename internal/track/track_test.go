package track

import (
	"testing"
	"time"

	"task201-heatdrift/internal/correction"
)

func mkPoint(seq int64, ts time.Time, lat, lon, temp float64, road, status string) correction.CorrectedPoint {
	return correction.CorrectedPoint{
		Seq: seq, Lat: lat, Lon: lon, Temp: temp, RoadID: road, Status: status,
	}
}

func TestAssembleStats(t *testing.T) {
	base := time.Now()
	pts := []correction.CorrectedPoint{
		mkPoint(1, base, 39.9030, 116.4050, 28.0, "r1", "matched"),
		mkPoint(2, base.Add(10*time.Second), 39.9040, 116.4060, 30.0, "r1", "matched"),
		mkPoint(3, base.Add(20*time.Second), 39.9050, 116.4070, 32.0, "r1", "matched"),
	}
	seg, err := (&Service{}).assemble("m1", pts, "snap")
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if seg.TempMean != 30 {
		t.Fatalf("expected mean 30, got %v", seg.TempMean)
	}
	if seg.TempMax != 32 {
		t.Fatalf("expected max 32, got %v", seg.TempMax)
	}
	if seg.StartSeq != 1 || seg.EndSeq != 3 {
		t.Fatalf("unexpected seq range: %d-%d", seg.StartSeq, seg.EndSeq)
	}
	if seg.RoadID != "r1" {
		t.Fatalf("expected road r1, got %s", seg.RoadID)
	}
	if seg.LengthM <= 0 {
		t.Fatalf("expected positive length, got %v", seg.LengthM)
	}
}

func TestAssembleRoadChange(t *testing.T) {
	base := time.Now()
	pts := []correction.CorrectedPoint{
		mkPoint(1, base, 39.9030, 116.4050, 28.0, "r1", "matched"),
		mkPoint(2, base.Add(10*time.Second), 39.9040, 116.4060, 28.1, "r2", "matched"),
	}
	seg, err := (&Service{}).assemble("m1", pts, "snap")
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	// 多数原则：两点道路不同取后者（最后一个非空）。
	if seg.RoadID != "r2" {
		t.Fatalf("expected r2, got %s", seg.RoadID)
	}
}

func TestAssembleEmpty(t *testing.T) {
	if _, err := (&Service{}).assemble("m1", nil, "snap"); err == nil {
		t.Fatal("expected error for empty points")
	}
}
