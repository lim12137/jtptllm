# /v1/responses 流式 tool/function call 修复报告（2026-03-19）

## 变更目标
修复 `/v1/responses` 流式输出中将可解析的 `function_call` / tool call JSON 误当作 `response.output_text.delta` 正文输出的问题。

## 先补失败测试（Red）
新增/强化以下测试，用于复现问题：
- `internal/openai/compat_test.go`
  - `TestParseToolSentinelFallbackRawJSONObjectFunctionCall`
- `internal/http/handlers_test.go`
  - `TestResponsesStreamRawFunctionCallJSONUsesFunctionEventsNotOutputText`
  - 既有 `TestResponsesStreamToolCallUsesFunctionEventsNotOutputText` 也可稳定复现

Red 阶段现象摘要：
- `ParseToolSentinel` 无法识别裸 JSON `{"function_call":...}`，返回 `toolcalls=0`。
- `/v1/responses` stream 产出 `response.output_text.delta`，内容是完整 function/tool JSON 字符串。

## 修复实现（Green）
- 在 `internal/openai/compat.go` 中扩展 `ParseToolSentinel`：
  - 在 sentinel/```json``` block 之外，新增“裸 JSON 对象”解析路径（复用现有 `parseToolCallsFromAny` 逻辑）。
- 在 `internal/http/handlers.go` 的 `streamResponses` 中：
  - 先汇总上游 delta，再用 `openai.ParseToolSentinel` 判断是否为可解析 tool/function call。
  - 若是 tool/function call，输出结构化事件：
    - `response.output_item.added`
    - `response.function_call_arguments.delta`
    - `response.function_call_arguments.done`
    - `response.output_item.done`
    - `response.completed`
  - 不再输出 `response.output_text.delta`。

## 测试命令与结果
仅执行以下两条命令：

1. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`
- Red：FAIL（`TestResponsesStreamToolCallUsesFunctionEventsNotOutputText`、`TestResponsesStreamRawFunctionCallJSONUsesFunctionEventsNotOutputText`）
- Green：PASS（`ok   github.com/lim12137/jtptllm/internal/http`）

2. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`
- Red：FAIL（`TestParseToolSentinelFallbackRawJSONObjectFunctionCall`）
- Green：PASS（`ok   github.com/lim12137/jtptllm/internal/openai`）
