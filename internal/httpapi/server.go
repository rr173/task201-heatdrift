// Package httpapi 提供城市热岛移动传感轨迹去漂移服务的 JSON HTTP API。
// 路由统一以 /api 开头，使用标准库 net/http ServeMux（Go 1.22+ 方法路由）。
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// Server HTTP 服务器。
type Server struct {
	mux     *http.ServeMux
	handler *Handler
	logger  *log.Logger
}

// New 构造 HTTP 服务器并注册全部路由。
func New(h *Handler, logger *log.Logger) *Server {
	s := &Server{mux: http.NewServeMux(), handler: h, logger: logger}
	s.routes()
	return s
}

// Handler 返回底层处理器（含日志与 CORS 中间件）。
func (s *Server) Handler() http.Handler {
	return corsMiddleware(loggingMiddleware(s.mux, s.logger))
}

// routes 注册全部 API 路由（前缀 /api）。
func (s *Server) routes() {
	// 设备
	s.mux.HandleFunc("POST /api/devices", s.withLog(s.handler.CreateDevice))
	s.mux.HandleFunc("GET /api/devices", s.withLog(s.handler.ListDevices))
	s.mux.HandleFunc("GET /api/devices/{id}", s.withLog(s.handler.GetDevice))
	s.mux.HandleFunc("POST /api/devices/{id}/archive", s.withLog(s.handler.ArchiveDevice))
	// 道路
	s.mux.HandleFunc("POST /api/roads", s.withLog(s.handler.CreateRoad))
	s.mux.HandleFunc("GET /api/roads", s.withLog(s.handler.ListRoads))
	s.mux.HandleFunc("GET /api/roads/{id}", s.withLog(s.handler.GetRoad))
	s.mux.HandleFunc("POST /api/roads/{id}/retire", s.withLog(s.handler.RetireRoad))
	// 采集任务
	s.mux.HandleFunc("POST /api/missions", s.withLog(s.handler.CreateMission))
	s.mux.HandleFunc("GET /api/missions", s.withLog(s.handler.ListMissions))
	s.mux.HandleFunc("GET /api/missions/{id}", s.withLog(s.handler.GetMission))
	s.mux.HandleFunc("POST /api/missions/{id}/observations", s.withLog(s.handler.UploadObservations))
	s.mux.HandleFunc("GET /api/missions/{id}/observations", s.withLog(s.handler.ListObservations))
	s.mux.HandleFunc("GET /api/missions/{id}/observations/{seq}", s.withLog(s.handler.GetObservation))
	s.mux.HandleFunc("GET /api/missions/{id}/jumps", s.withLog(s.handler.ListJumps))
	s.mux.HandleFunc("POST /api/missions/{id}/match", s.withLog(s.handler.RunMatch))
	s.mux.HandleFunc("POST /api/missions/{id}/correction", s.withLog(s.handler.SetCorrection))
	s.mux.HandleFunc("GET /api/missions/{id}/correction", s.withLog(s.handler.GetCorrection))
	s.mux.HandleFunc("POST /api/missions/{id}/pipeline", s.withLog(s.handler.RunPipeline))
	s.mux.HandleFunc("GET /api/missions/{id}/segments", s.withLog(s.handler.ListSegments))
	s.mux.HandleFunc("GET /api/missions/{id}/stats", s.withLog(s.handler.MissionStats))
	s.mux.HandleFunc("POST /api/missions/{id}/complete", s.withLog(s.handler.CompleteMission))
	// 候选
	s.mux.HandleFunc("GET /api/points/{pointId}/candidates", s.withLog(s.handler.ListCandidates))
	s.mux.HandleFunc("POST /api/points/{pointId}/candidates/{candidateId}/choose", s.withLog(s.handler.ChooseCandidate))
	// 轨迹段
	s.mux.HandleFunc("GET /api/segments/{id}", s.withLog(s.handler.GetSegment))
	s.mux.HandleFunc("POST /api/segments/{id}/review", s.withLog(s.handler.MarkSegmentReview))
	s.mux.HandleFunc("POST /api/segments/{id}/confirm", s.withLog(s.handler.ConfirmSegment))
	s.mux.HandleFunc("GET /api/segments/{id}/annotations", s.withLog(s.handler.ListAnnotations))
	// 版本
	s.mux.HandleFunc("POST /api/missions/{id}/versions", s.withLog(s.handler.CreateVersion))
	s.mux.HandleFunc("GET /api/missions/{id}/versions", s.withLog(s.handler.ListVersions))
	s.mux.HandleFunc("GET /api/versions/{id}", s.withLog(s.handler.GetVersion))
	s.mux.HandleFunc("POST /api/versions/{id}/publish", s.withLog(s.handler.PublishVersion))
	s.mux.HandleFunc("POST /api/versions/{id}/supersede", s.withLog(s.handler.SupersedeVersion))
	// 系统
	s.mux.HandleFunc("GET /api/system/stats", s.withLog(s.handler.SystemStats))
	s.mux.HandleFunc("GET /api/health", s.withLog(s.handler.Health))
}

// withLog 包装处理器：记录方法与路径，统一 panic 恢复。
func (s *Server) withLog(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		s.logger.Printf("%s %s", r.Method, r.URL.Path)
		next(w, r)
	}
}

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 编码失败时仅记录。
		return
	}
}

// apiError 错误响应体。
type apiError struct {
	Error string `json:"error"`
}

// writeError 输出错误响应。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// parseBody 解析 JSON 请求体。拒绝未知字段，避免拼写错误或多余字段
// 被静默吞掉而绕过必填校验（如缺少 name/serial 的设备请求）。
func parseBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return false
	}
	return true
}

// pathValue 读取路径参数。
func pathValue(r *http.Request, key string) string {
	return strings.TrimSpace(r.PathValue(key))
}
