# Go Proxy Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 用 Go 实现 OpenAI 兼容中转代理（/v1/chat/completions + /v1/responses），支持流式 SSE、10 分钟会话复用、CORS、/model 与 /v1/models、api.txt 挂载，并提供 Docker + GitHub Actions 多架构构建。

**Architecture:** 标准库 `net/http` 搭配少量内部模块（config/gateway/session/openai/http），无第三方 web 框架；SSE 直写 `http.ResponseWriter`。网关请求用 Go `net/http` 客户端。

**Tech Stack:** Go 1.22, Docker (multi-stage), GitHub Actions (buildx + ghcr.io)

---

### Task 1: 初始化 Go 模块与目录结构

**Files:**
- Create: `go.mod`
- Create: `cmd/proxy/main.go`
- Create: `internal/config/config.go`
- Create: `internal/gateway/client.go`
- Create: `internal/openai/compat.go`
- Create: `internal/session/manager.go`
- Create: `internal/http/handlers.go`
- Create: `internal/http/server.go`

**Step 1: 写一个最小编译测试**

```go
package main

import "fmt"

func main() {
	fmt.Println("ok")
}
```

**Step 2: 运行**

Run: `go run ./cmd/proxy`
Expected: 输出 `ok`

**Step 3: 写最小实现**

将 `main.go` 改为调用 `server.Run(...)`，但内部先只打印日志。

**Step 4: 验证**

Run: `go run ./cmd/proxy`
Expected: 看到启动日志（仍不启动服务）

**Step 5: Commit**

```bash
git add go.mod cmd/proxy/main.go internal/http/server.go
git commit -m "chore: init go module and entrypoint"
```

---

### Task 2: 配置加载（api.txt）

**Files:**
- Create: `internal/config/config_test.go`
- Modify: `internal/config/config.go`

**Step 1: 写失败测试**

```go
func TestParseApiTxt(t *testing.T) {
	txt := "key： abc\nagentCode： code\nagentVersion： 123\n"
	cfg, err := ParseApiTxt([]byte(txt))
	if err != nil { t.Fatal(err) }
	if cfg.AppKey != "abc" { t.Fatal("app key") }
	if cfg.AgentCode != "code" { t.Fatal("agentCode") }
	if cfg.AgentVersion != "123" { t.Fatal("agentVersion") }
}
```

**Step 2: 运行**

Run: `go test ./internal/config -v`
Expected: FAIL（ParseApiTxt 未实现）

**Step 3: 写最小实现**

在 `config.go` 实现：
- 支持 `:` 与 `：`
- trim 空白
- 必填 `key/agentCode`
- 默认 baseUrl（沿用 Python 版本）

**Step 4: 验证**

Run: `go test ./internal/config -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: parse api.txt config"
```

---

### Task 3: SessionManager（10 分钟复用）

**Files:**
- Create: `internal/session/manager_test.go`
- Modify: `internal/session/manager.go`

**Step 1: 写失败测试**

```go
func TestSessionReuse(t *testing.T) {
	m := NewManager(600)
	m.Set("k", "s1")
	if got := m.Get("k"); got != "s1" { t.Fatal("reuse") }
}
```

**Step 2: 运行**

Run: `go test ./internal/session -v`
Expected: FAIL

**Step 3: 写最小实现**

- `Get(key)` 返回未过期的 sessionId
- `Set(key, id)` 更新 lastSeen
- `Invalidate(key)` 删除
- TTL 过期自动清理

**Step 4: 验证**

Run: `go test ./internal/session -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session/manager.go internal/session/manager_test.go
git commit -m "feat: session manager with ttl"
```

---

### Task 4: 网关 Client（createSession/run/deleteSession）

**Files:**
- Create: `internal/gateway/client_test.go`
- Modify: `internal/gateway/client.go`

**Step 1: 写失败测试（使用 httptest）**

```go
func TestCreateSession(t *testing.T) {
	// mock server returns {"success":true,"data":{"uniqueCode":"u1"}}
}
```

**Step 2: 运行**

Run: `go test ./internal/gateway -v`
Expected: FAIL

**Step 3: 写最小实现**

- POST `/createSession`
- Header `Authorization: Bearer <APP_KEY>`
- 解析 `success` 与 `data.uniqueCode`

**Step 4: 验证**

Run: `go test ./internal/gateway -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/gateway/client.go internal/gateway/client_test.go
git commit -m "feat: gateway client create/run/delete"
```

---

### Task 5: OpenAI 兼容层（非流式）

**Files:**
- Create: `internal/openai/compat_test.go`
- Modify: `internal/openai/compat.go`

**Step 1: 写失败测试**

```go
func TestChatToPrompt(t *testing.T) {
	in := ChatRequest{Messages: []Message{{Role:"user", Content:"hi"}}}
	if PromptFromChat(in) != "user: hi" { t.Fatal("prompt") }
}
```

**Step 2: 运行**

Run: `go test ./internal/openai -v`
Expected: FAIL

**Step 3: 写最小实现**

- chat/request → prompt
- responses/input → prompt
- build chat response JSON
- build responses response JSON

**Step 4: 验证**

Run: `go test ./internal/openai -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/openai/compat.go internal/openai/compat_test.go
git commit -m "feat: openai non-stream mapping"
```

---

### Task 6: SSE 流式输出与“差分增量”

**Files:**
- Modify: `internal/openai/compat.go`
- Create: `internal/openai/stream_test.go`

**Step 1: 写失败测试**

```go
func TestDeltaDiff(t *testing.T) {
	full := []string{"你","你好","你好！"}
	out := DiffDeltas(full)
	if strings.Join(out, "") != "你好！" { t.Fatal("delta") }
}
```

**Step 2: 运行**

Run: `go test ./internal/openai -v`
Expected: FAIL

**Step 3: 写最小实现**

- Diff 逻辑：如果新 chunk 以旧 full 前缀开头，取后缀
- SSE 组装为 `data: {...}\n\n` + `data: [DONE]`

**Step 4: 验证**

Run: `go test ./internal/openai -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/openai/compat.go internal/openai/stream_test.go
git commit -m "feat: sse stream and delta diff"
```

---

### Task 7: HTTP 路由与 CORS

**Files:**
- Modify: `internal/http/handlers.go`
- Modify: `internal/http/server.go`

**Step 1: 写失败测试**

```go
func TestHealth(t *testing.T) { /* httptest -> /health */ }
```

**Step 2: 运行**

Run: `go test ./internal/http -v`
Expected: FAIL

**Step 3: 写最小实现**

- `/health`, `/model`, `/v1/models`
- `/v1/chat/completions`, `/v1/responses`
- CORS: `Access-Control-Allow-Origin: *` 等
- 读取 `x-agent-session` / `x-client-id`

**Step 4: 验证**

Run: `go test ./internal/http -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/http/handlers.go internal/http/server.go
git commit -m "feat: http handlers and cors"
```

---

### Task 8: Docker 与 CI

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Create: `.github/workflows/docker-build.yml`

**Step 1: 写失败检查**

确保文件不存在，计划新增。

**Step 2: 编写 Dockerfile**

- build: `golang:1.22`
- runtime: `gcr.io/distroless/static`
- entrypoint: `/app/proxy`
- expose 8022

**Step 3: 编写 Actions**

- buildx
- login ghcr
- push `ghcr.io/lim12137/jtptllm`
- platforms: `linux/amd64,linux/arm64`

**Step 4: 本地 dry-run**

Run: `docker build -t jtptllm:dev .`
Expected: build success

**Step 5: Commit**

```bash
git add Dockerfile .dockerignore .github/workflows/docker-build.yml
git commit -m "feat: docker and gh actions"
```

---

### Task 9: 文档与示例

**Files:**
- Create: `README.md`

**Step 1: 写最小文档**

- 如何挂载 `api.txt`
- 端口 8022
- curl 示例（旧/新端点）

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add usage"
```

