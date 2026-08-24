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

func TestBug10RetiredRoadStatusPersistsAndLeavesMatchingPool(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "retired.db")); if err != nil { t.Fatal(err) }; defer s.Close()
	srv:=New(NewHandler(service.NewServices(s,log.New(io.Discard,"",0))),log.New(io.Discard,"",0)).Handler()
	call:=func(method,path string,body any)*httptest.ResponseRecorder{var b []byte;if body!=nil{b,_=json.Marshal(body)};req:=httptest.NewRequest(method,path,bytes.NewReader(b));req.Header.Set("Content-Type","application/json");rr:=httptest.NewRecorder();srv.ServeHTTP(rr,req);return rr}
	if rr:=call(http.MethodPost,"/api/roads",map[string]any{"name":"ret","lat1":1,"lon1":2,"lat2":1.01,"lon2":2.01,"base_temp":28});rr.Code!=201{t.Fatalf("create road %d %s",rr.Code,rr.Body.String())}
	if rr:=call(http.MethodPost,"/api/roads/road-ret/retire",nil);rr.Code!=200{t.Fatalf("retire %d %s",rr.Code,rr.Body.String())}
	rr:=call(http.MethodGet,"/api/roads/road-ret",nil)
	var got map[string]any; if err:=json.Unmarshal(rr.Body.Bytes(),&got);err!=nil{t.Fatal(err)}
	if rr.Code!=200 || got["status"]!="retired" { t.Fatalf("retired road was revived or not persisted: %d %s",rr.Code,rr.Body.String()) }
}
