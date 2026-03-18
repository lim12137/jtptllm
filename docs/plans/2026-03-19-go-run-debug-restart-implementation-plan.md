# Go Run Debug Restart Script Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 新增 `restart_proxy_8022_go_debug.bat`，用于直接 `go run ./cmd/proxy` 启动调试版代理，并保留日志与健康检查。

**Architecture:** 复用现有 `scripts/restart_proxy.ps1` 的端口清理、日志 rotate、健康检查能力，必要时将启动目标抽象为可传入命令；批处理脚本仅负责设置调试环境变量并调用 PS1。优先最小改动，避免影响现有二进制启动脚本。

**Tech Stack:** Windows BAT/PowerShell、Go 1.22（`go run`）、现有代理脚本与 `/health`。

---

### Task 1: 计划内最小改动设计确认

**Files:**
- Inspect: `scripts/restart_proxy.ps1`
- Inspect: `restart_proxy_8022_exe.bat`

**Step 1: 读取现有脚本并确认可复用点**

```powershell
Get-Content scripts/restart_proxy.ps1
Get-Content restart_proxy_8022_exe.bat
```

**Step 2: 记录现有能力**

- 记录：端口清理、日志 rotate、健康检查逻辑的函数/参数位置。
- 记录：当前是否已有可变启动目标（若无，计划加入）。

**Step 3: 明确改动范围**

- 仅新增 `restart_proxy_8022_go_debug.bat`。
- 如必须修改 `scripts/restart_proxy.ps1`，限定为“启动目标可配置”。

**Step 4: Commit（若仅记录无需提交，继续下一任务）**

```bash
git status -sb
```

---

### Task 2: 实现 Go 调试重启脚本

**Files:**
- Create: `restart_proxy_8022_go_debug.bat`
- Modify (if needed): `scripts/restart_proxy.ps1`

**Step 1: 若需要，增加可配置启动目标**

示例（根据现有脚本实际结构调整）：

```powershell
param(
  [string]$BinPath = "bin\\proxy.exe",
  [switch]$LogIO,
  [string]$Command = ""
)
```

**Step 2: 在脚本中选择启动方式**

```powershell
if ($Command -ne "") {
  # 使用 Command 启动（go run）
} else {
  # 走现有 BinPath 启动逻辑
}
```

**Step 3: 新增 BAT 调试脚本**

`restart_proxy_8022_go_debug.bat` 行为：
- 设置 `PROXY_LOG_IO=1`
- 调用 `scripts/restart_proxy.ps1` 并传入 `-Command`：
  - `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe run ./cmd/proxy`

**Step 4: 自检**

```bash
git status -sb
```

---

### Task 3: 日志与健康检查验证

**Files:**
- Verify: `proxy_8022.log`
- Verify: `proxy_8022.err`

**Step 1: 运行调试脚本**

```powershell
.\restart_proxy_8022_go_debug.bat
```

**Step 2: 健康检查**

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8022/health | Select-Object -ExpandProperty Content
```

**Expected:** `{"ok":true}`

**Step 3: 日志确认**

```powershell
Get-Content -Tail 50 proxy_8022.err
Get-Content -Tail 50 proxy_8022.log
```

**Expected:** 日志存在且包含最新启动信息。

---

### Task 4: 回归测试

**Files:**
- Test: `internal/*`

**Step 1: 运行全量测试**

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

**Expected:** 全部 PASS。

---

### Task 5: 提交

**Files:**
- Add: `restart_proxy_8022_go_debug.bat`
- Modify (optional): `scripts/restart_proxy.ps1`

**Step 1: 提交变更**

```bash
git add restart_proxy_8022_go_debug.bat
git add scripts/restart_proxy.ps1
git commit -m "chore: add go run debug restart script"
```

**Step 2: 确认提交内容**

```bash
git show --name-only --oneline HEAD
```

---

### Task 6: 落盘验证报告

**Files:**
- Create: `docs/reports/2026-03-19-go-run-debug-restart-validation.md`

**Step 1: 写入验证结果**

记录：
- `restart_proxy_8022_go_debug.bat` 执行结果
- `/health` 结果
- 日志尾部摘要
- `go test ./... -v` 结果摘要

**Step 2: 提交报告**

```bash
git add docs/reports/2026-03-19-go-run-debug-restart-validation.md
git commit -m "docs: add go run debug restart validation"
```

---

## 交付与验收

**交付物：**
- `restart_proxy_8022_go_debug.bat`
-（如需）`scripts/restart_proxy.ps1` 支持 `-Command`
- `docs/reports/2026-03-19-go-run-debug-restart-validation.md`

**验收标准：**
- 调试脚本成功启动 `go run ./cmd/proxy`
- `/health` 返回 `{"ok":true}`
- `proxy_8022.log`/`proxy_8022.err` 有最新记录
- `go test ./... -v` 全部 PASS
