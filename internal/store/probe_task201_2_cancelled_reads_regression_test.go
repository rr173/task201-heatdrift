package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
)

func TestBug02CancelledReadContextIsNotIgnored(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cancel.db"))
	if err != nil { t.Fatal(err) }
	defer s.Close()
	now := time.Now().UTC()
	if err := s.CreateDevice(context.Background(), &model.Device{ID:"d", Name:"d", Serial:"sd", Lat:1, Lon:2, Status:"active", CreatedAt:now}); err != nil { t.Fatal(err) }
	if err := s.CreateRoad(context.Background(), &model.RoadSegment{ID:"r", Name:"r", Lat1:1, Lon1:2, Lat2:1.1, Lon2:2.1, Status:"active", CreatedAt:now}); err != nil { t.Fatal(err) }
	if err := s.CreateMission(context.Background(), &model.Mission{ID:"m", DeviceID:"d", Name:"m", Status:"running", CreatedAt:now, UpdatedAt:now}); err != nil { t.Fatal(err) }
	checks := []func(context.Context) error{
		func(ctx context.Context) error { _, err := s.GetDevice(ctx, "d"); return err },
		func(ctx context.Context) error { _, err := s.GetRoad(ctx, "r"); return err },
		func(ctx context.Context) error { _, err := s.GetMission(ctx, "m"); return err },
	}
	for i, check := range checks {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := check(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("read %d ignored cancellation: %v", i, err)
		}
	}
}
