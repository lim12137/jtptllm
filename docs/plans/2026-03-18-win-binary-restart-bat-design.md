# Windows 二进制构建 + 8022 重启脚本（BAT 入口 + PS1 核心）设计

**日期：** 2026-03-18  
**状态：** 提议（待实现）  

## 目标

- 在 Windows 环境下提供一套**本地构建** Go 可执行文件 `bin/proxy.exe` 的脚本能力。
- 提供一个与现有文档对齐的**重启入口脚本** `restart_proxy_8022_exe.bat`，用于：
  - 按端口 `8022` 查找并结束占用进程；
  - 启动本地构建的 `bin/proxy.exe`；
  - 便于双击/命令行直接运行。
- 复杂逻辑由 PowerShell 承担（可维护、可输出更清晰的错误信息），BAT 仅作为薄入口。

## 现状问题

- 仓库中已有 `restart_proxy_8022.bat`，但它启动的是 Python 脚本（`skills/.../openai_proxy_server.py`），不是 Go 二进制。
- 文档中已引用 `restart_proxy_8022_exe.bat`（例如用于 smoke/验证），但仓库内该文件不存在，导致“按文档操作无法复现”。
- Go 服务默认监听端口是 `:8022`（硬编码调用 `server.Run(\":8022\")`），重启脚本需要围绕此端口进行进程清理与启动。

## 方案选择

选择 **方案 2：PowerShell 做核心逻辑 + BAT 作为入口**。

原因：
- 兼顾“BAT 可双击运行”和“复杂逻辑更可靠可维护”的诉求。
- 通过根目录 `restart_proxy_8022_exe.bat` 与文档保持一致，减少文档改动。
- PowerShell 更适合做：多 PID 处理、等待端口释放、日志重定向、清晰错误提示。

## 文件与脚本清单

将新增如下文件（均小于 2000 行）：

- `restart_proxy_8022_exe.bat`
  - 根目录入口脚本，对齐文档引用；内部调用 PowerShell 脚本。
- `scripts/restart_proxy.ps1`
  - 核心重启逻辑：按端口 `8022` kill 进程并启动 `bin/proxy.exe`。
- `scripts/build_windows_amd64.ps1`
  - 本地构建 `bin/proxy.exe`，默认使用固定 Go 工具链路径。

## 关键行为

### 构建（`scripts/build_windows_amd64.ps1`）

- 默认 Go 路径（来自当前环境约定）：
  - `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe`
- 默认构建命令等价于：
  - `go build -o bin/proxy.exe ./cmd/proxy`
- 输出目录不存在时自动创建 `bin\`。
- 可选开关（实现时决定是否需要）：允许覆盖输出路径、是否启用 `CGO_ENABLED=0`、是否启用 `-trimpath`、是否设置 `-ldflags "-s -w"`。

### 重启（`scripts/restart_proxy.ps1` + `restart_proxy_8022_exe.bat`）

端口固定为 `8022`（不做参数化以降低复杂度）。

重启流程：

- 识别占用端口 `8022` 的 PID：
  - 以 `netstat -ano` 为基础解析（Windows 通用）。
  - 允许匹配到多个 PID，逐个处理。
- 结束进程：
  - 优先尝试 `taskkill /PID <pid> /F`。
  - 若失败（权限不足），输出清晰提示与 PID 列表，脚本以失败退出码返回。
- 等待端口释放：
  - 轮询端口占用情况，设置超时（例如 10 秒），超时则报错退出。
- 启动二进制：
  - 启动 `bin\proxy.exe`，工作目录为仓库根目录（确保相对路径配置一致）。
  - 可选：支持设置 `PROXY_LOG_IO=1`（用于调试 IO 日志；默认建议为 0）。
- 输出/日志：
  - 推荐将 stdout/stderr 重定向到 `proxy_8022.log` / `proxy_8022.err`（便于排查启动失败与 IO 日志）。

BAT 入口行为（`restart_proxy_8022_exe.bat`）：

- 仅负责定位仓库根目录、调用：
  - `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\restart_proxy.ps1`
- 不在 BAT 中做复杂解析与逻辑分支。

## 错误处理策略

- 找不到 Go：构建脚本明确提示 `go.exe` 预期路径，并给出如何修改/传参的建议。
- 找不到 `bin\proxy.exe`：
  - 重启脚本可选择“自动触发构建”或提示先运行构建脚本（实现时二选一；默认倾向自动构建以提升一键体验）。
- 端口占用 PID 无法结束：
  - 明确提示需要管理员权限或手动处理，并输出 `netstat` 解析到的 PID。
- 启动失败：
  - 输出 `bin\proxy.exe` 的实际启动命令与日志文件位置，便于定位。

## 日志与安全注意

- `PROXY_LOG_IO=1` 可能记录请求/响应内容，可能包含敏感信息；默认应关闭，仅在排查问题时开启。
- `taskkill /F` 具有强制终止效果；仅针对 `:8022` 端口关联 PID，避免误杀其他进程。
- PowerShell 使用 `-ExecutionPolicy Bypass` 仅用于本仓库脚本运行便利性；不修改系统策略。

## 验证方式

本设计对应的验证（实现后执行）：

- 单元测试：
  - `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v`
- 构建验证：
  - `powershell -File scripts/build_windows_amd64.ps1`
  - 期望产物：`bin/proxy.exe`
- 重启验证：
  - `.\restart_proxy_8022_exe.bat`
  - 期望：端口 `8022` 被占用时能清理并重启；日志写入 `proxy_8022.log`/`proxy_8022.err`
- 工具调用 smoke（需要 proxy 运行中）：
  - `powershell -File scripts/codex_toolcall_smoke.ps1`

验证结果需要落盘到 `docs/reports/*.md`，记录命令与摘要。

## 非目标

- 不引入新的端口/地址配置系统（端口固定 `8022`）。
- 不处理 Windows 防火墙规则自动放行（已有 `expose_port_8022.bat` 可单独使用）。
- 不改变现有 Go 服务监听逻辑（仍由 `cmd/proxy/main.go` 控制）。
- 不清理或提交当前工作区中与本功能无关的未跟踪文件。*** End Patch}]}commentary to=functions.apply_patch  玩大发快三json
