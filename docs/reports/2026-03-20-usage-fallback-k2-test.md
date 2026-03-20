# 2026-03-20 usage fallback K=2 测试报告

## 变更目标
- 当上游未透传 `usage` 时，fallback 从“按 rune 数”改为“按 `runeCount * 2`”。
- 上游透传 `usage` 的路径保持不变。

## TDD 红灯验证（改测试后、改实现前）

### 1) OpenAI 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run 'Usage|CharCount|RuneCount' -v`

结果摘要：
- `TestChatUsageFromCharCountScalesByRuneMultiplier` 失败
- `TestResponsesUsageFromCharCountScalesByRuneMultiplier` 失败
- 失败原因与预期一致：实现仍按 `runeCount`，尚未乘以 `2`

### 2) HTTP 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'Usage|Fallback' -v`

结果摘要：
- 透传测试通过（`PassesThroughUsage`）
- 3 个 fallback 测试失败（chat 非流式、responses 非流式、responses 流式）
- 失败原因与预期一致：仍返回旧口径（未乘以 `2`）

## 绿灯与回归验证（实现后）

### 1) OpenAI 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run 'Usage|CharCount|RuneCount' -v`

结果摘要：
- PASS
- 新增用例 `TestChatUsageFromCharCountScalesByRuneMultiplier`、`TestResponsesUsageFromCharCountScalesByRuneMultiplier` 通过

### 2) HTTP 层聚焦测试
命令：
`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'Usage|Fallback' -v`

结果摘要：
- PASS
- 3 个 fallback 测试通过，透传测试保持通过

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

## 结论
- `usage` fallback 已按 `runeCount * 2` 生效于 chat 非流式、responses 非流式、responses 流式。
- 上游透传 `usage` 的行为未被改变。
