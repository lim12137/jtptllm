# Proxy IO Logging + Codex CLI Verification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 增加代理 IO 日志开关并提供 Codex CLI 实战 smoke 测试脚本与结果记录。

**Architecture:** 在 HTTP 处理层记录请求/响应，使用环境变量 `PROXY_LOG_IO=1` 控制；新增 PowerShell smoke 脚本调用 Chat/Responses 端点；用 Codex CLI 执行脚本并产出结果报告。

**Tech Stack:** Go 1.22, PowerShell, Codex CLI

---

### Task 1: 代理 IO 日志（可开关）

**Files:**
- Modify: `internal/http/handlers.go`
- Modify: `internal/http/handlers_test.go`

**Step 1: 写失败测试**

```go
func TestIOLoggingEnabled(t *testing.T) {
	buf := &bytes.Buffer{}
	old := log.Writer()
	log.SetOutput(buf)
	defer log.SetOutput(old)
	os.Setenv("PROXY_LOG_IO", "1")
	defer os.Unsetenv("PROXY_LOG_IO")

	gw := &stubGateway{runResp: map[string]any{"data": map[string]any{"message": map[string]any{"text": "ok"}}}}
	srv := newTestServer(gw)

	payload := map[string]any{"model":"agent","messages": []any{map[string]any{"role":"user","content":"hi"}}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if !strings.Contains(buf.String(), "IOLOG") {
		t.Fatalf("missing IOLOG")
	}
}
```

**Step 2: 运行**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/http -v`
Expected: FAIL（日志尚未实现）

**Step 3: 写最小实现**

- 新增 `ioLogEnabled()` 读取 `PROXY_LOG_IO`。
- 新增 `logIO(fields map[string]any)` 输出 `IOLOG {json}`。
- 在 `handleChatCompletions` / `handleResponses` 记录 `in/out`。
- 流式在结束后记录拼接后的 `stream_output`。

**Step 4: 验证**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/http -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/http/handlers.go internal/http/handlers_test.go
git commit -m "feat: add proxy io logging"
```

---

### Task 2: Codex smoke 脚本

**Files:**
- Create: `scripts/codex_toolcall_smoke.ps1`

**Step 1: 写失败检查**

确认 `scripts/codex_toolcall_smoke.ps1` 不存在。

**Step 2: 写最小脚本**

- 调用 `/v1/chat/completions`（tools + tool_choice）
- 调用 `/v1/responses`（tools + tool_choice）
- 输出响应到控制台

**Step 3: 本地运行**

Run: `powershell -File scripts/codex_toolcall_smoke.ps1`
Expected: 端点返回 200 并输出 JSON（若代理已启动）

**Step 4: Commit**

```bash
git add scripts/codex_toolcall_smoke.ps1
git commit -m "feat: add codex toolcall smoke script"
```

---

### Task 3: Codex CLI 实战与结果记录

**Files:**
- Create: `docs/reports/2026-03-18-codex-toolcall-smoke.md`

**Step 1: 启动代理（开启日志）**

Run (example):
`$env:PROXY_LOG_IO="1"; .\restart_proxy_8022_exe.bat`

**Step 2: Codex CLI 执行**

Run:
`codex exec "Run scripts/codex_toolcall_smoke.ps1 and summarize tool-call stability based on bin\logs\proxy_8022.log"`

**Step 3: 记录结论**

在 `docs/reports/2026-03-18-codex-toolcall-smoke.md` 写入：
- 请求摘要（chat/responses）
- 是否出现 tool_calls 哨兵输出
- 解析是否稳定
- 发现的问题

**Step 4: Commit**

```bash
git add docs/reports/2026-03-18-codex-toolcall-smoke.md
git commit -m "docs: add codex toolcall smoke report"
```


