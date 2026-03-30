# cc-gateway

`cc-gateway` 是一个面向 Anthropic Messages API 的统一网关。

它对外暴露兼容 Anthropic 的 `POST /v1/messages` 接口，内部可以把请求路由到不同上游账号，并在需要时完成面向 Claude Code 主链路的 best-effort 协议转换：

- Anthropic -> 原样透传
- OpenAI -> 转到 `Responses API`（主链路兼容）
- Gemini -> 转到 `streamGenerateContent`（主链路兼容）
- Custom OpenAI / Custom Anthropic -> 对接兼容实现

项目同时内置：

- SQLite 持久化的账号、分组、API Key 管理
- Web 管理后台和 Admin API
- 熔断、并发限制、负载均衡
- 请求日志、Token 用量和成本统计
- Prometheus 指标

## 适用场景

- 给 Claude Code / Anthropic SDK 提供统一入口
- 用 Anthropic 协议接入 OpenAI、Gemini 或自建兼容服务
- 把多个上游账号做成一个可控的账号池
- 对外发放网关级 API Key，统一做限流、配额和审计

## 核心特性

- 对外统一入口：`/v1/messages`
- 仅接收 `POST`
- 请求体按 Anthropic Messages API 解析
- 服务端会强制使用流式模式
- 支持 `anthropic`、`openai`、`gemini`、`custom_openai`、`custom_anthropic`
- Anthropic 原生透传；OpenAI / Gemini 以 Claude Code 文本、工具调用、thinking 主链路兼容为目标
- 支持模型白名单和模型别名映射
- 支持 `round_robin`、`least_connections`、`weighted`、`priority`
- 账号级熔断器和并发限制
- API Key 级并发限制和月度 token 配额
- 请求日志、payload 保留和清理
- 管理端前端静态资源可嵌入后端二进制

## 架构说明

请求路径分成两部分：

- 业务入口：`http://<gateway>:8080/v1/messages`
- 管理入口：`http://<gateway>:8081/`

运行时大致流程如下：

1. 客户端按 Anthropic Messages API 调用 `/v1/messages`
2. 网关校验网关 API Key（如果系统里已经创建了 key）
3. 根据 key -> group -> accounts 选择可用上游账号
4. 按账号类型决定是透传还是做协议翻译
5. 记录日志、token 用量、成本和指标

兼容边界说明：

- `anthropic` / `custom_anthropic`：按 Anthropic Messages 透传
- `openai`：面向 Claude Code 主链路做 Anthropic -> Responses best-effort 映射
- `gemini`：面向 Claude Code 主链路做 Anthropic -> `streamGenerateContent` best-effort 映射
- 目前不承诺 Anthropic 多模态 / 结构化输出等全部语义在所有上游都完整等价

配置来源分两类：

- `config.yaml`：监听地址、SQLite 路径、日志保留、价格表
- SQLite：accounts、groups、api_keys、request_logs 等运行数据

也就是说，账号、分组、网关 API Key 不是写在 YAML 里，而是通过 Admin API / 管理后台维护。

## 快速开始

### 1. 准备配置

```bash
cp config.example.yaml config.yaml
```

按需修改：

- `server.listen`：业务入口，默认 `:8080`
- `server.admin_listen`：管理入口，默认 `:8081`
- `database.path`：SQLite 文件位置，默认 `./data/gateway.db`
- `pricing`：按模型模式匹配的价格表，用于成本统计

`config.yaml` 支持 `${ENV_VAR}` 形式的环境变量展开。

### 2. 启动服务

```bash
go run ./cmd/gateway --config config.yaml
```

或者直接构建：

```bash
make build
./cc-gateway --config config.yaml
```

### 3. 访问管理端

默认地址：

- 管理后台：`http://127.0.0.1:8081/`
- Admin API：`http://127.0.0.1:8081/admin/...`
- Metrics：`http://127.0.0.1:8081/metrics`

首次启动数据库是空的，需要先创建：

1. 上游账号 `accounts`
2. 账号组 `groups`
3. 网关 API Key `keys`

### 4. 配置客户端

网关对外是 Anthropic 风格接口，因此客户端只需要把 base URL 指向网关。

例如直接请求：

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H 'content-type: application/json' \
  -H 'x-api-key: <gateway-api-key>' \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

如果你使用 Claude Code / 兼容 Anthropic 的客户端，通常只需要把：

- `ANTHROPIC_BASE_URL` 指向网关地址
- 认证 token 改成网关签发的 API Key

网关接受两种认证头：

- `x-api-key: <key>`
- `Authorization: Bearer <key>`

注意：只有在系统里存在至少一个网关 API Key 时，业务入口才会启用鉴权；如果还没有创建任何 key，业务入口会处于无鉴权状态，方便冷启动初始化。

## 管理模型

### Account

一个 `account` 代表一个具体的上游账号或代理实例，包含：

- provider 类型
- 上游 API Key
- base URL / proxy URL / user agent
- 允许模型
- 模型映射
- 最大并发
- 熔断参数

### Group

一个 `group` 是账号池，包含：

- 多个 account
- 一种负载策略
- 可选的模型白名单

网关 API Key 会绑定到某个 group，请求最终从该 group 中选一个健康账号发出。

### API Key

一个 `api_key` 是对外发放给客户端使用的网关密钥，支持：

- 启停状态
- group 绑定
- 单 key 最大并发
- 月度输入 / 输出 token 配额
- 模型白名单

## 负载均衡和故障处理

- `round_robin`：轮询
- `least_connections`：优先活跃请求最少的账号
- `weighted`：按 `max_concurrent` 作为权重分流，未设置时按 100 处理
- `priority`：按 `account_ids` 顺序优先，前一个不可用时才切下一个

账号被选中前会经过这些过滤：

- 账号必须是 `enabled`
- 熔断器必须允许放行
- 账号必须能服务当前模型
- 如果当前账号并发已满，会尝试切换到其他候选账号

## 配置与热更新

### YAML 配置

示例见 [config.example.yaml](/Users/songzhibin/go/src/Songzhibin/cc-gateway/config.example.yaml)。

主要字段：

- `server.*`：监听端口与超时
- `database.path`：SQLite 路径
- `log.payload_retention_days`：payload 保留天数
- `log.cleanup_interval`：后台清理周期
- `pricing[]`：模型价格规则

### 热更新行为

- 给进程发送 `SIGHUP` 会重新加载 `config.yaml`
- 当前实现热更新的是配置文件内容，最重要的是 `pricing`
- accounts / groups / api_keys 的变更来自 Admin API，写库后会即时 reload 到内存路由

## 管理端与 API

管理端运行在独立端口，前端资源由 Go 服务直接内嵌提供。

如果设置了环境变量 `ADMIN_TOKEN`，所有 `/admin/*` 请求都需要：

```text
Authorization: Bearer <ADMIN_TOKEN>
```

但 `/metrics` 不受该认证保护，方便 Prometheus 抓取。

详细接口文档见 [docs/admin-api.md](/Users/songzhibin/go/src/Songzhibin/cc-gateway/docs/admin-api.md)。

## 监控与审计

### 健康检查

- 业务入口：`GET /health`
- 管理入口：`GET /admin/health`

### Prometheus 指标

暴露在 `GET /metrics`，包括：

- `ccgateway_requests_total`
- `ccgateway_request_duration_seconds`
- `ccgateway_tokens_total`
- `ccgateway_active_requests`
- `ccgateway_circuit_breaker_state`
- `ccgateway_cost_usd_total`

### 日志与成本

SQLite 中会保存：

- 请求基础日志
- 请求 / 响应 payload（按保留期清理）
- API Key 月度用量

成本统计依赖 `pricing` 配置，按模型模式匹配。

## 开发

### 后端

```bash
go test ./...
go run ./cmd/gateway --config config.yaml
```

### 前端

```bash
cd web
npm install
npm run dev
```

前端开发默认跑在 `5173`，并代理：

- `/admin` -> `http://localhost:8081`
- `/metrics` -> `http://localhost:8081`

### 一体化构建

```bash
make build
```

对应 Makefile 目标：

- `make frontend`
- `make backend`
- `make clean`
- `make dev-frontend`
- `make dev-backend`

## 目录结构

```text
cmd/gateway/           程序入口
internal/proxy/        对外消息入口、鉴权、转发
internal/router/       路由、负载均衡、熔断集成
internal/provider/     Anthropic/OpenAI/Gemini 适配器与协议翻译
internal/admin/        Admin API 与管理端服务
internal/store/        SQLite 持久化
internal/accounting/   成本计算、日志记录、清理
internal/metrics/      Prometheus 指标
web/                   React + Vite 管理后台
docs/admin-api.md      Admin API 说明
config.example.yaml    配置示例
```

## 当前实现边界

- 对外只暴露 `POST /v1/messages`
- 当前实现按 Anthropic Messages API 作为统一请求模型
- 服务端会强制走流式响应
- 管理数据依赖 SQLite，默认是单文件部署模型

如果你要把它作为生产网关，建议至少补上：

- 完整的部署脚本和 systemd / container 化
- 更细的审计与告警
- 回归测试和 provider 兼容性测试
- 更明确的限流 / 超时 / 重试策略
