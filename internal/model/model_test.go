package model

import (
	"math"
	"testing"
)

func TestHaversineM(t *testing.T) {
	// 北京到上海大致距离 ~1067 km。
	d := HaversineM(39.9042, 116.4074, 31.2304, 121.4737)
	if d < 1000000 || d > 1150000 {
		t.Fatalf("expected ~1067km, got %.0f m", d)
	}
	// 同点距离为 0。
	if d0 := HaversineM(1.0, 2.0, 1.0, 2.0); d0 != 0 {
		t.Fatalf("expected 0, got %v", d0)
	}
}

func TestProjectPoint(t *testing.T) {
	// 水平道路（经度方向），点在道路中点正上方 100 米。
	lat1, lon1, lat2, lon2 := 39.9042, 116.4050, 39.9042, 116.4150
	// 中点为经度 116.4100，在其北侧偏移 ~0.001 度纬度（约 111 米）。
	pl, po, ratio, dist := ProjectPoint(39.9051, 116.4100, lat1, lon1, lat2, lon2)
	if math.Abs(ratio-0.5) > 0.01 {
		t.Fatalf("expected ratio ~0.5, got %v", ratio)
	}
	if dist < 90 || dist > 130 {
		t.Fatalf("expected dist ~111m, got %v", dist)
	}
	if math.Abs(pl-lat1) > 0.001 || math.Abs(po-116.4100) > 0.0005 {
		t.Fatalf("projection point unexpected: %v %v", pl, po)
	}
}

func TestSpeedKPH(t *testing.T) {
	// 纬度差 0.001° ≈ 111 m，10 秒 -> 11.1 m/s ≈ 40 km/h。
	s := SpeedKPH(39.9042, 116.4050, 39.9052, 116.4050, 10)
	if s < 38 || s > 42 {
		t.Fatalf("expected ~40 km/h, got %v", s)
	}
	// 时间倒退返回 -1。
	if neg := SpeedKPH(1, 1, 2, 2, -5); neg != -1 {
		t.Fatalf("expected -1, got %v", neg)
	}
}

func TestValidCoord(t *testing.T) {
	cases := []struct {
		lat, lon float64
		want     bool
	}{
		{0, 0, true},
		{39.9, 116.4, true},
		{-90, 180, true},
		{90.1, 0, false},
		{0, 181, false},
		{0, -181, false},
	}
	for _, c := range cases {
		if got := ValidCoord(c.lat, c.lon); got != c.want {
			t.Errorf("ValidCoord(%v,%v)=%v want %v", c.lat, c.lon, got, c.want)
		}
	}
}

func TestStateTransitions(t *testing.T) {
	if err := TransitionMission("running", "cleaning"); err != nil {
		t.Fatalf("running->cleaning should be allowed: %v", err)
	}
	if err := TransitionMission("running", "completed"); err != nil {
		t.Fatalf("running->completed should be allowed: %v", err)
	}
	if err := TransitionMission("completed", "running"); err == nil {
		t.Fatal("completed->running should be rejected")
	}
	if err := TransitionObservation("pending", "matched"); err != nil {
		t.Fatalf("pending->matched should be allowed: %v", err)
	}
	if err := TransitionObservation("discarded", "matched"); err == nil {
		t.Fatal("discarded->matched should be rejected")
	}
	if err := TransitionSegment("corrected", "confirmed"); err != nil {
		t.Fatalf("corrected->confirmed should be allowed: %v", err)
	}
	if err := TransitionSegment("confirmed", "review"); err == nil {
		t.Fatal("confirmed->review should be rejected")
	}
	if err := TransitionVersion("computing", "published"); err != nil {
		t.Fatalf("computing->published should be allowed: %v", err)
	}
	if err := TransitionVersion("published", "computing"); err == nil {
		t.Fatal("published->computing should be rejected")
	}
	// published 可被新版本替代为 superseded。
	if err := TransitionVersion("published", "superseded"); err != nil {
		t.Fatalf("published->superseded should be allowed: %v", err)
	}
	// superseded 为冻结终态，禁止重新发布，否则已退出版本会重新成为当前版本。
	if err := TransitionVersion("superseded", "published"); err == nil {
		t.Fatal("superseded->published should be rejected (version frozen)")
	}
}

func TestRound2(t *testing.T) {
	if v := Round2(3.14159); v != 3.14 {
		t.Fatalf("expected 3.14, got %v", v)
	}
	if v := Round2(2.005); v != 2.01 {
		t.Fatalf("expected 2.01, got %v", v)
	}
}
