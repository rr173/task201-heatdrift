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

func TestBug01ArchivedDeviceRejectsNewObservations(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "archived.db"))
	if err != nil { t.Fatal(err) }
	defer st.Close()
	svc := service.NewServices(st, log.New(io.Discard, "", 0))
	srv := New(NewHandler(svc), log.New(io.Discard, "", 0)).Handler()
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		var data []byte
		if body != nil { data, _ = json.Marshal(body) }
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		return rr
	}
	if rr := call(http.MethodPost, "/api/devices", map[string]any{"name":"bike","serial":"S-ARCH","lat":39.9,"lon":116.4}); rr.Code != http.StatusCreated { t.Fatalf("create device: %d %s", rr.Code, rr.Body.String()) }
	if rr := call(http.MethodPost, "/api/missions", map[string]any{"device_id":"dev-S-ARCH","name":"arch"}); rr.Code != http.StatusCreated { t.Fatalf("create mission: %d %s", rr.Code, rr.Body.String()) }
	if rr := call(http.MethodPost, "/api/devices/dev-S-ARCH/archive", nil); rr.Code != http.StatusOK { t.Fatalf("archive: %d %s", rr.Code, rr.Body.String()) }
	rr := call(http.MethodPost, "/api/missions/miss-arch/observations", map[string]any{"points": []any{map[string]any{"seq":1,"ts":"2026-01-01T00:00:00Z","lat":39.9,"lon":116.4,"temp":30}}})
	if rr.Code != http.StatusConflict { t.Fatalf("archived device accepted observations: status=%d body=%s", rr.Code, rr.Body.String()) }
}
