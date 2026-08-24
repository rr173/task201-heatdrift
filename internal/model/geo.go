package model

import "math"

// 地理常量（WGS84 近似）。
const (
	earthRadiusM = 6371008.8
	degToRad     = math.Pi / 180.0
)

// HaversineM 计算两个经纬度点间的大圆距离（米）。
func HaversineM(lat1, lon1, lat2, lon2 float64) float64 {
	f1 := lat1 * degToRad
	f2 := lat2 * degToRad
	df := (lat2 - lat1) * degToRad
	dl := (lon2 - lon1) * degToRad
	a := math.Sin(df/2)*math.Sin(df/2) +
		math.Cos(f1)*math.Cos(f2)*math.Sin(dl/2)*math.Sin(dl/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

// ProjectPoint 计算点 (plat,plon) 到线段 (lat1,lon1)-(lat2,lon2) 的
// 垂直投影。返回 (投影纬度, 投影经度, 投影比例 0~1, 垂直距离米)。
// 采用等距圆柱近似（统一参考纬度 cos 缩放）将经纬度折算为米制平面
// 坐标再投影，避免不同纬度缩放不一致导致的几何扭曲。
func ProjectPoint(plat, plon, lat1, lon1, lat2, lon2 float64) (float64, float64, float64, float64) {
	latRef := (lat1 + lat2) / 2
	cosRef := math.Cos(latRef * degToRad)
	x1, y1 := lat1*degToRad*earthRadiusM, lon1*degToRad*earthRadiusM*cosRef
	x2, y2 := lat2*degToRad*earthRadiusM, lon2*degToRad*earthRadiusM*cosRef
	xp, yp := plat*degToRad*earthRadiusM, plon*degToRad*earthRadiusM*cosRef

	dx, dy := x2-x1, y2-y1
	lenSq := dx*dx + dy*dy
	var ratio float64
	if lenSq > 1e-12 {
		ratio = ((xp-x1)*dx + (yp-y1)*dy) / lenSq
		ratio = math.Max(0, math.Min(1, ratio))
	}
	px := x1 + ratio*dx
	py := y1 + ratio*dy
	dist := math.Hypot(xp-px, yp-py)
	projLat := px / (degToRad * earthRadiusM)
	projLon := py / (degToRad * earthRadiusM * cosRef)
	return projLat, projLon, ratio, dist
}

// SpeedKPH 由两个连续观测点计算移动速度（km/h）。
// 返回 -1 表示时间差非正（异常）。
func SpeedKPH(lat1, lon1, lat2, lon2 float64, dtSec float64) float64 {
	if dtSec <= 0 {
		return -1
	}
	d := HaversineM(lat1, lon1, lat2, lon2)
	return d / dtSec * 3.6
}

// ValidCoord 校验经纬度范围。
// 边界值合法：纬度可达极点 ±90，经度可在反子午线 ±180。
func ValidCoord(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// Round2 保留两位小数。
func Round2(v float64) float64 {
	return math.Round(v*100) / 100
}
