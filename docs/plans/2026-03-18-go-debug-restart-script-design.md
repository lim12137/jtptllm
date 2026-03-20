# Go 版代理调试重启脚本设计

## 目标
- 为 Go 版代理提供独立的调试重启入口，避免把日常启动和排障启动混在同一个脚本里。
- 一键完成端口清理、调试环境变量设置、日志轮转、启动 `bin/proxy.exe` 与基础健康检查。
- 保持现有普通重启脚本可继续用于非调试场景，降低误用风险。

## 为什么采用独立 debug 脚本
- 调试模式需要稳定开启 `PROXY_LOG_IO=1`，这会输出请求/响应日志，不适合作为默认启动行为。
- 独立脚本能明确区分“普通运行”和“问题排查”两种场景，避免后续误把高噪声日志带入正常使用。
- 现有 `restart_proxy_8022_exe.bat` 已承担普通 Go 二进制启动职责，追加参数分支会增加双击使用时的心智负担。

## 与现有 `restart_proxy_8022_exe.bat` 的关系
- `restart_proxy_8022_exe.bat` 保持普通模式，不默认打开 IO 调试日志。
- 新脚本建议命名为 `restart_proxy_8022_exe_debug.bat`，专门用于调试 Go 版代理。
- 两者共享同一目标二进制 `bin/proxy.exe` 与同一端口 `8022`，但环境变量与日志策略不同。

## 脚本行为
1. 结束当前占用 `8022` 端口的进程，确保旧实例不会残留。
2. 将已有 `bin\logs\proxy_8022.log` / `bin\logs\proxy_8022.err` 轮转为带时间戳的归档文件，避免覆盖上一次调试现场。
3. 在当前启动会话中设置 `PROXY_LOG_IO=1`，让 Go 代理输出入/出站 IO 日志。
4. 以仓库根目录为工作目录启动 `bin/proxy.exe`，继续把 stdout/stderr 重定向到 `bin\logs\proxy_8022.log` / `bin\logs\proxy_8022.err`。
5. 启动后执行一次 `http://127.0.0.1:8022/health` 检查，若失败则在控制台输出明确错误。

## 风险与非目标

### 风险
- `PROXY_LOG_IO=1` 会记录请求与响应内容，仅适合临时排障，不能长期作为默认模式。
- 日志轮转后会产生新的归档文件，需要依赖现有 `.gitignore` 规则避免污染 `git status`。
- 若 `bin/proxy.exe` 不是最新构建产物，调试脚本仍会启动旧二进制，因此脚本本身不负责自动构建。

### 非目标
- 不替代 Python 版 `restart_proxy_8022.bat`。
- 不在本次设计中加入自动构建 `bin/proxy.exe` 的逻辑。
- 不改变 `restart_proxy_8022_exe.bat` 的默认行为。

## 验证方式
- 运行调试脚本后访问 `GET /health`，预期返回 `{"ok":true}`。
- 检查 `bin\logs\proxy_8022.err` 中是否出现 `starting on :8022` 与 `IOLOG` 相关输出。
- 使用外部客户端（如 Cherry Studio）发送带 `tools` 的请求，确认 `PROXY_LOG_IO=1` 下可观察到原始请求体和代理输出。
- 完成实现后执行 `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v`，确保脚本相关变更未影响 Go 代理测试基线。


