package httpapi

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"task201-heatdrift/internal/service"
	"task201-heatdrift/internal/store"
)

func TestBug06GeographicBoundaryCoordinatesAreAccepted(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "boundary.db")); if err != nil { t.Fatal(err) }; defer s.Close()
	srv := New(NewHandler(service.NewServices(s, log.New(io.Discard, "", 0))), log.New(io.Discard, "", 0)).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/devices", bytes.NewBufferString(`{"name":"pole","serial":"sb","lat":90,"lon":180}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder(); srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated { t.Fatalf("valid boundary coordinates were rejected: %d %s", rr.Code, rr.Body.String()) }
}
