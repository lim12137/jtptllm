# 2026-03-20 usage fallback K=2 测试报告

## 变更目标
- 当上游未透传 `usage` 时，fallback 从“按 rune 数”改为“按 `runeCount * 2`”。
- 上游透传 `usage` 的路径保持不变。

## TDD 红灯验证（改测试后、改实现前）

### 1) OpenAI 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run 'TestChatUsageFromCharCountScalesByRuneMultiplier|TestResponsesUsageFromCharCountScalesByRuneMultiplier' -v`

结果摘要：
- `TestChatUsageFromCharCountScalesByRuneMultiplier` 失败
- `TestResponsesUsageFromCharCountScalesByRuneMultiplier` 失败
- 失败原因与预期一致：实现仍按 `runeCount`，尚未乘以 `2`

### 2) HTTP 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'TestChatCompletionsNonStreamPassesThroughUsage|TestResponsesNonStreamPassesThroughUsage|TestResponsesStreamPassesThroughUsage|TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesStreamFallsBackToCharCountUsageWhenMissing' -v`

结果摘要：
- 透传测试通过（`PassesThroughUsage`）
- 3 个 fallback 测试失败（chat 非流式、responses 非流式、responses 流式）
- 失败原因与预期一致：仍返回旧口径（未乘以 `2`）

## 绿灯与回归验证（实现后）

### 1) OpenAI 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run 'TestChatUsageFromCharCountScalesByRuneMultiplier|TestResponsesUsageFromCharCountScalesByRuneMultiplier' -v`

结果摘要：
- PASS
- 新增用例 `TestChatUsageFromCharCountScalesByRuneMultiplier`、`TestResponsesUsageFromCharCountScalesByRuneMultiplier` 通过

### 2) HTTP 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'TestChatCompletionsNonStreamPassesThroughUsage|TestResponsesNonStreamPassesThroughUsage|TestResponsesStreamPassesThroughUsage|TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesStreamFallsBackToCharCountUsageWhenMissing' -v`

结果摘要：
- PASS
- 透传测试通过：`TestChatCompletionsNonStreamPassesThroughUsage`、`TestResponsesNonStreamPassesThroughUsage`、`TestResponsesStreamPassesThroughUsage`
- fallback 测试通过：`TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing`、`TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing`、`TestResponsesStreamFallsBackToCharCountUsageWhenMissing`

### 3) OpenAI 包级回归
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`

结果摘要：
- PASS（全部通过）

### 4) HTTP 包级回归
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`

结果摘要：
- PASS（全部通过）

## 复验（精确命令，2026-03-20 22:13:39 +08:00）

### OpenAI 精确命中验证
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run 'TestChatUsageFromCharCountScalesByRuneMultiplier|TestResponsesUsageFromCharCountScalesByRuneMultiplier' -v`

命中测试名：
- `TestChatUsageFromCharCountScalesByRuneMultiplier`
- `TestResponsesUsageFromCharCountScalesByRuneMultiplier`

结果摘要：
- PASS

### HTTP 透传 + fallback 精确命中验证
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'TestChatCompletionsNonStreamPassesThroughUsage|TestResponsesNonStreamPassesThroughUsage|TestResponsesStreamPassesThroughUsage|TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesStreamFallsBackToCharCountUsageWhenMissing' -v`

命中测试名：
- `TestChatCompletionsNonStreamPassesThroughUsage`
- `TestResponsesNonStreamPassesThroughUsage`
- `TestResponsesStreamPassesThroughUsage`
- `TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing`
- `TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing`
- `TestResponsesStreamFallsBackToCharCountUsageWhenMissing`

结果摘要：
- PASS

## 结论
- `usage` fallback 已按 `runeCount * 2` 生效于 chat 非流式、responses 非流式、responses 流式。
- 上游透传 `usage` 的行为未被改变。
