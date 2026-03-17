# Toolcall Text-Proxy Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在代理层实现 OpenAI 工具调用 JSON <-> 上游纯文本协议的双向转换，并保证非流式与（工具调用场景下）缓冲式返回可用。

**Architecture:** 在 `internal/openai/compat.go` 实现工具定义压缩、system 前缀注入、哨兵协议解析与响应构建；在 `internal/http/handlers.go` 集成输入/输出转换逻辑并对工具调用场景采用“缓冲后一次性返回”策略。

**Tech Stack:** Go 1.22

---

### Task 1: 输入侧工具定义压缩与 system 前缀注入

**Files:**
- Modify: `internal/openai/compat.go`
- Modify: `internal/openai/compat_test.go`

**Step 1: 写失败测试**

```go
func TestParseChatRequestWithToolsAddsSystemPrefix(t *testing.T) {
	payload := map[string]any{
		"model": "agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "get_weather",
				"description": "Get weather",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{"location": map[string]any{"type": "string", "title": "loc"}},
					"required": []any{"location"},
					"title": "Weather",
				},
			},
		}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}},
	}
	parsed := ParseChatRequest(payload)
	if !strings.Contains(parsed.Prompt, "system:") {
		t.Fatalf("missing system prefix")
	}
	if !strings.Contains(parsed.Prompt, "get_weather") {
		t.Fatalf("missing tool name")
	}
	if strings.Contains(parsed.Prompt, "title") {
		t.Fatalf("schema not compressed")
	}
}
```

**Step 2: 运行**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: FAIL（尚未注入 system 前缀）

**Step 3: 写最小实现**

- 在 `ParsedRequest` 中新增 `HasTools bool` 字段。
- 实现：
  - `compressSchema(parameters map[string]any) map[string]any` 仅保留 `type/required/properties/enum/items`。
  - `extractTools(payload map[string]any) []map[string]any` 支持 `tools` 与 legacy `functions`。
  - `extractToolChoice(payload map[string]any) string` 支持 `tool_choice` 与 legacy `function_call`。
  - `buildToolSystemPrefix(tools []map[string]any, choice string) string` 返回 JSON 字符串。
  - 修改 `ParseChatRequest` / `ParseResponsesRequest`：若有工具，向 prompt 前缀插入 `system: <json>`。

**Step 4: 验证**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/openai/compat.go internal/openai/compat_test.go
git commit -m "feat: add tool schema compression and prompt prefix"
```

---

### Task 2: 输出侧哨兵协议解析与工具调用响应构建

**Files:**
- Modify: `internal/openai/compat.go`
- Modify: `internal/openai/compat_test.go`

**Step 1: 写失败测试**

```go
func TestParseToolSentinelToChatResponse(t *testing.T) {
	text := "<<<TC>>>{\"tc\":[{\"id\":\"call_1\",\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\"}}],\"c\":\"\"}<<<END>>>"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	out := BuildChatCompletionResponseFromText(text, "agent")
	choices := out["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["tool_calls"] == nil {
		t.Fatalf("missing tool_calls")
	}
}
```

**Step 2: 运行**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: FAIL

**Step 3: 写最小实现**

- 实现：
  - `ParseToolSentinel(text string) ToolParseResult`：
    - 识别 `<<<TC>>> ... <<<END>>>`
    - 解析 JSON（`tc/c/id/n/a`）
    - `a` 统一序列化为 JSON 字符串
    - 缺失 `id` 时生成
  - `BuildChatCompletionResponseFromText(text, model)`：若存在 tool_calls 则写入 `message.tool_calls`，单个时兼容 `function_call`。
  - `BuildResponsesResponseFromText(text, model)`：若存在 tool_calls 则输出 `function_call` items，`output_text` 使用 `c`。
  - 解析失败回退为普通文本。

**Step 4: 验证**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/openai/compat.go internal/openai/compat_test.go
git commit -m "feat: parse toolcall sentinel and build responses"
```

---

### Task 3: HTTP 端点集成（工具调用缓冲返回）

**Files:**
- Modify: `internal/http/handlers.go`
- Modify: `internal/http/handlers_test.go`

**Step 1: 写失败测试**

```go
func TestChatCompletionToolSentinelMapping(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{"data": map[string]any{"message": map[string]any{"text": "<<<TC>>>{\"tc\":[{\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\"}}],\"c\":\"\"}<<<END>>>"}}}}}
	srv := newTestServer(gw)
	payload := map[string]any{"model": "agent", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status=%d", rec.Code) }
	var out map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&out)
	choices := out["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["tool_calls"] == nil { t.Fatalf("missing tool_calls") }
}
```

**Step 2: 运行**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/http -v`
Expected: FAIL

**Step 3: 写最小实现**

- 使用 `openai.ParseChatRequest` / `ParseResponsesRequest`（已注入 system 前缀）
- 非流式：用 `BuildChatCompletionResponseFromText` / `BuildResponsesResponseFromText`
- 流式 + 工具调用：**缓冲上游输出**并一次性返回（非 SSE），确保 tool_calls 完整。

**Step 4: 验证**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/http -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/http/handlers.go internal/http/handlers_test.go
git commit -m "feat: map toolcall text protocol in http handlers"
```
