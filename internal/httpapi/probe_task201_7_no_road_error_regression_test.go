package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"task201-heatdrift/internal/service"
	"task201-heatdrift/internal/store"
)

func TestBug07NoActiveRoadsRemainNotFound(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "roads.db")); if err != nil { t.Fatal(err) }; defer s.Close()
	srv := New(NewHandler(service.NewServices(s, log.New(io.Discard, "", 0))), log.New(io.Discard, "", 0)).Handler()
	post := func(path string, body any) *httptest.ResponseRecorder { b,_:=json.Marshal(body); req:=httptest.NewRequest(http.MethodPost,path,bytes.NewReader(b)); req.Header.Set("Content-Type","application/json"); rr:=httptest.NewRecorder(); srv.ServeHTTP(rr,req); return rr }
	if rr:=post("/api/devices",map[string]any{"name":"d","serial":"snr","lat":1,"lon":2}); rr.Code!=201 { t.Fatalf("device %d %s",rr.Code,rr.Body.String()) }
	if rr:=post("/api/missions",map[string]any{"device_id":"dev-snr","name":"noroad"}); rr.Code!=201 { t.Fatalf("mission %d %s",rr.Code,rr.Body.String()) }
	if rr:=post("/api/missions/miss-noroad/match",nil); rr.Code!=http.StatusNotFound { t.Fatalf("no-road lookup returned %d, body=%s",rr.Code,rr.Body.String()) }
}
