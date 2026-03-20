# Qingyuan Model Selection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为代理新增可选模型 `qingyuan`，保持默认模型 `agent` 不变，并让 `qingyuan` 与 `fast`、`deepseek` 一样通过 `**model = qingyuan**` marker 选择上游模型，同时在 `/v1/models` 中展示。

**Architecture:** 沿用现有模型选择链路：请求解析层读取 `model`，兼容层在 prompt 头部注入模型 marker，gateway 继续只透传文本给上游，不新增独立模型字段。最小改动仅限兼容层 marker 白名单、模型列表输出和对应测试。

**Tech Stack:** Go 1.22、标准库 `testing`、OpenAI 兼容层、HTTP handler。

---

### Task 1: 确认当前模型选择实现与测试基线

**Files:**
- Inspect: `internal/openai/compat.go`
- Inspect: `internal/openai/compat_test.go`
- Inspect: `internal/http/handlers.go`
- Inspect: `internal/http/handlers_test.go`

**Step 1: 读取当前模型选择实现**

```powershell
Get-Content internal/openai/compat.go
Get-Content internal/http/handlers.go
```

**Step 2: 读取现有测试**

```powershell
Get-Content internal/openai/compat_test.go
Get-Content internal/http/handlers_test.go
```

**Step 3: 记录当前行为基线**

- `ParseChatRequest()` / `ParseResponsesRequest()` 默认模型为 `agent`
- `injectModelMarker()` 仅对白名单模型注入 marker
- `/v1/models` 当前返回 `fast`、`deepseek`

**Step 4: 运行现有相关测试建立基线**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
```

**Expected:** 当前测试全部 PASS。

---

### Task 2: 先写兼容层失败测试（TDD）

**Files:**
- Modify: `internal/openai/compat_test.go`
- Target: `internal/openai/compat.go`

**Step 1: 新增 chat 请求对 `qingyuan` 的失败测试**

示例断言：

```go
func TestParseChatRequestInjectsModelQingyuan(t *testing.T) {
    payload := map[string]any{
        "model":    "qingyuan",
        "messages": []any{map[string]any{"role": "user", "content": "hi"}},
    }
    parsed := ParseChatRequest(payload)
    if !strings.HasPrefix(parsed.Prompt, "**model = qingyuan**\n") {
        t.Fatalf("prompt=%q", parsed.Prompt)
    }
}
```

**Step 2: 新增 responses 请求对 `qingyuan` 的失败测试**

```go
func TestParseResponsesRequestInjectsModelQingyuan(t *testing.T) {
    payload := map[string]any{
        "model": "qingyuan",
        "input": "hi",
    }
    parsed := ParseResponsesRequest(payload)
    if !strings.HasPrefix(parsed.Prompt, "**model = qingyuan**\n") {
        t.Fatalf("prompt=%q", parsed.Prompt)
    }
}
```

**Step 3: 新增 marker 替换测试**

```go
func TestParseChatRequestReplacesExistingMarkerWithQingyuan(t *testing.T) {
    payload := map[string]any{
        "model":    "qingyuan",
        "messages": []any{map[string]any{"role": "user", "content": "**model = fast**\nhello"}},
    }
    parsed := ParseChatRequest(payload)
    if strings.Contains(parsed.Prompt, "**model = fast**") {
        t.Fatalf("old marker still present: %q", parsed.Prompt)
    }
    if !strings.HasPrefix(parsed.Prompt, "**model = qingyuan**\n") {
        t.Fatalf("prompt=%q", parsed.Prompt)
    }
}
```

**Step 4: 运行定向测试，确认先失败**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run Qingyuan -v
```

**Expected:** FAIL，提示尚未支持 `qingyuan` marker 注入。

---

### Task 3: 以最小代码改动支持 `qingyuan` marker

**Files:**
- Modify: `internal/openai/compat.go`
- Test: `internal/openai/compat_test.go`

**Step 1: 仅修改 marker 白名单**

最小改动点：`injectModelMarker()` 中的模型判断。

实现目标：
- 保持 `agent` 默认行为不变
- 保持 `fast`、`deepseek` 行为不变
- 新增 `qingyuan` 与二者同等待遇

**Step 2: 避免扩大语义**

- 不要改成“任意模型都注入 marker”
- 不要改请求/响应结构
- 不要改 gateway 协议

**Step 3: 运行定向测试验证通过**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run Qingyuan -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
```

**Expected:** 新增测试与现有兼容层测试均 PASS。

**Step 4: 提交兼容层改动**

```bash
git add internal/openai/compat.go internal/openai/compat_test.go
git commit -m "feat: add qingyuan model marker support"
```

---

### Task 4: 先写模型列表失败测试（TDD）

**Files:**
- Modify: `internal/http/handlers_test.go`
- Target: `internal/http/handlers.go`

**Step 1: 更新 `/v1/models` 断言**

将现有列表断言从“包含 `fast`、`deepseek`”扩展为“包含 `fast`、`deepseek`、`qingyuan`”。

示例检查：

```go
if !found["fast"] || !found["deepseek"] || !found["qingyuan"] {
    t.Fatalf("models=%v", found)
}
```

**Step 2: 运行定向测试，确认先失败**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
```

**Expected:** FAIL，提示模型列表尚未包含 `qingyuan`。

---

### Task 5: 以最小代码改动更新模型列表

**Files:**
- Modify: `internal/http/handlers.go`
- Test: `internal/http/handlers_test.go`

**Step 1: 在 `/v1/models` 中增加 `qingyuan`**

最小改动点：`handleModels()` 返回的 `data` 列表。

**Step 2: 保持默认模型与请求流程不变**

- 不要改 `defaultModelName`
- 不要改 `ParseChatRequest()` / `ParseResponsesRequest()` 默认值
- 不要改 handler 的主流程分支

**Step 3: 运行定向测试验证通过**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
```

**Expected:** `/v1/models` 相关测试与 HTTP 包测试全部 PASS。

**Step 4: 提交模型列表改动**

```bash
git add internal/http/handlers.go internal/http/handlers_test.go
git commit -m "feat: expose qingyuan model in model list"
```

---

### Task 6: 全量回归验证

**Files:**
- Verify: `internal/openai/compat.go`
- Verify: `internal/openai/compat_test.go`
- Verify: `internal/http/handlers.go`
- Verify: `internal/http/handlers_test.go`

**Step 1: 运行 HTTP 包测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
```

**Expected:** PASS。

**Step 2: 运行全量测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

**Expected:** PASS。

**Step 3: 检查工作区状态**

```bash
git status --short
```

**Expected:** 仅出现本次相关改动；若有无关脏文件，不回退、不覆盖。

---

### Task 7: 文档与收尾

**Files:**
- Update if needed: `docs/plans/2026-03-19-qingyuan-model-selection-design.md`
- Update if needed: `docs/plans/2026-03-19-qingyuan-model-selection.md`

**Step 1: 复核验收条件**

- `qingyuan` 出现在 `/v1/models`
- chat / responses 都会注入 `**model = qingyuan**`
- 默认模型仍是 `agent`
- 所有测试通过

**Step 2: 建议最终汇总提交**

如果希望压缩为单个功能提交，可在独立执行会话中整理为：

```bash
git commit -m "feat: add qingyuan model selection"
```

说明：若前面已按小步提交，这里不需要额外提交。

---

## 最小代码改动点摘要

- `internal/openai/compat.go`
  - 扩展 `injectModelMarker()` 白名单，加入 `qingyuan`
- `internal/openai/compat_test.go`
  - 新增 `qingyuan` marker 注入与替换测试
- `internal/http/handlers.go`
  - `/v1/models` 返回列表新增 `qingyuan`
- `internal/http/handlers_test.go`
  - 更新模型列表断言

## 验证命令摘要

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run Qingyuan -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

## 建议提交信息

- `feat: add qingyuan model marker support`
- `feat: expose qingyuan model in model list`
- 或合并为一个提交：`feat: add qingyuan model selection`
