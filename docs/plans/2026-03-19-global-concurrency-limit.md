# Global Concurrency Limit Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 HTTP 层为 `/v1/chat/completions` 和 `/v1/responses` 增加统一的全局总并发门闸，将全局在途请求限制为 `16`；超过上限时请求排队等待，请求结束后释放名额，同时保留现有 per-key session pool / queue 语义。

**Architecture:** 在 `internal/http` 层增加一层全局并发控制对象，负责 chat / responses 两条入口的统一并发占位与释放。请求在通过全局门闸后再进入现有 `ensureSession()` 与 per-key `PoolManager.Acquire()` 逻辑，因此形成“第一层全局并发限制 + 第二层 per-key pool/queue”的双层结构。最小改动集中在 `internal/http/handlers.go`、`internal/http/server.go` 和 `internal/http/handlers_test.go`。

**Tech Stack:** Go 1.22、标准库 `net/http`、`context`、channel/semaphore 风格并发控制、Go `testing`。

---

### Task 1: 读取当前请求生命周期与并发基线

**Files:**
- Inspect: `internal/http/handlers.go`
- Inspect: `internal/http/server.go`
- Inspect: `internal/session/pool.go`
- Inspect: `internal/http/handlers_test.go`

**Step 1: 读取 HTTP 入口代码**

```powershell
Get-Content internal/http/handlers.go
Get-Content internal/http/server.go
```

**Step 2: 读取 session pool 代码**

```powershell
Get-Content internal/session/pool.go
```

**Step 3: 记录当前调用链**

- `handleChatCompletions()` -> `ensureSession()` -> `PoolManager.Acquire()`
- `handleResponses()` -> `ensureSession()` -> `PoolManager.Acquire()`

**Step 4: 读取现有测试基线**

```powershell
Get-Content internal/http/handlers_test.go
```

---

### Task 2: 先写全局并发门闸失败测试（TDD）

**Files:**
- Modify: `internal/http/handlers_test.go`
- Target: `internal/http/handlers.go`
- Target: `internal/http/server.go`

**Step 1: 新增“chat 超过全局并发时等待”测试**

测试目标：

- 配置全局上限为 `1`（测试更容易验证）
- 第一个请求占住名额
- 第二个请求启动后应阻塞等待，而不是立即返回错误
- 释放第一个请求后，第二个请求应继续完成

**Step 2: 新增“responses 共享同一个全局上限”测试**

测试目标：

- 第一个 chat 请求占用全局名额
- 第二个 responses 请求也应被同一门闸挡住并等待
- 证明 chat / responses 共享的是同一全局限制，不是两套独立上限

**Step 3: 新增“错误路径也会释放名额”测试**

测试目标：

- 第一个请求进入后走错误返回
- 之后新的请求不应永远卡住
- 证明 defer/release 路径完整

**Step 4: 运行定向测试，确认先失败**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run Global -v
```

**Expected:** FAIL，因为当前还没有全局并发门闸。

---

### Task 3: 在 HTTP 层实现全局并发门闸

**Files:**
- Modify: `internal/http/handlers.go`
- Modify: `internal/http/server.go`
- Test: `internal/http/handlers_test.go`

**Step 1: 在 `Server` 上增加全局并发控制字段**

建议最小改动：

- 在 `Server` 结构体上增加一个全局 gate（例如 buffered channel / semaphore）
- 在 `Options` 中增加全局并发上限字段，便于测试注入

**Step 2: 在启动层设置默认值 `16`**

在 `internal/http/server.go` 中增加默认常量，例如：

```go
const defaultGlobalConcurrencyLimit = 16
```

并在 `NewServer(...)` 时传入。

**Step 3: 在 chat / responses 生命周期中统一获取与释放名额**

实现要求：

- 两个入口共享同一个全局门闸
- 超过上限时阻塞等待，不直接返回 `429`
- 请求结束后释放名额
- 保持与现有 `defer release(closeAfter)` 同样可靠的释放模式

**Step 4: 保持 per-key pool 逻辑不变**

- 不改 `internal/session/pool.go` 的核心语义
- 不把全局限制下沉到 `session` 层
- `forceNew` / `closeAfter` 语义继续由 `ensureSession()` / `lease.Release()` 控制

**Step 5: 运行定向测试验证通过**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run Global -v
```

**Expected:** PASS。

**Step 6: 提交最小实现**

```bash
git add internal/http/handlers.go internal/http/server.go internal/http/handlers_test.go
git commit -m "feat: add global concurrency gate for http handlers"
```

---

### Task 4: 补充回归测试，验证双层并发结构不被破坏

**Files:**
- Modify: `internal/http/handlers_test.go`
- Verify: `internal/session/pool.go`

**Step 1: 验证现有 session 相关行为仍成立**

需要重点验证：

- per-key pool 仍被调用
- `x-agent-session-reset` 逻辑未破坏
- `x-agent-session-close` 逻辑未破坏

**Step 2: 运行 HTTP 包完整测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
```

**Expected:** PASS。

**Step 3: 运行 session 包测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/session -v
```

**Expected:** PASS。

---

### Task 5: 全量回归验证

**Files:**
- Verify: `internal/http/handlers.go`
- Verify: `internal/http/server.go`
- Verify: `internal/http/handlers_test.go`
- Verify: `internal/session/pool.go`

**Step 1: 运行全仓测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

**Expected:** PASS。

**Step 2: 检查工作区状态**

```bash
git status --short
```

**Expected:** 仅出现本功能相关改动；若有无关脏文件，不回退、不覆盖。

---

### Task 6: 写验证报告

**Files:**
- Create: `docs/reports/concurrency/2026-03-19-global-concurrency-limit-validation.md`

**Step 1: 记录功能验证结果**

报告至少包含：

- 测试命令
- 全局上限默认值 `16`
- chat / responses 共用同一门闸的证据
- 超过上限时“等待而非拒绝”的证据
- 错误路径释放名额的证据
- 回归测试结果

**Step 2: 提交报告**

```bash
git add docs/reports/concurrency/2026-03-19-global-concurrency-limit-validation.md
git commit -m "docs: add global concurrency limit validation"
```

---

## 最小改动点摘要

- `internal/http/handlers.go`
  - 增加全局并发 gate 获取/释放逻辑
- `internal/http/server.go`
  - 增加默认全局并发上限 `16`
- `internal/http/handlers_test.go`
  - 增加“等待而非拒绝”、“chat/responses 共享门闸”、“错误路径释放”测试
- `docs/reports/concurrency/2026-03-19-global-concurrency-limit-validation.md`
  - 记录验证结果

## TDD 验证命令摘要

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run Global -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/session -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

## 产出物

- 设计文档：`docs/plans/2026-03-19-global-concurrency-limit-design.md`
- 实施计划：`docs/plans/2026-03-19-global-concurrency-limit.md`
- 验证报告：`docs/reports/concurrency/2026-03-19-global-concurrency-limit-validation.md`

## 建议提交信息

- `feat: add global concurrency gate for http handlers`
- `test: cover queued global concurrency behavior`
- `docs: add global concurrency limit validation`
- 或合并为：`feat: enforce queued global concurrency limit`
