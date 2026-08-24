# task201-heatdrift 城市热岛移动传感轨迹去漂移服务

面向城市气候研究：清洗骑行传感器轨迹，把温度读数投影到道路片段并识别 GPS 漂移。

## 业务闭环

1. 气候研究人员注册采集设备，登记城市道路片段（含背景温度基线）。
2. 创建采集任务，批量上传带时间戳与坐标的观测点（`(mission_id, seq)` 幂等）。
3. 匹配模块检测 GPS 跳点（速度超阈值/时间倒退/坐标越界），对正常点做道路匹配生成候选。
4. 研究人员选择漂移修正策略（snap 吸附 / interpolate 插值 / discard 剔除）并设置温度响应延迟。
5. 校正模块重建校正点序列，轨迹模块聚合成连续热岛轨迹段。
6. 复核确认轨迹段后发布研究版本（绑定修正策略），旧版本自动替代；发布后新点只进入新版本。

## 核心状态机

- 采集任务：`running → gapped（缺口）/ cleaning（待清洗）→ completed`
- 观测点：`pending → matched / jump → discarded`
- 轨迹段：`draft → corrected → review（需复核）→ confirmed`
- 版本：`computing → published → superseded`（superseded 为冻结终态，不可重新发布）

## 关键不变量

- 同一任务内设备序号唯一；重复上传按 `(mission_id, seq)` 幂等跳过。
- 拒绝坐标越界、时间倒退、未知设备；任务完成后拒绝直接覆盖（`ErrFrozen`）。
- 发布版本绑定修正策略与参数快照；旧 published 版本在发布新版本时自动 superseded。
- superseded 为冻结终态：已被替代的版本不可重新发布，也不会因此错误替代当前发布版本。
- 同一任务匹配游标串行推进，重启后从最后游标继续接收。

## 标准命令

```bash
export GOTOOLCHAIN=local GOVERSION=1.26.3 CGO_ENABLED=0
export GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn

# 构建
CGO_ENABLED=0 GOTOOLCHAIN=local go build ./...
# 静态检查
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
# 单元测试
CGO_ENABLED=0 GOTOOLCHAIN=local go test ./...
# 端到端自检（创建数据 → 关闭重开数据库 → 验证恢复）
go run ./cmd/heatdrift --smoke-test
# 启动服务（默认 :8080，db=heatdrift.db）
go run ./cmd/heatdrift --addr :8080 --db heatdrift.db
```

## API 一览（前缀 /api，35 个）

| 能力 | 方法与路径 |
| --- | --- |
| 注册设备 | `POST /api/devices` |
| 设备列表/详情 | `GET /api/devices`、`GET /api/devices/{id}` |
| 归档设备 | `POST /api/devices/{id}/archive` |
| 登记道路 | `POST /api/roads` |
| 道路列表/详情 | `GET /api/roads`、`GET /api/roads/{id}` |
| 停用道路 | `POST /api/roads/{id}/retire` |
| 创建采集任务 | `POST /api/missions` |
| 任务列表/详情 | `GET /api/missions`、`GET /api/missions/{id}` |
| 批量上传观测点 | `POST /api/missions/{id}/observations` |
| 观测点列表/详情 | `GET /api/missions/{id}/observations`、`GET /api/missions/{id}/observations/{seq}` |
| 跳点列表 | `GET /api/missions/{id}/jumps` |
| 执行匹配 | `POST /api/missions/{id}/match` |
| 设置/查询校正参数 | `POST /api/missions/{id}/correction`、`GET /api/missions/{id}/correction` |
| 运行完整流水线 | `POST /api/missions/{id}/pipeline` |
| 轨迹段列表 | `GET /api/missions/{id}/segments` |
| 任务统计 | `GET /api/missions/{id}/stats` |
| 完成任务 | `POST /api/missions/{id}/complete` |
| 候选列表 | `GET /api/points/{pointId}/candidates` |
| 选择候选 | `POST /api/points/{pointId}/candidates/{candidateId}/choose` |
| 轨迹段详情 | `GET /api/segments/{id}` |
| 标记需复核 | `POST /api/segments/{id}/review` |
| 复核确认 | `POST /api/segments/{id}/confirm` |
| 标注列表 | `GET /api/segments/{id}/annotations` |
| 创建/列表版本 | `POST /api/missions/{id}/versions`、`GET /api/missions/{id}/versions` |
| 版本详情 | `GET /api/versions/{id}` |
| 发布版本 | `POST /api/versions/{id}/publish` |
| 替代版本 | `POST /api/versions/{id}/supersede` |
| 系统统计 | `GET /api/system/stats` |
| 健康检查 | `GET /api/health` |

## 目录结构

```
env/
├── cmd/heatdrift/          # 入口（--addr / --db / --smoke-test）
├── internal/
│   ├── model/              # 实体、状态机、地理工具、领域错误
│   ├── store/              # SQLite 持久化（建表迁移 + CRUD + 幂等 + 游标）
│   ├── ingest/             # 采集模块：幂等接收、校验、缺口检测
│   ├── matching/           # 匹配模块：跳点检测、道路投影匹配、候选生成
│   ├── correction/         # 校正模块：延迟校正、漂移修正策略（snap/interpolate/discard）
│   ├── track/              # 轨迹模块：段聚合、统计、复核确认
│   ├── version/            # 版本模块：发布/替代/冻结
│   ├── service/            # 编排层（流水线）
│   └── httpapi/            # HTTP 层（35 个 API，前缀 /api）
├── Dockerfile / benzhi.Dockerfile
└── build_benzhi_docker.sh
```
