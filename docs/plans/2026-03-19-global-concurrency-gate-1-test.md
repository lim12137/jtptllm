# Global Concurrency Gate=1 Runtime Test Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 临时将 HTTP 层全局并发门闸设置为 `1`，并发发出 `5` 个 `/v1/chat/completions` 请求，以运行态验证“超过上限时排队等待而不是直接失败”的行为；测试完成后恢复默认值或确保临时值不被长期保留。

**Architecture:** 仅对全局并发门闸默认值做最小、临时的测试改动，不扩展新功能接口。验证时使用 `/v1/chat/completions` 并发请求作为外部驱动，重点观测请求完成顺序、等待行为和日志线索，以确认第一层全局 gate 生效；现有第二层 per-key pool/queue 保持不变。测试完成后必须恢复默认值 `16`，或通过临时提交/测试工作树避免将 `1` 长期保留在主工作区。

**Tech Stack:** Go 1.22、PowerShell、HTTP `/v1/chat/completions`、现有代理运行实例、日志与时间观测。

---

### Task 1: 确认当前全局 gate 落点与默认值位置

**Files:**
- Inspect: `internal/http/handlers.go`
- Inspect: `internal/http/server.go`
- Inspect: `internal/http/handlers_test.go`
- Inspect: `docs/plans/2026-03-19-global-concurrency-limit-design.md`

**Step 1: 读取全局并发门闸实现位置**

```powershell
Get-Content internal/http/handlers.go
Get-Content internal/http/server.go
```

**Step 2: 确认默认值 `16` 的具体位置**

记录：

- 默认常量定义文件与位置
- 初始化 gate 的代码位置

**Step 3: 读取现有测试**

```powershell
Get-Content internal/http/handlers_test.go
```

---

### Task 2: 先写测试，证明 gate=1 时请求会等待（TDD）

**Files:**
- Modify: `internal/http/handlers_test.go`
- Target: `internal/http/handlers.go`
- Target: `internal/http/server.go`

**Step 1: 新增 gate=1 的等待测试**

测试目标：

- 使用测试配置把全局 gate 设为 `1`
- 启动第一个 chat 请求并持有执行窗口
- 并发启动另外 4 个请求
- 观察这 4 个请求在第一个释放前不会全部完成
- 释放后应依次推进

**Step 2: 定向运行测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run Global -v
```

**Expected:** PASS，能证明“等待而非拒绝”。

---

### Task 3: 做最小临时改动，把默认 gate 设为 1

**Files:**
- Modify (temporary): `internal/http/server.go`
- Verify: `internal/http/handlers.go`

**Step 1: 将默认全局 gate 常量临时改为 `1`**

要求：

- 仅做最小测试改动
- 不连带修改其他并发参数
- 明确标记这是临时运行态验证值

**Step 2: 运行本地测试确保仍能启动**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run Global -v
```

**Expected:** PASS。

**Step 3: 启动代理实例**

根据当前项目已有启动方式，启动测试代理并确认 `/health`：

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8022/health | Select-Object -ExpandProperty Content
```

**Expected:** `{"ok":true}`

---

### Task 4: 并发发 5 个 `/v1/chat/completions` 请求，观察排队行为

**Files:**
- Run: `scripts/bench_concurrency_multi_key.ps1` 或单独测试脚本
- Output: `docs/reports/concurrency/2026-03-19-global-concurrency-gate-1-runtime-test.md`

**Step 1: 使用不同 session key 发出 5 个并发请求**

建议最小命令思路：

- 总并发：`5`
- 请求数：`5`
- 唯一 `x-client-id`
- 目标：`/v1/chat/completions`

如果直接复用脚本不够直观，可写最小 PowerShell 并发命令，例如：

```powershell
1..5 | ForEach-Object {
  Start-Job { ... Invoke-RestMethod ... }
}
```

**Step 2: 记录每个请求的开始/结束时间**

至少记录：

- 请求编号
- session key
- 开始时间
- 结束时间
- 状态码
- 是否有有效返回

**Step 3: 结合日志观察排队**

若已启用代理日志，补充记录：

- 请求进入顺序
- 请求完成顺序
- 是否存在明显串行推进

**Step 4: 形成运行态结论**

重点判断：

- 是否只有 1 个请求先进入执行
- 后 4 个是否等待而不是立即失败
- 是否在前一个结束后逐步推进

---

### Task 5: 测试结束后恢复默认值或避免临时值遗留

**Files:**
- Modify (restore): `internal/http/server.go`
- Verify: `git status`

**Step 1: 恢复默认值 `16`**

若本次是在主工作区直接临时改值，测试结束后必须立即改回：

- `defaultGlobalConcurrencyLimit = 16`

**Step 2: 重新运行关键测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
```

**Expected:** PASS。

**Step 3: 检查是否仍有临时值残留**

```bash
git diff -- internal/http/server.go
```

**Expected:** 不再保留 gate=1 的临时改动。

**Step 4: 更优替代方式说明**

若使用独立测试提交 / worktree，则：

- 在测试 worktree 中保留临时值用于验证
- 主工作区不保留该临时改动

这是比“主工作区改完再改回”更稳妥的做法。

---

### Task 6: 写验证报告

**Files:**
- Create: `docs/reports/concurrency/2026-03-19-global-concurrency-gate-1-runtime-test.md`

**Step 1: 记录测试参数**

至少记录：

- gate 临时值：`1`
- 请求数量：`5`
- 接口：`/v1/chat/completions`
- session key 策略：不同 key
- 启动方式与日志条件

**Step 2: 记录结果摘要**

包括：

- 每个请求的开始/结束时间
- 是否观察到排队
- 是否有直接拒绝
- 日志摘要（如有）

**Step 3: 记录恢复动作**

报告必须明确：

- 测试后是否已恢复默认值 `16`
- 或测试是否发生在独立工作树/临时提交，不会污染长期默认值

---

## 最小改动点摘要

- `internal/http/server.go`
  - 临时把默认全局 gate 从 `16` 改到 `1` 仅用于运行态验证
- `internal/http/handlers_test.go`
  - 验证 gate=1 时等待行为
- `docs/reports/concurrency/2026-03-19-global-concurrency-gate-1-runtime-test.md`
  - 记录运行态结果与恢复动作

## TDD / 验证命令摘要

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run Global -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8022/health | Select-Object -ExpandProperty Content
```

## 产出物

- 设计文档：`docs/plans/2026-03-19-global-concurrency-gate-1-test-design.md`
- 实施计划：`docs/plans/2026-03-19-global-concurrency-gate-1-test.md`
- 验证报告：`docs/reports/concurrency/2026-03-19-global-concurrency-gate-1-runtime-test.md`

## 建议提交信息

- `test: validate global concurrency gate with limit 1`
- `docs: add global concurrency gate=1 runtime validation report`

## 临时值恢复原则

- `gate=1` 仅用于运行态验证
- 不应长期保留在默认配置中
- 若在主工作区临时修改，测试结束后必须恢复 `16`
- 更稳妥做法是在独立 worktree 或临时测试提交中完成验证
