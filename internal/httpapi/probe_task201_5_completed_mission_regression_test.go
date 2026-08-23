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

func TestBug05CompletedMissionCannotBeCompletedAgain(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "completed.db")); if err != nil { t.Fatal(err) }; defer s.Close()
	svc := service.NewServices(s, log.New(io.Discard, "", 0))
	srv := New(NewHandler(svc), log.New(io.Discard, "", 0)).Handler()
	call := func(path string) *httptest.ResponseRecorder { req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(nil)); rr := httptest.NewRecorder(); srv.ServeHTTP(rr, req); return rr }
	post := func(path string, body any) *httptest.ResponseRecorder { b,_ := json.Marshal(body); req := httptest.NewRequest(http.MethodPost,path,bytes.NewReader(b)); req.Header.Set("Content-Type","application/json"); rr:=httptest.NewRecorder(); srv.ServeHTTP(rr,req); return rr }
	if rr:=post("/api/devices",map[string]any{"name":"d","serial":"sc","lat":1,"lon":2}); rr.Code!=201 { t.Fatalf("device %d %s",rr.Code,rr.Body.String()) }
	if rr:=post("/api/missions",map[string]any{"device_id":"dev-sc","name":"done"}); rr.Code!=201 { t.Fatalf("mission %d %s",rr.Code,rr.Body.String()) }
	if rr:=call("/api/missions/miss-done/complete"); rr.Code!=200 { t.Fatalf("first complete %d %s",rr.Code,rr.Body.String()) }
	if rr:=call("/api/missions/miss-done/complete"); rr.Code!=409 { t.Fatalf("completed mission was completed twice: %d %s",rr.Code,rr.Body.String()) }
}
