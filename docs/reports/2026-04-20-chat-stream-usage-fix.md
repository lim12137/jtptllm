# 2026-04-20 chat stream usage fix 验证报告

## 结论

当前 `internal/http/handlers.go` 与 `internal/http/handlers_test.go` 的 diff 已实现 `chat.completions` 流式 `usage` 修复：

- 流式收集从 `collectStreamText` 改为 `collectStreamTextWithUsage`，可以在消费网关 SSE 时同时提取 `usage`。
- `handleChatCompletions` 的两条流式分支都会在构造最终响应时填充 `usage`。
- `streamChatCompletion` 与 `streamChatCompletionFromResponse` 会把 `usage` 写入最终 `chat.completion.chunk`。
- 新增回归测试覆盖：
  - 上游已返回 `usage` 的透传行为
  - 上游未返回 `usage` 时的字符数 fallback 行为

## 验证命令

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -run 'TestChatCompletionsStream(PassesThroughUsage|FallsBackToCharCountUsageWhenMissing)$' -v
```

## 结果摘要

- 命令退出码：`0`
- 通过测试：
  - `TestChatCompletionsStreamPassesThroughUsage`
  - `TestChatCompletionsStreamFallsBackToCharCountUsageWhenMissing`
- 关键结论：
  - 流式最终 chunk 已包含 `usage`
  - 上游 `usage` 会透传
  - 上游缺失 `usage` 时会回退到字符数估算
