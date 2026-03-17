# Proxy IO Logging + Codex CLI Verification Design

**日期**: 2026-03-18

## 目标
- 在 Go 代理层记录“中转请求/响应”的完整入与出，便于定位工具调用是否稳定。
- 使用 Codex CLI 进行实际调用与结果判读，给出稳定性结论。

## 范围
- 仅覆盖 `/v1/chat/completions` 与 `/v1/responses` 两个入口。
- 记录入口请求与出口响应（非流式与流式）。
- 使用环境变量控制是否记录全量日志，避免生产噪音。

## 设计决策
- **日志开关**：`PROXY_LOG_IO=1` 时记录；默认关闭。
- **日志格式**：单行 JSON（便于 grep/解析），前缀 `IOLOG `。
- **记录字段**：
  - `dir`: `in` | `out`
  - `path`: 请求路径
  - `stream`: 是否流式
  - `session_id`: 实际 gateway sessionId
  - `session_key`: 复用 key（从 header 或 IP）
  - `model`
  - `payload`: 原始请求 JSON（in）
  - `prompt`: 解析后的 prompt（in）
  - `gateway`: 非流式 gateway 响应（out，仅非流式）
  - `output`: OpenAI 兼容响应 JSON（out，非流式）
  - `stream_output`: 流式拼接后的完整输出文本（out，流式）

## 数据流与日志位置
- `handleChatCompletions` / `handleResponses`：
  - 入：解析 JSON 后立即记录 `in`。
  - 出：非流式在组装响应后记录 `out`。
  - 流式：在 SSE 完成后记录 `out`，仅记录拼接后的完整文本。
- 日志输出走标准 `log.Printf`，由外部启动脚本负责重定向到文件。

## Codex CLI 实战验证
- 新增脚本 `scripts/codex_toolcall_smoke.ps1`：
  - 调用 `/v1/chat/completions`（带 tools/tool_choice）
  - 调用 `/v1/responses`（带 tools/tool_choice）
- 使用 `codex exec` 执行脚本并读取日志，让 Codex 总结：
  - 是否存在工具调用输出
  - 是否出现解析失败/回退为纯文本

## 测试策略
- 单测：开启 `PROXY_LOG_IO=1` 时，验证日志包含 `IOLOG` 前缀和关键字段。
- 实战：通过 `codex exec` 运行 smoke 脚本，检查日志中 `in/out` 对应关系与输出。

## 风险与缓解
- **日志过大**：默认关闭，且仅在问题定位时开启。
- **流式日志不完整**：仅记录拼接后的完整文本，避免高频刷屏。
