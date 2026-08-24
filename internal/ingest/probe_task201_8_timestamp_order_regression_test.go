package ingest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/store"
)

func TestBug08BackwardTelemetryTimestampIsRejected(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "time.db")); if err != nil { t.Fatal(err) }; defer s.Close()
	now:=time.Now().UTC()
	if err:=s.CreateDevice(context.Background(),&model.Device{ID:"d",Name:"d",Serial:"st",Lat:1,Lon:2,Status:"active",CreatedAt:now});err!=nil{t.Fatal(err)}
	if err:=s.CreateMission(context.Background(),&model.Mission{ID:"m",DeviceID:"d",Name:"m",Status:"running",CreatedAt:now,UpdatedAt:now});err!=nil{t.Fatal(err)}
	res,err:=New(s).Ingest(context.Background(),"m",BatchInput{Points:[]PointInput{{Seq:1,TS:now,Lat:1,Lon:2,Temp:20},{Seq:2,TS:now.Add(-time.Second),Lat:1.0001,Lon:2.0001,Temp:20.1}}})
	if err!=nil{t.Fatal(err)}
	if res.Accepted!=1 || res.Rejected!=1 { t.Fatalf("backward timestamp was accepted: %+v",res) }
}
