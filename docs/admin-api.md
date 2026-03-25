# Admin API 文档

Admin API 运行在独立端口（默认 `:8081`），用于管理源账号、组、API 密钥以及查看日志和统计。

## 认证

如果设置了环境变量 `ADMIN_TOKEN`，所有 `/admin/*` 请求需要携带 Bearer Token：

```
Authorization: Bearer <ADMIN_TOKEN>
```

`/metrics` 端点不需要认证（供 Prometheus 抓取）。

---

## 源账号 (Accounts)

### 列出所有账号

```
GET /admin/accounts
```

**响应** `200`:
```json
[
  {
    "id": "openai-1",
    "name": "OpenAI Mirror",
    "provider": "openai",
    "api_key": "sk-a3****b002",
    "base_url": "https://ai.last.ee",
    "proxy_url": "",
    "user_agent": "",
    "status": "enabled",
    "allowed_models": ["gpt-5.2", "claude-opus-4-6"],
    "model_aliases": {"claude-opus-4-6": "gpt-5.2"},
    "max_concurrent": 10,
    "circuit_breaker": {"failure_threshold": 5, "success_threshold": 2, "open_duration": "60s"},
    "breaker_state": "closed"
  }
]
```

### 获取单个账号

```
GET /admin/accounts/{id}
```

### 创建账号

```
POST /admin/accounts
Content-Type: application/json
```

**通用字段说明**:

| 字段 | 必填 | 说明 |
|------|------|------|
| `id` | | 唯一标识；不传时服务端自动生成 |
| `provider` | ✅ | `anthropic` / `openai` / `gemini` / `custom_openai` / `custom_anthropic` |
| `api_key` | ✅ | 上游 Provider 的 API Key（各厂商格式不同，见下方） |
| `name` | | 显示名称 |
| `base_url` | | 覆盖默认 endpoint（见各厂商默认值） |
| `proxy_url` | | HTTP 代理地址 |
| `user_agent` | | 空=透传客户端 UA，非空=覆盖（如 `claude-cli/1.0.25 (external, cli)`） |
| `status` | | `enabled`(默认) / `disabled` |
| `allowed_models` | | 允许服务的模型列表，空=全部。**跨厂商映射的 Claude 模型名也要写在这里** |
| `model_aliases` | | 模型名映射，key=客户端请求的模型名，value=实际发给 Provider 的模型名 |
| `max_concurrent` | | 并发上限，0=不限 |
| `circuit_breaker` | | 熔断配置，不填使用默认值 (failure_threshold=5, success_threshold=2, open_duration=60s) |
| `extra` | | Provider 特有的扩展参数（JSON object） |

---

#### Anthropic 账号 (`provider: "anthropic"`)

原生 Anthropic API，网关直接透传请求，无协议翻译。

```json
{
  "id": "anthropic-main",
  "name": "Anthropic Official",
  "provider": "anthropic",
  "api_key": "sk-ant-api03-xxxxxxxxx",
  "status": "enabled",
  "allowed_models": ["claude-opus-4-6", "claude-sonnet-4-20250514"],
  "max_concurrent": 10
}
```

**注意事项**:
- `api_key` 格式：`sk-ant-api03-...`
- `base_url` 默认：`https://api.anthropic.com`（通常不需要设置）
- 网关会自动透传客户端的 `anthropic-version` 和 `anthropic-beta` header
- 支持 extended thinking（thinking blocks 直接透传）
- `user_agent`：建议设为 `claude-cli/1.0.25 (external, cli)` 以匹配 Claude Code CLI 的真实 UA

---

#### OpenAI 账号 (`provider: "openai"`)

使用 OpenAI **Responses API** (`/v1/responses`)，网关自动完成 Anthropic ↔ OpenAI 协议翻译。

```json
{
  "id": "openai-mirror",
  "name": "OpenAI via Mirror",
  "provider": "openai",
  "api_key": "sk-xxxxxxxxxxxxxxxx",
  "base_url": "https://ai.last.ee",
  "status": "enabled",
  "allowed_models": ["gpt-5.2", "gpt-4o", "claude-opus-4-6", "claude-sonnet-4-20250514"],
  "model_aliases": {
    "claude-opus-4-6": "gpt-5.2",
    "claude-sonnet-4-20250514": "gpt-4o"
  },
  "max_concurrent": 20
}
```

**注意事项**:
- `api_key` 格式：`sk-...`（OpenAI 标准格式）
- `base_url` 默认：`https://api.openai.com`，使用镜像站时填镜像地址（不要带 `/v1` 后缀）
- 网关会自动拼接 `/v1/responses` 作为请求路径
- 认证方式：`Authorization: Bearer <api_key>`（网关自动处理）
- **协议翻译**：
  - Anthropic `system` → OpenAI `instructions`
  - Anthropic `tool_use`/`tool_result` → OpenAI `function_call`/`function_call_output`
  - Anthropic `thinking` blocks → OpenAI `reasoning`（best-effort 映射）
  - SSE 事件实时翻译：`response.output_text.delta` → `content_block_delta`
- **跨厂商映射**：在 `allowed_models` 中写入 Claude 模型名，`model_aliases` 中配置映射关系
- `extra` 可选字段：`{"reasoning_effort": "high", "reasoning_summary": "auto"}`，作为 OpenAI 推理模型的账号默认值；若请求自带 `thinking` 映射出的 effort，则请求值优先

---

#### Gemini 账号 (`provider: "gemini"`)

使用 Google Gemini `streamGenerateContent` API，网关自动完成 Anthropic ↔ Gemini 协议翻译。

```json
{
  "id": "gemini-official",
  "name": "Gemini Official",
  "provider": "gemini",
  "api_key": "AIzaSyXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "status": "enabled",
  "allowed_models": ["gemini-2.0-flash", "gemini-2.5-pro-preview-05-06", "claude-haiku-4-5-20251001"],
  "model_aliases": {
    "claude-haiku-4-5-20251001": "gemini-2.0-flash"
  },
  "max_concurrent": 10
}
```

**注意事项**:
- `api_key` 格式：`AIzaSy...`（Google API Key）
- `base_url` 默认：`https://generativelanguage.googleapis.com`（通常不需要设置）
- 认证方式：API Key 通过 URL query param `key=` 传递（网关自动处理，不走 header）
- **协议翻译**：
  - Anthropic `user`/`assistant` → Gemini `user`/`model`
  - Anthropic `system` → Gemini `systemInstruction`
  - Anthropic `tool_use`/`tool_result` → Gemini `functionCall`/`functionResponse`
  - JSON Schema 类型名自动转大写：`string` → `STRING`、`object` → `OBJECT` 等
  - Gemini 流式响应为累积快照，网关自动计算增量 delta
- `proxy_url`：如果 Gemini API 需要通过代理访问，在此配置
- `extra` 可选字段：`{"thinking_enabled": true, "thinking_budget": 8192, "safety_settings": {"HARM_CATEGORY_HARASSMENT": "BLOCK_NONE"}}`

---

#### 自定义 Anthropic 兼容 (`provider: "custom_anthropic"`)

适用于任何兼容 Anthropic Messages API 的第三方服务或自建代理。行为与 `anthropic` 基本一致，通常应填写 `base_url` 指向你的兼容服务。

```json
{
  "id": "my-anthropic-proxy",
  "name": "My Anthropic Proxy",
  "provider": "custom_anthropic",
  "api_key": "my-proxy-key",
  "base_url": "https://my-proxy.example.com",
  "user_agent": "claude-cli/1.0.25 (external, cli)",
  "status": "enabled",
  "max_concurrent": 5
}
```

**注意事项**:
- 建议填写 `base_url`，网关会向 `{base_url}/v1/messages` 发送请求
- 如果 `base_url` 为空，当前实现会回退到官方 Anthropic 地址 `https://api.anthropic.com`
- 认证方式同 Anthropic：`x-api-key` header
- 适用场景：自建 Anthropic 代理、Anthropic 兼容的第三方服务（如 AWS Bedrock 代理）

---

#### 自定义 OpenAI 兼容 (`provider: "custom_openai"`)

适用于任何兼容 OpenAI Responses API 的第三方服务。行为与 `openai` 基本一致，通常应填写 `base_url` 指向你的兼容服务。

```json
{
  "id": "my-openai-proxy",
  "name": "My OpenAI Compatible Service",
  "provider": "custom_openai",
  "api_key": "my-service-key",
  "base_url": "https://my-llm-service.example.com",
  "status": "enabled",
  "allowed_models": ["my-model-v1"],
  "model_aliases": {
    "claude-sonnet-4-20250514": "my-model-v1"
  },
  "max_concurrent": 5
}
```

**注意事项**:
- 建议填写 `base_url`，网关会向 `{base_url}/v1/responses` 发送请求
- 如果 `base_url` 为空，当前实现会回退到官方 OpenAI 地址 `https://api.openai.com`
- 认证方式同 OpenAI：`Authorization: Bearer` header
- 目标服务必须兼容 OpenAI Responses API 的流式 SSE 格式
- 适用场景：vLLM、LocalAI 等兼容 OpenAI 协议的自部署服务

---

#### 跨厂商模型映射示例

让 Claude Code CLI 请求 `claude-opus-4-6` 时实际走 OpenAI 的 `gpt-5.2`：

```json
{
  "allowed_models": ["claude-opus-4-6"],
  "model_aliases": {
    "claude-opus-4-6": "gpt-5.2"
  }
}
```

工作原理：
1. 客户端发送 `{"model": "claude-opus-4-6", ...}`
2. 路由器匹配到此账号（`allowed_models` 包含 `claude-opus-4-6`）
3. `model_aliases` 将请求模型替换为 `gpt-5.2`
4. 实际发送给 OpenAI 的模型是 `gpt-5.2`
5. 响应中的 model 字段反映上游返回值

一个模型可以被多个账号映射，路由器会根据健康状态和负载均衡策略选择

### 更新账号（部分更新）

```
PUT /admin/accounts/{id}
Content-Type: application/json
```

只需传要修改的字段：
```json
{
  "name": "New Name",
  "max_concurrent": 20
}
```

### 快捷启用/禁用

```
PUT /admin/accounts/{id}/status
Content-Type: application/json

{"status": "disabled"}
```

### 删除账号

```
DELETE /admin/accounts/{id}
```

**级联校验**：如果账号被某个 Group 引用，返回 `409`：
```json
{"error": "cannot delete account: referenced by group \"default\""}
```
需先从 Group 中移除该账号，或删除 Group 后才能删除账号。

### 重置熔断器

```
POST /admin/accounts/{id}/reset-breaker
```

强制将熔断器恢复到 `closed` 状态。

---

## 组 (Groups)

组将多个源账号聚合在一起，API 密钥通过绑定组来访问组内所有账号的模型。

### 列出所有组

```
GET /admin/groups
```

### 获取单个组

```
GET /admin/groups/{id}
```

### 创建组

```
POST /admin/groups
Content-Type: application/json
```

```json
{
  "id": "default",
  "name": "Default Group",
  "account_ids": ["openai-1", "gemini-1"],
  "allowed_models": [],
  "balancer": "round_robin"
}
```

| 字段 | 必填 | 说明 |
|------|------|------|
| `id` | | 唯一标识；不传时服务端自动生成 |
| `account_ids` | ✅ | 包含的源账号 ID 列表（按优先级排序） |
| `name` | | 显示名称 |
| `allowed_models` | | 组级模型白名单，空=不限 |
| `balancer` | | `round_robin`(默认) / `least_connections` / `weighted` / `priority` |

**负载均衡策略**：

| 策略 | 说明 |
|------|------|
| `round_robin` | 轮询（默认），请求均匀分配到所有候选账号 |
| `least_connections` | 最少连接，优先选择当前活跃请求最少的账号 |
| `weighted` | 加权轮询，按 `max_concurrent` 值作为权重分配流量（值越大流量越多，0 视为 100） |
| `priority` | 固定优先级，始终优先选择 `account_ids` 中最靠前的健康账号，后续账号作为回退 |

### 更新组（部分更新）

```
PUT /admin/groups/{id}
```

### 删除组

```
DELETE /admin/groups/{id}
```

**级联校验**：如果组被某个 API Key 引用，返回 `409`：
```json
{"error": "cannot delete group: referenced by api key \"key-1\""}
```
需先删除绑定的 API Key 后才能删除 Group。

---

## API 密钥 (Keys)

对外 API 密钥，Claude Code CLI 通过此密钥访问网关。

### 列出所有密钥

```
GET /admin/keys
```

> 不会返回原始密钥，只显示 `key_hint`（后 4 位）。响应包含当月用量。

**响应**：
```json
[
  {
    "id": "key-prod",
    "key_hint": "a6f2",
    "group_id": "default",
    "status": "enabled",
    "allowed_models": [],
    "max_concurrent": 5,
    "max_input_tokens_monthly": 10000000,
    "max_output_tokens_monthly": 5000000,
    "used_input_tokens": 18500,
    "used_output_tokens": 3200,
    "created_at": "2026-03-24T03:37:09Z"
  }
]
```

> `used_input_tokens` / `used_output_tokens` 为当前自然月的累计用量，持久化存储，按自然月自动重置。

### 获取单个密钥

```
GET /admin/keys/{id}
```

### 创建密钥

```
POST /admin/keys
Content-Type: application/json
```

```json
{
  "id": "key-prod",
  "group_id": "default",
  "status": "enabled",
  "allowed_models": [],
  "max_concurrent": 5,
  "max_input_tokens_monthly": 10000000,
  "max_output_tokens_monthly": 5000000
}
```

| 字段 | 必填 | 说明 |
|------|------|------|
| `id` | | 唯一标识；不传时服务端自动生成 |
| `group_id` | ✅ | 绑定的组 ID（必须已存在） |
| `status` | | `enabled`(默认) / `disabled` |
| `allowed_models` | | 密钥级模型白名单，空=组内全部 |
| `max_concurrent` | | 并发上限，0=不限 |
| `max_input_tokens_monthly` | | 月度输入 Token 限额，0=不限 |
| `max_output_tokens_monthly` | | 月度输出 Token 限额，0=不限 |

**响应** `201`（原始密钥仅返回一次！）:
```json
{
  "key": {
    "id": "key-prod",
    "key_hint": "a6f2",
    "group_id": "default",
    "status": "enabled",
    ...
  },
  "raw_key": "sk-8f3a...a6f2"
}
```

### 更新密钥（部分更新）

```
PUT /admin/keys/{id}
```

> 不能修改密钥值本身，只能修改配置（status、allowed_models、limits 等）。

### 快捷启用/禁用

```
PUT /admin/keys/{id}/status

{"status": "disabled"}
```

### 轮换密钥

```
POST /admin/keys/{id}/rotate
```

生成新密钥，旧密钥立即失效。**新密钥仅返回一次**：
```json
{
  "key": { "id": "key-prod", "key_hint": "b3c1", ... },
  "raw_key": "sk-new...b3c1"
}
```

### 删除密钥

```
DELETE /admin/keys/{id}
```

---

## 请求日志 (Logs)

### 查询日志

```
GET /admin/logs?key_id=&account_id=&model=&from=&to=&limit=&offset=
```

| 参数 | 说明 |
|------|------|
| `key_id` | 按 API 密钥 ID 过滤 |
| `account_id` | 按源账号 ID 过滤 |
| `model` | 按模型名过滤（匹配请求模型或实际模型） |
| `from` | 起始时间 (RFC3339: `2026-03-24T00:00:00Z`) |
| `to` | 结束时间 |
| `limit` | 返回数量（默认 100） |
| `offset` | 偏移量 |

**响应**:
```json
[
  {
    "ID": 1,
    "CreatedAt": "2026-03-24T03:37:09Z",
    "KeyID": "key-prod",
    "AccountID": "openai-1",
    "Provider": "openai",
    "ModelRequested": "claude-opus-4-6",
    "ModelActual": "gpt-5.2",
    "InputTokens": 18,
    "OutputTokens": 33,
    "CostUSD": 0.000375,
    "LatencyMs": 5035,
    "StopReason": "end_turn",
    "Error": "",
    "StatusCode": 200
  }
]
```

### 查看请求/响应 Payload

```
GET /admin/logs/{id}/payload
```

返回完整的请求体和响应体（保留期内，默认 7 天）：
```json
{
  "LogID": 1,
  "RequestBody": "{\"model\":\"gpt-5.2\",...}",
  "ResponseBody": "event: message_start\ndata: {...}\n\n..."
}
```

---

## 费用统计 (Stats)

### 费用汇总

```
GET /admin/stats/cost?group_by=account&from=&to=
```

| 参数 | 说明 |
|------|------|
| `group_by` | 分组维度：`key` / `account` / `model`（默认 `account`） |
| `from` | 起始时间 |
| `to` | 结束时间 |

**响应**:
```json
[
  {
    "group_by": "account",
    "group_value": "openai-1",
    "total_cost_usd": 0.0315,
    "total_input_tokens": 1800,
    "total_output_tokens": 3300,
    "request_count": 100
  }
]
```

---

## 系统 (System)

### 健康检查

```
GET /admin/health
```

```json
{"status": "ok"}
```

### 手动清理过期 Payload

```
POST /admin/cleanup
```

立即清理超过保留期的请求/响应 Payload，返回删除条数：
```json
{"deleted": 42, "status": "ok"}
```

> 自动清理默认每小时执行一次，可通过配置 `log.cleanup_interval` 调整（如 `30m`、`2h`）。

### Prometheus 指标

```
GET /metrics
```

暴露的指标：
- `ccgateway_requests_total{provider, model, account_id, status}` — 请求总数
- `ccgateway_request_duration_seconds{provider, model}` — 请求延迟直方图
- `ccgateway_tokens_total{provider, account_id, direction}` — Token 总数（input/output）
- `ccgateway_active_requests{account_id}` — 当前活跃请求数
- `ccgateway_circuit_breaker_state{account_id}` — 熔断器状态（0=closed, 1=open, 2=half_open）
- `ccgateway_cost_usd_total{provider, account_id, model}` — 累计费用

---

## 错误格式

所有错误返回统一格式：
```json
{"error": "error message"}
```

HTTP 状态码：
- `400` — 请求参数错误
- `401` — 认证失败（ADMIN_TOKEN 不匹配）
- `404` — 资源不存在
- `409` — 级联校验失败（被引用无法删除）
- `429` — 并发超限 / 用量超限（主 API）
- `500` — 内部错误（包含当前实现中的数据库写入失败，如重复主键）
