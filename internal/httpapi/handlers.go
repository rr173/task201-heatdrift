package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"task201-heatdrift/internal/ingest"
	"task201-heatdrift/internal/matching"
	"task201-heatdrift/internal/model"
	"task201-heatdrift/internal/service"
	"task201-heatdrift/internal/version"
)

// Handler HTTP 处理器，持有全部服务。
type Handler struct {
	svc *service.Services
}

// NewHandler 构造处理器。
func NewHandler(svc *service.Services) *Handler {
	return &Handler{svc: svc}
}

// mapErr 将领域错误映射为 HTTP 状态码。
func mapErr(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, model.ErrNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, model.ErrConflict), errors.Is(err, model.ErrFrozen):
		return http.StatusConflict, err.Error()
	case errors.Is(err, model.ErrBadState):
		return http.StatusConflict, err.Error()
	case errors.Is(err, model.ErrInvalid), errors.Is(err, model.ErrOutOfOrder),
		errors.Is(err, model.ErrOutOfBounds):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, model.ErrUnknownDevice):
		return http.StatusNotFound, err.Error()
	default:
		return http.StatusInternalServerError, "internal error: " + err.Error()
	}
}

// writeErr 统一错误写出。
func writeErr(w http.ResponseWriter, err error) {
	status, msg := mapErr(err)
	writeError(w, status, msg)
}

// ---------- 设备 ----------

type deviceInput struct {
	Name   string  `json:"name"`
	Serial string  `json:"serial"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

// CreateDevice POST /api/devices
func (h *Handler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	var in deviceInput
	if !parseBody(w, r, &in) {
		return
	}
	if in.Name == "" || in.Serial == "" {
		writeError(w, http.StatusBadRequest, "name and serial required")
		return
	}
	if !model.ValidCoord(in.Lat, in.Lon) {
		writeError(w, http.StatusBadRequest, "invalid coordinates")
		return
	}
	d := &model.Device{
		ID:        "dev-" + in.Serial,
		Name:      in.Name,
		Serial:    in.Serial,
		Lat:       in.Lat,
		Lon:       in.Lon,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := h.svc.Store.CreateDevice(r.Context(), d); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// ListDevices GET /api/devices
func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.svc.Store.ListDevices(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

// GetDevice GET /api/devices/{id}
func (h *Handler) GetDevice(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Store.GetDevice(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// ArchiveDevice POST /api/devices/{id}/archive
func (h *Handler) ArchiveDevice(w http.ResponseWriter, r *http.Request) {
	id := pathValue(r, "id")
	d, err := h.svc.Store.GetDevice(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.svc.Store.SetDeviceStatus(r.Context(), d.ID, "archived"); err != nil {
		writeErr(w, err)
		return
	}
	d.Status = "archived"
	writeJSON(w, http.StatusOK, d)
}

// ---------- 道路 ----------

type roadInput struct {
	Name     string  `json:"name"`
	Lat1     float64 `json:"lat1"`
	Lon1     float64 `json:"lon1"`
	Lat2     float64 `json:"lat2"`
	Lon2     float64 `json:"lon2"`
	BaseTemp float64 `json:"base_temp"`
}

// CreateRoad POST /api/roads
func (h *Handler) CreateRoad(w http.ResponseWriter, r *http.Request) {
	var in roadInput
	if !parseBody(w, r, &in) {
		return
	}
	if in.Name == "" || !model.ValidCoord(in.Lat1, in.Lon1) || !model.ValidCoord(in.Lat2, in.Lon2) {
		writeError(w, http.StatusBadRequest, "invalid road input")
		return
	}
	rd := &model.RoadSegment{
		ID:        "road-" + in.Name,
		Name:      in.Name,
		Lat1:      in.Lat1,
		Lon1:      in.Lon1,
		Lat2:      in.Lat2,
		Lon2:      in.Lon2,
		BaseTemp:  in.BaseTemp,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := h.svc.Store.CreateRoad(r.Context(), rd); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rd)
}

// ListRoads GET /api/roads
func (h *Handler) ListRoads(w http.ResponseWriter, r *http.Request) {
	roads, err := h.svc.Store.ListRoads(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roads)
}

// GetRoad GET /api/roads/{id}
func (h *Handler) GetRoad(w http.ResponseWriter, r *http.Request) {
	rd, err := h.svc.Store.GetRoad(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rd)
}

// RetireRoad POST /api/roads/{id}/retire
func (h *Handler) RetireRoad(w http.ResponseWriter, r *http.Request) {
	id := pathValue(r, "id")
	if err := h.svc.Store.SetRoadStatus(r.Context(), id, "retired"); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "retired"})
}

// ---------- 采集任务 ----------

type missionInput struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

// CreateMission POST /api/missions
func (h *Handler) CreateMission(w http.ResponseWriter, r *http.Request) {
	var in missionInput
	if !parseBody(w, r, &in) {
		return
	}
	if in.DeviceID == "" || in.Name == "" {
		writeError(w, http.StatusBadRequest, "device_id and name required")
		return
	}
	if _, err := h.svc.Store.GetDevice(r.Context(), in.DeviceID); err != nil {
		writeErr(w, err)
		return
	}
	now := time.Now().UTC()
	m := &model.Mission{
		ID:        "miss-" + in.Name,
		DeviceID:  in.DeviceID,
		Name:      in.Name,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.svc.Store.CreateMission(r.Context(), m); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// ListMissions GET /api/missions?device_id=
func (h *Handler) ListMissions(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	missions, err := h.svc.Store.ListMissions(r.Context(), deviceID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, missions)
}

// GetMission GET /api/missions/{id}
func (h *Handler) GetMission(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.Store.GetMission(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// UploadObservations POST /api/missions/{id}/observations
func (h *Handler) UploadObservations(w http.ResponseWriter, r *http.Request) {
	missionID := pathValue(r, "id")
	var in ingest.BatchInput
	if !parseBody(w, r, &in) {
		return
	}
	res, err := h.svc.Ingest.Ingest(r.Context(), missionID, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ListObservations GET /api/missions/{id}/observations?status=&limit=
func (h *Handler) ListObservations(w http.ResponseWriter, r *http.Request) {
	missionID := pathValue(r, "id")
	status := r.URL.Query().Get("status")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	obs, err := h.svc.Store.ListObservations(r.Context(), missionID, status, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obs)
}

// GetObservation GET /api/missions/{id}/observations/{seq}
func (h *Handler) GetObservation(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(pathValue(r, "seq"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid seq")
		return
	}
	o, err := h.svc.Store.GetObservation(r.Context(), pathValue(r, "id"), seq)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// ListJumps GET /api/missions/{id}/jumps
func (h *Handler) ListJumps(w http.ResponseWriter, r *http.Request) {
	missionID := pathValue(r, "id")
	points, err := h.svc.Store.ListObservations(r.Context(), missionID, "jump", 0)
	if err != nil {
		writeErr(w, err)
		return
	}
	jumps := make([]matching.Jump, 0, len(points))
	for _, p := range points {
		jumps = append(jumps, matching.Jump{
			Seq: p.Seq, Lat: p.Lat, Lon: p.Lon,
			FromSeq: p.Seq - 1, FromTS: p.TS.Format(time.RFC3339),
			Reason: "detected jump",
		})
	}
	writeJSON(w, http.StatusOK, jumps)
}

// RunMatch POST /api/missions/{id}/match
func (h *Handler) RunMatch(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Matching.MatchPoints(r.Context(), pathValue(r, "id"), matching.DefaultOptions())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------- 校正参数 ----------

type correctionInput struct {
	DelaySec   float64 `json:"delay_sec"`
	TempOffset float64 `json:"temp_offset"`
	Strategy   string  `json:"strategy"`
}

// SetCorrection POST /api/missions/{id}/correction
func (h *Handler) SetCorrection(w http.ResponseWriter, r *http.Request) {
	missionID := pathValue(r, "id")
	var in correctionInput
	if !parseBody(w, r, &in) {
		return
	}
	switch in.Strategy {
	case "snap", "interpolate", "discard":
	default:
		writeError(w, http.StatusBadRequest, "strategy must be snap/interpolate/discard")
		return
	}
	cp := &model.CorrectionParams{
		ID:            "corr-" + missionID,
		MissionID:     missionID,
		DelaySec:      in.DelaySec,
		TempOffset:    in.TempOffset,
		Strategy:      in.Strategy,
		SpeedLimitKPH: 80,
		CreatedAt:     time.Now().UTC(),
	}
	if err := h.svc.Store.UpsertCorrection(r.Context(), cp); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cp)
}

// GetCorrection GET /api/missions/{id}/correction
func (h *Handler) GetCorrection(w http.ResponseWriter, r *http.Request) {
	cp, err := h.svc.Store.GetCorrection(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cp)
}

// RunPipeline POST /api/missions/{id}/pipeline
func (h *Handler) RunPipeline(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.RunPipeline(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ListSegments GET /api/missions/{id}/segments
func (h *Handler) ListSegments(w http.ResponseWriter, r *http.Request) {
	segs, err := h.svc.Store.ListSegments(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, segs)
}

// MissionStats GET /api/missions/{id}/stats
func (h *Handler) MissionStats(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.MissionStats(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// CompleteMission POST /api/missions/{id}/complete
func (h *Handler) CompleteMission(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.CompleteMission(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// ---------- 候选 ----------

// ListCandidates GET /api/points/{pointId}/candidates
func (h *Handler) ListCandidates(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Store.ListCandidatesByPoint(r.Context(), pathValue(r, "pointId"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

// ChooseCandidate POST /api/points/{pointId}/candidates/{candidateId}/choose
// 从竞争候选中选择一个，将观测点置为 matched。
func (h *Handler) ChooseCandidate(w http.ResponseWriter, r *http.Request) {
	pointID := pathValue(r, "pointId")
	candID := pathValue(r, "candidateId")
	cs, err := h.svc.Store.ListCandidatesByPoint(r.Context(), pointID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var chosen *model.RoadCandidate
	for _, c := range cs {
		if c.ID == candID {
			chosen = c
			break
		}
	}
	if chosen == nil {
		writeErr(w, model.ErrNotFound)
		return
	}
	obs, err := h.svc.Store.GetObservationByID(r.Context(), pointID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.svc.Store.UpdateObservationStatus(r.Context(), h.svc.Store.DB(), obs.ID, "matched", chosen.RoadID, chosen.DistM); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chosen)
}

// ---------- 轨迹段 ----------

// GetSegment GET /api/segments/{id}
func (h *Handler) GetSegment(w http.ResponseWriter, r *http.Request) {
	seg, err := h.svc.Store.GetSegment(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, seg)
}

// MarkSegmentReview POST /api/segments/{id}/review
func (h *Handler) MarkSegmentReview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note string `json:"note"`
	}
	if !parseBody(w, r, &in) {
		return
	}
	seg, err := h.svc.Track.MarkReview(r.Context(), pathValue(r, "id"), in.Note)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, seg)
}

// ConfirmSegment POST /api/segments/{id}/confirm
func (h *Handler) ConfirmSegment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Author string `json:"author"`
		Note   string `json:"note"`
	}
	if !parseBody(w, r, &in) {
		return
	}
	seg, err := h.svc.Track.Confirm(r.Context(), pathValue(r, "id"), in.Author, in.Note)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, seg)
}

// ListAnnotations GET /api/segments/{id}/annotations
func (h *Handler) ListAnnotations(w http.ResponseWriter, r *http.Request) {
	anns, err := h.svc.Store.ListAnnotations(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, anns)
}

// ---------- 版本 ----------

type versionInput struct {
	Strategy   string `json:"strategy"`
	DelaySec   float64 `json:"delay_sec"`
	PointCount int64  `json:"point_count"`
}

// CreateVersion POST /api/missions/{id}/versions
func (h *Handler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	missionID := pathValue(r, "id")
	var in versionInput
	if !parseBody(w, r, &in) {
		return
	}
	if in.Strategy == "" {
		in.Strategy = "snap"
	}
	segs, err := h.svc.Store.ListSegments(r.Context(), missionID)
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := h.svc.Version.Create(r.Context(), version.CreateInput{
		MissionID:  missionID,
		Strategy:   in.Strategy,
		DelaySec:   in.DelaySec,
		SegmentCnt: len(segs),
		PointCount: in.PointCount,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// ListVersions GET /api/missions/{id}/versions
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := h.svc.Version.List(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

// GetVersion GET /api/versions/{id}
func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Version.Get(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// PublishVersion POST /api/versions/{id}/publish
func (h *Handler) PublishVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Version.Publish(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// SupersedeVersion POST /api/versions/{id}/supersede
func (h *Handler) SupersedeVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Version.Supersede(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ---------- 系统 ----------

// SystemStats GET /api/system/stats
func (h *Handler) SystemStats(w http.ResponseWriter, r *http.Request) {
	devices, missions, roads, versions, published, err := h.svc.Store.SystemStats(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.SystemStats{
		Devices: devices, Missions: missions, Roads: roads,
		Versions: versions, Published: published,
	})
}

// Health GET /api/health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Health(r.Context()); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseFloat 辅助解析可选浮点参数。
func parseFloat(s string, def float64) float64 {
	if strings.TrimSpace(s) == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}
