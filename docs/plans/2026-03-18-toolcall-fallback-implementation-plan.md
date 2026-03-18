# Toolcall Fallback Parsing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在工具调用未输出哨兵时，从 JSON 代码块兜底解析 tool_calls，并在 system 前缀中注入哨兵协议指令。

**Architecture:** 扩展 `internal/openai/compat.go` 的 system 前缀与 `ParseToolSentinel` 逻辑；新增/扩展测试覆盖哨兵优先与 JSON 代码块兜底解析。

**Tech Stack:** Go 1.22

---

### Task 1: 哨兵协议指令注入

**Files:**
- Modify: `internal/openai/compat.go`
- Modify: `internal/openai/compat_test.go`

**Step 1: 写失败测试**

```go
func TestToolSystemPrefixIncludesProtocol(t *testing.T) {
	payload := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "get_weather",
				"parameters": map[string]any{"type": "object"},
			},
		}},
		"tool_choice": "none",
	}
	parsed := ParseChatRequest(payload)
	if !strings.Contains(parsed.Prompt, "tc_protocol") {
		t.Fatalf("missing tc_protocol")
	}
	if !strings.Contains(parsed.Prompt, "tc_forbid") {
		t.Fatalf("missing tc_forbid")
	}
}
```

**Step 2: 运行**

Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: FAIL

**Step 3: 写最小实现**
- 在 `buildToolSystemPrefix` 中加入 `tc_protocol` 字段（含哨兵示例）。
- 当 `tool_choice == "none"` 时加入 `tc_forbid: true`。

**Step 4: 验证**
Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: PASS

**Step 5: Commit**
```bash
git add internal/openai/compat.go internal/openai/compat_test.go
git commit -m "feat: add toolcall protocol prefix"
```

---

### Task 2: JSON 代码块兜底解析

**Files:**
- Modify: `internal/openai/compat.go`
- Modify: `internal/openai/compat_test.go`

**Step 1: 写失败测试**

```go
func TestParseToolSentinelFallbackJsonBlock(t *testing.T) {
	text := "answer\n```json\n{\"toolCallId\":\"tools_01\",\"toolName\":\"get_weather\",\"arguments\":{\"location\":\"Paris\"}}\n```"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.Content != "answer" {
		t.Fatalf("content=%q", res.Content)
	}
}
```

**Step 2: 运行**
Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: FAIL

**Step 3: 写最小实现**
- 在 `ParseToolSentinel` 中：
  - 未命中哨兵时，查找首个 ```json 代码块。
  - 支持解析三类格式：
    1. `toolCallId/toolName/arguments`
    2. `tool_calls` 数组（OpenAI 风格）
    3. `function_call`
  - 解析出 `tool_calls` 后：
    - `Content` 为移除代码块后的文本；
    - `Arguments` 统一序列化为 JSON 字符串。
- 解析失败回退纯文本。

**Step 4: 验证**
Run: `C:/Users/Administrator/Desktop/人工智能项目/低代码智能体/api调用/worktrees/toolcall-proxy/.tools/go/bin/go.exe test ./internal/openai -v`
Expected: PASS

**Step 5: Commit**
```bash
git add internal/openai/compat.go internal/openai/compat_test.go
git commit -m "feat: add json codeblock fallback"
```
