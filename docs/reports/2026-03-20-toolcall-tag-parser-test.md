# 2026-03-20 标签式工具调用解析测试报告

## 变更目标

为 `ParseToolSentinel` 增加对 `<tool_call><tool_name>...</tool_name></tool_call>` 标签式工具调用的最小解析支持，并确认 `/v1/responses` 流式输出不再把该内容当作普通文本。

## 执行命令

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelTagWrappedToolCall -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
powershell -File scripts/codex_toolcall_smoke.ps1
```

## 结果摘要

- `TestParseToolSentinelTagWrappedToolCall`：先失败后通过，证明新增回归用例能稳定复现并覆盖标签式工具调用解析。
- `TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText`：先失败后通过，证明 `/v1/responses` stream 已改为发出 `response.function_call_arguments.*` 事件，而不是 `response.output_text.delta`。
- `go test ./internal/openai -v`：通过。
- `go test ./internal/http -v`：通过。
- `powershell -File scripts/codex_toolcall_smoke.ps1`：通过；`/v1/chat/completions` 与 `/v1/responses` 非流式链路均返回 `200`，且工具调用成功。

## 覆盖说明

- parser 级别覆盖：
  - 裸 JSON `function_call`
  - 标签式 `<tool_call><multi_tool_use.parallel>...</multi_tool_use.parallel></tool_call>`
- handler 级别覆盖：
  - `/v1/responses` stream 对标签式工具调用输出 function-call 事件
  - 不再把标签式工具调用作为普通 output text 输出
