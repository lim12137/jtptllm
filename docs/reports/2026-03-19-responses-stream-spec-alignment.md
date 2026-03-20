# 2026-03-19 Responses Stream Spec Alignment Validation

## 对齐点清单

- 已完成：`/v1/responses` 流式不再输出 `data: [DONE]`，以 `response.completed` 作为收尾事件。
- 已完成：`response.output_text.delta` 事件补齐关键字段：`item_id`、`output_index`、`content_index`（并维护 `sequence_number`）。
- 已完成：补齐完成类事件（done events）：`response.output_text.done`、`response.output_item.done`，并确保 `response.completed` 里包含完整 `response` 对象。
- 已完成：`/v1/responses` 请求解析兼容 `input` 顶层 content-part list：`input: [{\"type\":\"input_text\",\"text\":\"...\"}]`。
- 部分完成：`response.completed.response.usage` 结构存在，但当前为占位（例如 `{}`），尚未提供可用的 token 统计字段（如 `input_tokens` / `output_tokens` / `total_tokens`）。
- 未完成：SSE `event: <name>` 行格式（当前以 `data: <json>` 为主，未显式输出 `event:` 行）。
- 未完成：`content_part` 级别事件（例如 `response.content_part.added` / `response.content_part.done`）。
- 未完成：错误路径语义化 SSE（例如 `error`、`response.failed` / `response.incomplete`）。当前错误仍可能导致客户端在未收到 `response.completed` 时断流。

## 运行的测试命令

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
```

## 测试结果摘要

- `PASS`：`go test ./internal/openai -v`
  - 关键用例：`TestParseResponsesRequestAcceptsTopLevelContentPartsInput`
  - 关键用例：`TestIterResponsesSSEDoesNotIncludeDONEAndIncludesDoneEvents`
- `PASS`：`go test ./internal/http -v`
  - 关键用例：`TestResponsesStreamConformsToResponseEvents`

