# 2026-04-20 Session Context Toolcall Retry Report

## 变更目标
- 规则收敛为：当检测到“多个 `tool_call` 候选且存在不完整/畸形块”时，判定本次工具调用解析无效，不做部分恢复。
- 在会话内部上下文追加隐藏提示词，驱动模型自动重试一次。
- 自动重试上限固定为 1 次，防止循环重试。

## TDD 关键用例
- `internal/openai/compat_test.go`
  - `TestParseToolSentinelMalformedTagWrappedToolCallInvalidatesMixedCandidates`
  - `TestParseToolSentinelMalformedTagWrappedToolCallInvalidatesEvenWhenFirstIsValid`
  - `TestAppendHiddenToolCallRetryPrompt`
- `internal/http/handlers_test.go`
  - `TestChatCompletionsNonStreamRetriesOnceOnMalformedMultiToolCall`
  - `TestChatCompletionsNonStreamMalformedMultiToolCallRetriesAtMostOnce`

## 测试命令与结果摘要
- 命令：`go test ./internal/openai -v`
  - 结果：PASS（新增/调整的 tool_call 解析测试通过）
- 命令：`go test ./internal/http -v`
  - 结果：PASS（新增“自动重试 1 次 + 防循环”行为测试通过）
- 命令：`go test ./... -v`
  - 结果：PASS（全量回归通过）

## 行为结果摘要
- 多候选且含畸形 `tool_call` 时：
  - 不再恢复部分有效块，不再向用户暴露 `tool_calls` 结构化结果。
  - 仅在内部 prompt 追加隐藏提示：`本次 tool-call 格式不完整，已忽略，请重试，并且一次只调用一个 tool-call。`
  - 自动重试固定 1 次，超过不再重试。
