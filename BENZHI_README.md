# task201-heatdrift 城市热岛移动传感轨迹去漂移服务

城市气候研究人员骑行采集 GPS+温度观测点，服务检测 GPS 跳点、匹配道路片段、
按传感器响应延迟校正温度，并冻结可回溯的连续热岛轨迹版本。

## 标准命令

```bash
export GOTOOLCHAIN=local GOVERSION=1.26.3 CGO_ENABLED=0
export GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn

CGO_ENABLED=0 GOTOOLCHAIN=local go build ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet   ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go test  ./...
go run ./cmd/heatdrift --smoke-test
```

## --smoke-test 契约

`--smoke-test` 不启动长驻服务，而是真实执行：

1. 注册设备与两条道路片段；
2. 创建采集任务，上传 12 个观测点（含 1 个 GPS 跳点）；
3. 幂等重传验证去重；
4. 执行匹配：检测跳点、生成道路候选；
5. 设置校正参数（discard 策略）并运行流水线，生成轨迹段；
6. 复核确认轨迹段、创建并发布版本；
7. **关闭并重新打开同一数据库**，验证任务状态、观测点、轨迹段、版本全部恢复。

全部断言通过后以退出码 0 结束；任何断言失败以非 0 退出。

## Docker 双架构

```bash
bash build_benzhi_docker.sh task201-heatdrift linux/amd64
bash build_benzhi_docker.sh task201-heatdrift linux/arm64
docker run --rm task201-heatdrift --smoke-test   # 镜像内自检，退出码 0 为通过
```

Dockerfile 以 `golang:1.26.3-bookworm` 构建，`CGO_ENABLED=0`，产物为
`/app/heatdrift`，ENTRYPOINT + CMD `["--smoke-test"]`。

## API 摘要

全部 JSON API 前缀 `/api`（35 个）：设备注册/归档、道路登记/停用、采集任务创建/完成、
观测点幂等上传/查询、跳点查询、匹配流水线、校正参数设置、轨迹段构建/复核/确认、
版本创建/发布/替代、系统统计与健康检查。详见 README.md。
