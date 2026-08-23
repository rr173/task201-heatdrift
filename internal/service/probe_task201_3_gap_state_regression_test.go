package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/ingest"
	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

func TestBug03PipelinePreservesTelemetryGapState(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "gap.db"))
	if err != nil { t.Fatal(err) }
	defer s.Close()
	svc := NewServices(s, nil)
	now := time.Now().UTC()
	if err := s.CreateDevice(context.Background(), &model.Device{ID:"d", Name:"d", Serial:"sg", Lat:39.9, Lon:116.4, Status:"active", CreatedAt:now}); err != nil { t.Fatal(err) }
	if err := s.CreateRoad(context.Background(), &model.RoadSegment{ID:"r", Name:"r", Lat1:39.9, Lon1:116.4, Lat2:39.91, Lon2:116.41, BaseTemp:28, Status:"active", CreatedAt:now}); err != nil { t.Fatal(err) }
	if err := s.CreateMission(context.Background(), &model.Mission{ID:"m", DeviceID:"d", Name:"m", Status:"running", CreatedAt:now, UpdatedAt:now}); err != nil { t.Fatal(err) }
	_, err = svc.Ingest.Ingest(context.Background(), "m", ingest.BatchInput{Points: []ingest.PointInput{
		{Seq:1, TS:now, Lat:39.9, Lon:116.4, Temp:30},
		{Seq:2, TS:now.Add(121*time.Second), Lat:39.9001, Lon:116.4001, Temp:30.1},
	}})
	if err != nil { t.Fatal(err) }
	if _, err := svc.RunPipeline(context.Background(), "m"); err != nil { t.Fatal(err) }
	m, err := s.GetMission(context.Background(), "m")
	if err != nil { t.Fatal(err) }
	if m.Status != "gapped" { t.Fatalf("gap state was lost after pipeline: %s", m.Status) }
}
