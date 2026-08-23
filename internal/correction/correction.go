// Package correction 校正模块：按传感器响应延迟校正温度序列，
// 并实现三种漂移修正策略（snap 吸附 / interpolate 插值 / discard 剔除）。
// 校正结果用于重算轨迹段，不修改原始观测点。
package correction

import (
	"math"

	"task201-heatdrift/internal/model"
)

// Strategy 修正策略标识。
type Strategy string

// 支持的修正策略。
const (
	StrategySnap         Strategy = "snap"         // 跳点吸附到最近道路投影
	StrategyInterpolate  Strategy = "interpolate"  // 跳点按前后点线性插值
	StrategyDiscard      Strategy = "discard"      // 剔除跳点
)

// Params 校正参数（与 CorrectionParams 对齐）。
type Params struct {
	DelaySec   float64 // 传感器响应延迟（秒）
	TempOffset float64 // 系统温漂补偿
	Strategy   Strategy
}

// ApplyDelay 对温度序列应用传感器响应延迟校正：
// 采用一阶滞后近似（指数平滑），等效于把读数按时间常数移动。
// 返回校正后的温度切片（长度与输入一致）。
func ApplyDelay(temps []float64, timesSec []float64, delaySec float64) []float64 {
	if delaySec <= 0 {
		out := make([]float64, len(temps))
		copy(out, temps)
		return out
	}
	alpha := 1.0 - math.Exp(-1.0/delaySec) // 单位时间步的平滑系数
	out := make([]float64, len(temps))
	prev := temps[0]
	for i := 0; i < len(temps); i++ {
		if i == 0 {
			out[i] = temps[i]
			prev = temps[i]
			continue
		}
		// 一阶滞后：out[i] = out[i-1] + alpha*(in[i]-out[i-1])
		cur := prev + alpha*(temps[i]-prev)
		out[i] = cur
		prev = cur
	}
	return out
}

// CorrectedPoint 校正后的轨迹点（用于轨迹段聚合）。
type CorrectedPoint struct {
	Seq        int64
	Lat        float64
	Lon        float64
	Temp       float64
	RoadID     string
	DistM      float64
	Status     string // matched / interpolated / snapped / discarded
}

// Rebuild 按策略修正一条观测序列，产出可用的校正点序列。
//
//   - snap：将 jump 点坐标替换为已选定候选的投影坐标（保留温度）；
//   - interpolate：对 jump 点温度与坐标做前后线性插值；
//   - discard：直接剔除 jump 点。
//
// 无候选的 jump 点在 snap 策略下无法吸附，将被剔除。
func Rebuild(points []*model.Observation, candidates map[string][]*model.RoadCandidate, params Params, strategy Strategy) []CorrectedPoint {
	// 先按索引建立 seq 有序切片。
	sorted := make([]*model.Observation, len(points))
	copy(sorted, points)

	var out []CorrectedPoint
	// 收集可用点索引（discard 跳过 jump）。
	usable := make([]int, 0, len(sorted))
	for i, p := range sorted {
		if p.Status == "discarded" {
			continue
		}
		if p.Status == "jump" && strategy == StrategyDiscard {
			continue
		}
		usable = append(usable, i)
	}
	if len(usable) == 0 {
		return out
	}
	_ = params
	for idx, i := range usable {
		p := sorted[i]
		switch p.Status {
		case "matched":
			out = append(out, CorrectedPoint{
				Seq: p.Seq, Lat: p.Lat, Lon: p.Lon, Temp: p.Temp,
				RoadID: p.MatchedRoad, DistM: p.DistM, Status: "matched",
			})
		case "pending":
			// 多候选竞争：若唯一候选则吸附，否则保持原坐标。
			cs := candidates[p.ID]
			if len(cs) == 1 {
				out = append(out, CorrectedPoint{
					Seq: p.Seq, Lat: cs[0].ProjLat, Lon: cs[0].ProjLon, Temp: p.Temp,
					RoadID: cs[0].RoadID, DistM: cs[0].DistM, Status: "snapped",
				})
			} else {
				out = append(out, CorrectedPoint{
					Seq: p.Seq, Lat: p.Lat, Lon: p.Lon, Temp: p.Temp,
					RoadID: "", DistM: 0, Status: "pending",
				})
			}
		case "jump":
			switch strategy {
			case StrategySnap:
				cs := candidates[p.ID]
				if len(cs) == 1 {
					out = append(out, CorrectedPoint{
						Seq: p.Seq, Lat: cs[0].ProjLat, Lon: cs[0].ProjLon, Temp: p.Temp,
						RoadID: cs[0].RoadID, DistM: cs[0].DistM, Status: "snapped",
					})
				}
				// 无候选：剔除（不输出）。
			case StrategyInterpolate:
				// 用前后可用点插值。
				if idx == 0 || idx == len(usable)-1 {
					// 边界跳点无法插值，剔除。
					continue
				}
				prev := sorted[usable[idx-1]]
				next := sorted[usable[idx+1]]
				span := next.TS.Sub(prev.TS).Seconds()
				if span <= 0 {
					continue
				}
				frac := p.TS.Sub(prev.TS).Seconds() / span
				lat := prev.Lat + frac*(next.Lat-prev.Lat)
				lon := prev.Lon + frac*(next.Lon-prev.Lon)
				temp := prev.Temp + frac*(next.Temp-prev.Temp)
				road := prev.MatchedRoad
				if road == "" {
					road = next.MatchedRoad
				}
				out = append(out, CorrectedPoint{
					Seq: p.Seq, Lat: lat, Lon: lon, Temp: temp,
					RoadID: road, DistM: 0, Status: "interpolated",
				})
			default: // discard
				continue
			}
		default:
			out = append(out, CorrectedPoint{
				Seq: p.Seq, Lat: p.Lat, Lon: p.Lon, Temp: p.Temp,
				RoadID: p.MatchedRoad, DistM: p.DistM, Status: p.Status,
			})
		}
	}
	return out
}

// CompensateOffset 应用系统温漂补偿。
func CompensateOffset(temps []float64, offset float64) []float64 {
	out := make([]float64, len(temps))
	for i, t := range temps {
		out[i] = t + offset
	}
	return out
}
