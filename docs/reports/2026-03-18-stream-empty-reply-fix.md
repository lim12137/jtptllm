# 2026-03-18 流式空回复修复报告

## 结论摘要
- 根因：流式解析只识别 `data.content`，忽略 `data.message.text` / `data.message.content`，且流式错误事件未被识别，导致上游有输出或错误时被吞成“空 SSE 成功结束”。
- 修复：流式 delta 解析新增 `data.message` 支持；流式解析遇到上游错误事件直接中止，避免写出 `finish_reason: "stop"` 和 `[DONE]`。

## 新增测试（先红后绿）
新增测试：
- `TestChatCompletionsStreamMessageTextDelta`
- `TestChatCompletionsStreamMessageContentDelta`
- `TestChatCompletionsStreamUpstreamErrorDoesNotFinish`

RED（失败是预期）：
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestChatCompletionsStream -v`
  - 失败点：缺少 `content` delta，且错误事件仍写出 `finish_reason: "stop"` / `[DONE]`。

GREEN（修复后通过）：
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestChatCompletionsStream -v`
  - 结果：新增用例全部通过。

## 验证命令与结果摘要
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`
  - 结果：全部通过。
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v`
  - 结果：全部通过。
