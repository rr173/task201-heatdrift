package version

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

func TestBug04SupersededVersionCannotBeRepublished(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "version.db"))
	if err != nil { t.Fatal(err) }
	defer s.Close()
	now := time.Now().UTC()
	if err := s.CreateDevice(context.Background(), &model.Device{ID:"d", Name:"d", Serial:"sv", Lat:1, Lon:2, Status:"active", CreatedAt:now}); err != nil { t.Fatal(err) }
	if err := s.CreateMission(context.Background(), &model.Mission{ID:"m", DeviceID:"d", Name:"m", Status:"running", CreatedAt:now, UpdatedAt:now}); err != nil { t.Fatal(err) }
	svc := New(s)
	v1, err := svc.Create(context.Background(), CreateInput{MissionID:"m", Strategy:"snap", SegmentCnt:1}); if err != nil { t.Fatal(err) }
	v2, err := svc.Create(context.Background(), CreateInput{MissionID:"m", Strategy:"interpolate", SegmentCnt:2}); if err != nil { t.Fatal(err) }
	if _, err := svc.Publish(context.Background(), v1.ID); err != nil { t.Fatal(err) }
	if _, err := svc.Publish(context.Background(), v2.ID); err != nil { t.Fatal(err) }
	if _, err := svc.Publish(context.Background(), v1.ID); err == nil { t.Fatal("republished a superseded version") }
	versions, err := svc.List(context.Background(), "m"); if err != nil { t.Fatal(err) }
	if versions[0].Status != "superseded" || versions[1].Status != "published" { t.Fatalf("unexpected statuses: %+v", versions) }
}
