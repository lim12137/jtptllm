# Go 代码直跑调试重启脚本设计

## 目标
- 为本地调试新增一个独立入口，直接使用 `go run ./cmd/proxy` 启动代理，而不是依赖已有的 `bin/proxy.exe`。
- 保持现有 `restart_proxy_8022_exe.bat` 的二进制启动路径不变，避免调试流程和常规运行流程混淆。
- 在调试启动时自动开启 `PROXY_LOG_IO=1`，并沿用当前日志落盘与健康检查习惯，方便排查 Cherry Studio 等外部客户端请求。

## 为何不再走 `bin/proxy.exe`
- 当前调试问题集中在代理兼容层与请求链路行为，直接运行 Go 代码可以确保每次启动都使用最新源码，而不受旧二进制残留影响。
- 现有工作区里 `bin/proxy.exe` 与 `bin/proxy.exe~` 已有本地改动，继续依赖二进制容易让“代码已改但运行进程仍是旧版本”的问题反复出现。
- `go run ./cmd/proxy` 更适合高频调试与快速验证，不需要每次先构建再重启。

## 与现有 `restart_proxy_8022_exe.bat` 的关系
- `restart_proxy_8022_exe.bat` 保持现状，继续服务“运行 `bin/proxy.exe`”的普通场景。
- 新增独立的 `restart_proxy_8022_go_debug.bat`，明确表示其用途是“Go 代码直跑 + 调试日志”。
- 两个脚本职责分离：
  - 普通版：面向稳定复现或交付前检查。
  - 调试版：面向快速排障、验证最新源码、观察 IO 日志。

## 推荐方案
- 新增根目录脚本：`restart_proxy_8022_go_debug.bat`。
- 优先复用 `scripts/restart_proxy.ps1` 已有能力：
  - 杀掉占用 `8022` 的旧进程。
  - rotate `proxy_8022.log` / `proxy_8022.err`。
  - 启动后轮询 `/health`。
- 为 `scripts/restart_proxy.ps1` 增加一种“启动命令模式”或等价参数，使其既能启动 `bin/proxy.exe`，也能启动：
  - `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe run ./cmd/proxy`
- `restart_proxy_8022_go_debug.bat` 仅负责设置：
  - `PROXY_LOG_IO=1`
  - Go 可执行路径
  - 启动命令参数
  - 调用 PowerShell 重启逻辑

## 日志与调试开关
- 调试脚本默认设置 `PROXY_LOG_IO=1`，确保请求/响应会进入 `proxy_8022.err`。
- stdout 继续写入 `proxy_8022.log`，stderr 继续写入 `proxy_8022.err`。
- 每次重启前对旧日志做时间戳归档，避免多次调试日志互相覆盖。
- 不修改普通版脚本的默认日志级别与默认环境变量，避免把高噪声日志带入日常使用。

## 验证方式
1. 执行 `restart_proxy_8022_go_debug.bat`。
2. 访问 `http://127.0.0.1:8022/health`，确认返回 `200` 与 `{"ok":true}`。
3. 检查 `proxy_8022.err`，确认存在启动日志以及 `IOLOG` 记录。
4. 用 Cherry Studio 或本地最小请求触发一次 `/v1/chat/completions`，确认日志中可见实际 `tools`、`tool_choice`、prompt 注入结果。
5. 运行 `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v`，确保脚本相关改动未影响现有 Go 逻辑。

## 风险
- `go run` 启动速度会略慢于直接运行 `proxy.exe`，但对调试场景影响可接受。
- 如果 PowerShell 重启逻辑抽象不当，可能把普通版与调试版耦合，增加维护成本。
- 调试时默认开启 `PROXY_LOG_IO=1`，日志会包含请求/响应摘要，必须仅在本地排障使用。

## 非目标
- 不替换现有 `restart_proxy_8022_exe.bat`。
- 不删除或回滚已有 `bin/proxy.exe`、`bin/proxy.exe~` 本地修改。
- 不在本次设计中扩展命令行参数、端口动态配置或多实例管理。
- 不处理上游网关逻辑本身，只解决“如何稳定跑最新 Go 代码并拿到调试日志”的启动问题。
